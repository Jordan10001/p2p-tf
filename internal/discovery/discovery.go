package discovery

import (
	"context"
	"fmt"
	"p2p-transfer/internal/config"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"
	"p2p-transfer/internal/utils"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

const offlineAfter = 30 * time.Second

var (
	devices         = make(map[string]models.Device)
	devicesMu       sync.RWMutex
	server          *zeroconf.Server
	cancelDiscovery context.CancelFunc
	scanRequests    chan struct{}
	scanRequestsMu  sync.RWMutex

	// OnDeviceChange is a callback triggered when the list of devices changes.
	OnDeviceChange func(devices []models.Device)
)

// Start begins advertising this device and searching for peers.
func Start() error {
	devicesMu.Lock()
	changed := false
	// Mark existing devices offline until they are rediscovered.
	for id, dev := range devices {
		if dev.Status != "offline" {
			dev.Status = "offline"
			devices[id] = dev
			changed = true
		}
	}
	devicesMu.Unlock()
	if changed {
		triggerChangeCallback()
	}

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
	ctx, cancel := context.WithCancel(context.Background())
	cancelDiscovery = cancel
	requests := make(chan struct{}, 1)
	scanRequestsMu.Lock()
	scanRequests = requests
	scanRequestsMu.Unlock()

	// Start browsing
	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for {
			select {
			case <-ctx.Done():
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
		err := res.Browse(ctx, "_p2ptransfer._tcp", "local.", entries)
		if err != nil && ctx.Err() == nil {
			logger.Error("Error browsing mDNS services: %v", err)
		}
	}()

	// Keep browsing in the background and allow manual scans without restarting mDNS.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				browseServices(ctx, "Periodic")
			case <-requests:
				browseServices(ctx, "Manual")
			}
		}
	}()

	// Start background housekeeper to mark offline devices
	go housekeeper(ctx)

	return nil
}

// RequestScan starts an immediate mDNS browse without interrupting advertising.
func RequestScan() bool {
	scanRequestsMu.RLock()
	requests := scanRequests
	scanRequestsMu.RUnlock()
	if requests == nil {
		return false
	}
	select {
	case requests <- struct{}{}:
	default:
		// A scan is already queued, so the request is accepted.
	}
	return true
}

func browseServices(parent context.Context, label string) {
	res, err := zeroconf.NewResolver(nil)
	if err != nil {
		logger.Error("%s mDNS resolver error: %v", label, err)
		return
	}
	browseCtx, browseCancel := context.WithTimeout(parent, 5*time.Second)
	defer browseCancel()
	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for {
			select {
			case <-browseCtx.Done():
				return
			case entry, ok := <-entries:
				if !ok {
					return
				}
				handleServiceEntry(entry)
			}
		}
	}()

	if err := res.Browse(browseCtx, "_p2ptransfer._tcp", "local.", entries); err != nil {
		logger.Error("%s mDNS browse error: %v", label, err)
		return
	}
	// Browse starts its receive loop and returns immediately after sending the
	// query. Keep this resolver alive for the timeout so responses can arrive.
	<-browseCtx.Done()
}

// Stop halts advertising and browsing.
func Stop() {
	scanRequestsMu.Lock()
	scanRequests = nil
	scanRequestsMu.Unlock()
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

// GetActiveDevices returns only devices that are currently considered online.
func GetActiveDevices() []models.Device {
	devicesMu.RLock()
	defer devicesMu.RUnlock()

	list := make([]models.Device, 0, len(devices))
	for _, dev := range devices {
		if dev.Status != "offline" {
			list = append(list, dev)
		}
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
		ID:       peerID,
		Hostname: hostname,
		Name:     name,
		IP:       ip,
		Port:     entry.Port,
		Status:   status,
		LastSeen: time.Now(),
	}

	devices[peerID] = newDev
	devicesMu.Unlock()

	// Notify if new device or status changed
	if !exists || existing.Status != status || existing.IP != ip {
		logger.Info("Device discovered/updated: %s (%s) at %s:%d, status: %s", name, hostname, ip, entry.Port, status)
		triggerChangeCallback()
	}
}

func housekeeper(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if markStaleDevicesOffline(time.Now()) {
				triggerChangeCallback()
			}
		}
	}
}

func markStaleDevicesOffline(now time.Time) bool {
	changed := false
	devicesMu.Lock()
	for id, dev := range devices {
		if dev.Status != "offline" && now.Sub(dev.LastSeen) > offlineAfter {
			dev.Status = "offline"
			devices[id] = dev
			changed = true
			logger.Info("Device timed out: %s (%s)", dev.Name, dev.Hostname)
		}
		// Remove offline entries after 10 minutes to keep the cache bounded.
		if dev.Status == "offline" && now.Sub(dev.LastSeen) > 10*time.Minute {
			delete(devices, id)
			changed = true
		}
	}
	devicesMu.Unlock()
	return changed
}

func triggerChangeCallback() {
	if OnDeviceChange != nil {
		OnDeviceChange(GetActiveDevices())
	}
}
