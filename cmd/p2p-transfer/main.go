package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"p2p-transfer"
	"p2p-transfer/internal/config"
	"p2p-transfer/internal/discovery"
	"p2p-transfer/internal/history"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"
	"p2p-transfer/internal/systray"
	"p2p-transfer/internal/transfer"
	"p2p-transfer/internal/web"
	"p2p-transfer/internal/websocket"
)

func main() {
	// 1. Initialize Logger
	if err := logger.Init(); err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	defer logger.Close()

	logger.Info("Starting P2P File Transfer Application...")

	// Extract favicon.ico to Temp directory for Win32 System Tray
	icoBytes, err := p2ptransfer.WebFS.ReadFile("web/static/icons/favicon.ico")
	if err == nil {
		tempIco := filepath.Join(os.TempDir(), "p2p-transfer-favicon.ico")
		_ = os.WriteFile(tempIco, icoBytes, 0644)
	}

	// 2. Initialize Config
	cfg, err := config.Init()
	if err != nil {
		logger.Error("Failed to initialize configuration: %v", err)
		os.Exit(1)
	}

	// 3. Initialize History
	if err := history.Init(); err != nil {
		logger.Error("Failed to initialize history: %v", err)
		os.Exit(1)
	}

	// 4. Start TCP File Transfer Receiver
	if err := transfer.StartTCPReceiver(cfg.TransferPort); err != nil {
		logger.Error("Failed to start TCP file receiver: %v", err)
		os.Exit(1)
	}
	defer transfer.StopTCPReceiver()

	// 5. Start WebSocket Hub on port 8081
	websocket.Start(":8081")

	// Hook initial data delivery for new websocket clients
	websocket.TriggerInitialData = func(client *websocket.Client) {
		sendToClient := func(c *websocket.Client, msgType string, payload interface{}) {
			msg := models.WSMessage{
				Type:    msgType,
				Payload: payload,
			}
			data, err := json.Marshal(msg)
			if err == nil {
				c.Send(data)
			}
		}

		sendToClient(client, "device_list", discovery.GetDevices())
		sendToClient(client, "config_update", config.Get())
		sendToClient(client, "queue_update", transfer.GetQueue())
		sendToClient(client, "history_update", history.GetEntries())
	}

	// 6. Hook discovery changes to WebSocket broadcasts
	discovery.OnDeviceChange = func(devices []models.Device) {
		websocket.Broadcast("device_list", devices)
	}

	// 7. Start Peer Discovery Service
	if err := discovery.Start(); err != nil {
		logger.Error("Failed to start discovery service: %v", err)
	}
	defer discovery.Stop()

	// 8. Start Web Dashboard API on Port 8080
	web.Start(8080)

	// 9. Start Windows System Tray Integration
	onOpenDashboard := func() {
		exec.Command("cmd", "/c", "start", "http://localhost:8080").Start()
	}
	onSettings := func() {
		exec.Command("cmd", "/c", "start", "http://localhost:8080").Start() // Frontend handles route routing
	}
	onRestartDiscovery := func() {
		logger.Info("Restarting discovery from system tray...")
		discovery.Restart()
	}
	onExit := func() {
		logger.Info("Exit requested from system tray. Shutting down...")
		cleanupAndExit()
	}

	systray.Start(onOpenDashboard, onSettings, onRestartDiscovery, onExit)

	// Open browser automatically on startup
	go func() {
		time.Sleep(1 * time.Second)
		logger.Info("Opening dashboard in browser...")
		exec.Command("cmd", "/c", "start", "http://localhost:8080").Start()
	}()

	// Show startup notification balloon in Windows System Tray
	go func() {
		time.Sleep(2 * time.Second)
		systray.ShowNotification(
			"P2P File Transfer Active", 
			fmt.Sprintf("Device: %s\nScan active on LAN transfer port %d", cfg.DeviceName, cfg.TransferPort),
		)
	}()

	// 10. Handle OS shutdown signals for clean exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	
	select {
	case sig := <-sigChan:
		logger.Info("Received OS signal: %v. Shutting down...", sig)
		cleanupAndExit()
	}
}

func cleanupAndExit() {
	discovery.Stop()
	transfer.StopTCPReceiver()
	logger.Close()
	os.Exit(0)
}
