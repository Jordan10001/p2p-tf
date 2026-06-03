package history

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"p2p-transfer/internal/models"
)

var (
	historyPath string
	historyList []models.HistoryEntry
	mu          sync.Mutex
)

// Init loads transfer history from file.
func Init() error {
	mu.Lock()
	defer mu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	historyPath = filepath.Join(home, ".p2p-transfer", "history.json")

	if _, err := os.Stat(historyPath); errors.Is(err, os.ErrNotExist) {
		historyList = make([]models.HistoryEntry, 0)
		return saveLocked()
	}

	data, err := os.ReadFile(historyPath)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, &historyList)
}

// GetEntries returns all history entries.
func GetEntries() []models.HistoryEntry {
	mu.Lock()
	defer mu.Unlock()
	
	// Return a copy to prevent concurrent modification
	res := make([]models.HistoryEntry, len(historyList))
	copy(res, historyList)
	return res
}

// AddEntry adds a new transfer history entry and saves to disk.
func AddEntry(entry models.HistoryEntry) error {
	mu.Lock()
	defer mu.Unlock()

	// Prepend to show newest first
	historyList = append([]models.HistoryEntry{entry}, historyList...)
	
	// Keep history to a reasonable size (e.g. 500 entries)
	if len(historyList) > 500 {
		historyList = historyList[:500]
	}

	return saveLocked()
}

// ClearEntries deletes all history records.
func ClearEntries() error {
	mu.Lock()
	defer mu.Unlock()

	historyList = make([]models.HistoryEntry, 0)
	return saveLocked()
}

func saveLocked() error {
	data, err := json.MarshalIndent(historyList, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(historyPath, data, 0644)
}
