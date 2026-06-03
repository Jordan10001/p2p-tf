package discovery

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
	"p2p-transfer/internal/config"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"
	"p2p-transfer/internal/utils"

	"github.com/grandcat/zeroconf"
)

var (
	devices         = make(map[string]models.Device)
	devicesMu       sync.RWMutex
	server          *zeroconf.Server
	resolver        *zeroconf.Resolver
	cancelDiscovery context.CancelFunc
	discoveryCtx    context.Context
	
	// OnDeviceChange is a callback triggered when the list of devices changes.
	OnDeviceChange func(devices []models.Device)
)

// Start begins advertising this device and searching for peers.
func Start() error {
	devicesMu.Lock()
	// Mark existing devices as offline initially instead of clearing, preventing UI flash
	for id, dev := range devices {
		dev.Status = "offline"
		devices[id] = dev
	}
	devicesMu.Unlock()

	cfg := config.Get()
	if !cfg.EnableDiscovery {
		logger.Warn("Discovery is disabled in settings")
		return nil
	}

	ip, err := utils.GetLocalIP()
	if err != nil {
		return fmt.Errorf("could not get local IP: %w", err)
	}

	// 1. Register mDNS service
	txtRecords := []string{
		"id=" + cfg.DeviceID,
		"name=" + cfg.DeviceName,
		"hostname=" + cfg.Hostname,
		"port=" + fmt.Sprintf("%d", cfg.TransferPort),
		"status=online",
	}

	logger.Info("Advertising mDNS service: Name=%s, Port=%d, IP=%s", cfg.DeviceName, cfg.TransferPort, ip)
	
	srv, err := zeroconf.Register(
		cfg.DeviceName,
		"_p2ptransfer._tcp",
		"local.",
		cfg.TransferPort,
		txtRecords,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register mDNS service: %w", err)
	}
	server = srv

	// 2. Start resolver to browse for other services
	res, err := zeroconf.NewResolver(nil)
	if err != nil {
		server.Shutdown()
		return fmt.Errorf("failed to create resolver: %w", err)
	}
	resolver = res

	discoveryCtx, cancelDiscovery = context.WithCancel(context.Background())

	// Start browsing
	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for {
			select {
			case <-discoveryCtx.Done():
				return
			case entry, ok := <-entries:
				if !ok {
					return
				}
				handleServiceEntry(entry)
			}
		}
	}()

	go func() {
		err := resolver.Browse(discoveryCtx, "_p2ptransfer._tcp", "local.", entries)
		if err != nil && discoveryCtx.Err() == nil {
			logger.Error("Error browsing mDNS services: %v", err)
		}
	}()

	// Start periodic query loop to keep devices alive and find any that missed the initial multicast
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-discoveryCtx.Done():
				return
			case <-ticker.C:
				logger.Info("Performing periodic background mDNS scan...")
				browseCtx, browseCancel := context.WithTimeout(discoveryCtx, 5*time.Second)
				browseEntries := make(chan *zeroconf.ServiceEntry)
				
				go func() {
					for {
						select {
						case <-browseCtx.Done():
							return
						case entry, ok := <-browseEntries:
							if !ok {
								return
							}
							handleServiceEntry(entry)
						}
					}
				}()
				
				err := resolver.Browse(browseCtx, "_p2ptransfer._tcp", "local.", browseEntries)
				if err != nil && browseCtx.Err() != context.DeadlineExceeded && browseCtx.Err() != context.Canceled {
					logger.Error("Periodic mDNS browse error: %v", err)
				}
				browseCancel()
			}
		}
	}()

	// Start background housekeeper to mark offline devices
	go housekeeper()

	return nil
}

// Stop halts advertising and browsing.
func Stop() {
	if cancelDiscovery != nil {
		cancelDiscovery()
	}
	if server != nil {
		server.Shutdown()
	}
	logger.Info("Discovery service stopped")
}

// Restart stops and starts the discovery service.
func Restart() error {
	Stop()
	// Allow system tray or other components to settle
	time.Sleep(500 * time.Millisecond)
	return Start()
}

// GetDevices returns the list of all currently known devices.
func GetDevices() []models.Device {
	devicesMu.RLock()
	defer devicesMu.RUnlock()

	list := make([]models.Device, 0, len(devices))
	for _, dev := range devices {
		list = append(list, dev)
	}
	return list
}

// UpdateDeviceStatus updates the status of a specific device.
func UpdateDeviceStatus(deviceID string, status string) {
	devicesMu.Lock()
	dev, exists := devices[deviceID]
	if exists {
		dev.Status = status
		dev.LastSeen = time.Now()
		devices[deviceID] = dev
	}
	devicesMu.Unlock()
	
	if exists {
		triggerChangeCallback()
	}
}

func handleServiceEntry(entry *zeroconf.ServiceEntry) {
	cfg := config.Get()
	
	var peerID, name, hostname, status string
	for _, txt := range entry.Text {
		parts := strings.SplitN(txt, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(parts[0])
		val := parts[1]
		switch key {
		case "id":
			peerID = val
		case "name":
			name = val
		case "hostname":
			hostname = val
		case "status":
			status = val
		}
	}

	// Ignore ourselves
	if peerID == cfg.DeviceID || peerID == "" {
		return
	}

	// Determine IP
	var ip string
	if len(entry.AddrIPv4) > 0 {
		ip = entry.AddrIPv4[0].String()
	} else if len(entry.AddrIPv6) > 0 {
		return
	} else {
		return
	}

	devicesMu.Lock()
	existing, exists := devices[peerID]
	
	newDev := models.Device{
		ID:        peerID,
		Hostname:  hostname,
		Name:      name,
		IP:        ip,
		Port:      entry.Port,
		Status:    status,
		LastSeen:  time.Now(),
	}
	
	devices[peerID] = newDev
	devicesMu.Unlock()

	// Notify if new device or status changed
	if !exists || existing.Status != status || existing.IP != ip {
		logger.Info("Device discovered/updated: %s (%s) at %s:%d, status: %s", name, hostname, ip, entry.Port, status)
		triggerChangeCallback()
	}
}

func housekeeper() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-discoveryCtx.Done():
			return
		case <-ticker.C:
			changed := false
			now := time.Now()
			
			devicesMu.Lock()
			for id, dev := range devices {
				// If we haven't seen the device in 120 seconds, mark it as offline
				if dev.Status != "offline" && now.Sub(dev.LastSeen) > 120*time.Second {
					dev.Status = "offline"
					devices[id] = dev
					changed = true
					logger.Info("Device timed out: %s (%s)", dev.Name, dev.Hostname)
				}
				// Remove completely if offline for more than 10 minutes
				if dev.Status == "offline" && now.Sub(dev.LastSeen) > 10*time.Minute {
					delete(devices, id)
					changed = true
				}
			}
			devicesMu.Unlock()

			if changed {
				triggerChangeCallback()
			}
		}
	}
}

func triggerChangeCallback() {
	if OnDeviceChange != nil {
		OnDeviceChange(GetDevices())
	}
}
