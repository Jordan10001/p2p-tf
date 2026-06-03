package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"p2p-transfer/internal/models"

	"github.com/google/uuid"
)

var (
	configDir  string
	configPath string
	settings   models.Settings
)

// Init loads the config from file, or creates default settings if not exists.
func Init() (*models.Settings, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	configDir = filepath.Join(home, ".p2p-transfer")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, err
	}

	configPath = filepath.Join(configDir, "config.json")

	if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
		// Create default configuration
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "Windows-PC"
		}

		settings = models.Settings{
			DeviceID:                         uuid.New().String(),
			DeviceName:                       hostname + "-P2P",
			Hostname:                         hostname,
			TransferPort:                     50005,
			DownloadDir:                      filepath.Join(home, "Downloads", "P2PTransfer"),
			ShowNotifications:                true,
			ShowTransferCompleteNotification: true,
			AutoOpenFolder:                   false,
			AutoAccept:                       false,
			EnableDiscovery:                  true,
			AutoScan:                         true,
			MinimizeToTray:                   true,
			StartWithWindows:                 false,
			Theme:                            "dark",
		}

		if err := Save(&settings); err != nil {
			return nil, err
		}
	} else {
		// Load existing
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, err
		}
	}

	// Ensure download directory exists
	if err := os.MkdirAll(settings.DownloadDir, 0755); err != nil {
		// Fallback to home/Downloads/P2PTransfer
		settings.DownloadDir = filepath.Join(home, "Downloads", "P2PTransfer")
		os.MkdirAll(settings.DownloadDir, 0755)
	}

	return &settings, nil
}

// Get returns the current loaded settings.
func Get() *models.Settings {
	return &settings
}

// Save saves the settings back to the JSON file.
func Save(newSettings *models.Settings) error {
	settings = *newSettings
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
