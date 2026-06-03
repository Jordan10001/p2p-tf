package transfer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	"p2p-transfer/internal/config"
	"p2p-transfer/internal/history"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"
	"p2p-transfer/internal/websocket"
)

var (
	activeTransfers   = make(map[string]*models.Transfer)
	activeTransfersMu sync.RWMutex

	pendingAccepts    = make(map[string]chan bool)
	pendingAcceptsMu  sync.Mutex

	cancelChannels    = make(map[string]chan struct{})
	cancelChannelsMu  sync.Mutex

	// Local send queue (files waiting to be sent)
	sendQueue   = make([]models.TransferItem, 0)
	sendQueueMu sync.Mutex
)

// AddToQueue validates and adds files to the local send queue.
func AddToQueue(paths []string) ([]string, []string) {
	sendQueueMu.Lock()
	defer sendQueueMu.Unlock()

	var added []string
	var duplicates []string

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			logger.Error("Queue validation failed for %s: %v", path, err)
			continue
		}

		if info.Size() == 0 && !info.IsDir() {
			logger.Warn("Skipping empty file: %s", path)
			continue
		}

		name := filepath.Base(path)
		// Check for duplicate in queue
		isDup := false
		for _, item := range sendQueue {
			if item.Path == path {
				isDup = true
				break
			}
		}

		if isDup {
			duplicates = append(duplicates, name)
			continue
		}

		itemType := "file"
		if info.IsDir() {
			itemType = "folder"
		}

		sendQueue = append(sendQueue, models.TransferItem{
			Path: path,
			Name: name,
			Size: info.Size(),
			Type: itemType,
		})
		added = append(added, name)
	}

	broadcastQueue()
	return added, duplicates
}

// RemoveFromQueue removes an item from the local send queue.
func RemoveFromQueue(index int) {
	sendQueueMu.Lock()
	defer sendQueueMu.Unlock()

	if index >= 0 && index < len(sendQueue) {
		sendQueue = append(sendQueue[:index], sendQueue[index+1:]...)
	}
	broadcastQueue()
}

// ClearQueue clears the local send queue.
func ClearQueue() {
	sendQueueMu.Lock()
	sendQueue = make([]models.TransferItem, 0)
	sendQueueMu.Unlock()
	broadcastQueue()
}

// GetQueue returns the current send queue.
func GetQueue() []models.TransferItem {
	sendQueueMu.Lock()
	defer sendQueueMu.Unlock()
	
	res := make([]models.TransferItem, len(sendQueue))
	copy(res, sendQueue)
	return res
}

func broadcastQueue() {
	websocket.Broadcast("queue_update", GetQueue())
}

// RegisterActiveTransfer adds a transfer to the active map and broadcasts.
func RegisterActiveTransfer(t *models.Transfer) {
	activeTransfersMu.Lock()
	activeTransfers[t.ID] = t
	activeTransfersMu.Unlock()

	websocket.Broadcast("transfer_update", t)
}

// UpdateTransferProgress updates the progress of an active transfer.
func UpdateTransferProgress(id string, bytesTrans int64, speed float64, eta int64, progress float64) {
	activeTransfersMu.Lock()
	t, ok := activeTransfers[id]
	if ok {
		t.BytesTrans = bytesTrans
		t.Speed = speed
		t.ETA = eta
		t.Progress = progress
	}
	activeTransfersMu.Unlock()

	if ok {
		websocket.Broadcast("transfer_update", t)
	}
}

// UpdateTransferStatus changes the status of a transfer and updates history if completed.
func UpdateTransferStatus(id string, status string, errStr string) {
	activeTransfersMu.Lock()
	t, ok := activeTransfers[id]
	if ok {
		if t.Status == "completed" || t.Status == "failed" || t.Status == "cancelled" {
			activeTransfersMu.Unlock()
			return
		}
		t.Status = status
		t.Error = errStr
		if status == "completed" || status == "failed" || status == "cancelled" {
			t.Speed = 0
			t.ETA = 0
			if status == "completed" {
				t.Progress = 100.0
			}
		}
	}
	activeTransfersMu.Unlock()

	if ok {
		websocket.Broadcast("transfer_update", t)

		// Save to history when terminal state is reached
		if status == "completed" || status == "failed" || status == "cancelled" {
			fileName := ""
			if len(t.Files) > 0 {
				if len(t.Files) == 1 {
					fileName = t.Files[0].Name
				} else {
					fileName = fmt.Sprintf("%s and %d other items", t.Files[0].Name, len(t.Files)-1)
				}
			}

			senderName := t.PeerName
			receiverName := config.Get().DeviceName
			if t.Direction == "send" {
				senderName = config.Get().DeviceName
				receiverName = t.PeerName
			}

			history.AddEntry(models.HistoryEntry{
				ID:        t.ID,
				FileName:  fileName,
				Sender:    senderName,
				Receiver:  receiverName,
				Size:      t.TotalSize,
				Timestamp: time.Now(),
				Status:    status,
			})
			websocket.Broadcast("history_update", history.GetEntries())
		}
	}
}

// GetActiveTransfers returns the list of all currently tracked active transfers.
func GetActiveTransfers() []models.Transfer {
	activeTransfersMu.RLock()
	defer activeTransfersMu.RUnlock()

	list := make([]models.Transfer, 0, len(activeTransfers))
	for _, t := range activeTransfers {
		list = append(list, *t)
	}
	return list
}

// CancelTransfer cancels an active transfer.
func CancelTransfer(id string) {
	cancelChannelsMu.Lock()
	ch, ok := cancelChannels[id]
	cancelChannelsMu.Unlock()

	if ok && ch != nil {
		close(ch)
		UpdateTransferStatus(id, "cancelled", "Cancelled by user")
	}
}

// RegisterCancelChannel registers the cancellation channel for a transfer.
func RegisterCancelChannel(id string, ch chan struct{}) {
	cancelChannelsMu.Lock()
	cancelChannels[id] = ch
	cancelChannelsMu.Unlock()
}

// UnregisterCancelChannel removes the cancel channel mapping.
func UnregisterCancelChannel(id string) {
	cancelChannelsMu.Lock()
	delete(cancelChannels, id)
	cancelChannelsMu.Unlock()
}

// AddPendingAccept adds a channel waiting for user authorization.
func AddPendingAccept(id string, ch chan bool) {
	pendingAcceptsMu.Lock()
	pendingAccepts[id] = ch
	pendingAcceptsMu.Unlock()
}

// ResolvePendingAccept completes the pending accept authorization.
func ResolvePendingAccept(id string, accept bool) {
	pendingAcceptsMu.Lock()
	ch, ok := pendingAccepts[id]
	if ok {
		delete(pendingAccepts, id)
	}
	pendingAcceptsMu.Unlock()

	if ok && ch != nil {
		ch <- accept
		close(ch)
	}
}

// ScanFolder recursively collects all files within a folder, preserving structure relative to parent.
func ScanFolder(folderPath string) ([]models.TransferItem, error) {
	var items []models.TransferItem

	err := filepath.Walk(folderPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil // We only transfer file payloads; directories are created as needed
		}

		items = append(items, models.TransferItem{
			Path: path,
			Name: info.Name(),
			Size: info.Size(),
			Type: "file",
		})
		return nil
	})

	return items, err
}

// GetResumeOffset checks if a partial .part file exists for a transfer item and returns its size.
func GetResumeOffset(transferID string, relPath string) int64 {
	home, _ := os.UserHomeDir()
	resumeDir := filepath.Join(home, ".p2p-transfer", "resume", transferID)
	partPath := filepath.Join(resumeDir, relPath + ".part")
	
	info, err := os.Stat(partPath)
	if err == nil {
		return info.Size()
	}
	return 0
}

// CleanResumeDirectory cleans up the temp resume directory upon completion.
func CleanResumeDirectory(transferID string) {
	home, _ := os.UserHomeDir()
	resumeDir := filepath.Join(home, ".p2p-transfer", "resume", transferID)
	os.RemoveAll(resumeDir)
}

// Helper to track speed, ETA and progress
type ProgressTracker struct {
	transferID     string
	totalSize      int64
	bytesPrev      int64
	bytesCur       int64
	mu             sync.Mutex
	lastUpdateTime time.Time
}

func NewProgressTracker(transferID string, totalSize int64, initialBytes int64) *ProgressTracker {
	return &ProgressTracker{
		transferID:     transferID,
		totalSize:      totalSize,
		bytesPrev:      initialBytes,
		bytesCur:       initialBytes,
		lastUpdateTime: time.Now(),
	}
}

func (pt *ProgressTracker) Update(bytesRead int) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	pt.bytesCur += int64(bytesRead)
	
	now := time.Now()
	dur := now.Sub(pt.lastUpdateTime)
	if dur >= 1*time.Second {
		elapsed := dur.Seconds()
		diff := pt.bytesCur - pt.bytesPrev
		speed := float64(diff) / elapsed
		
		var eta int64 = 99999
		if speed > 0 {
			eta = int64(float64(pt.totalSize-pt.bytesCur) / speed)
		}

		progress := 0.0
		if pt.totalSize > 0 {
			progress = (float64(pt.bytesCur) / float64(pt.totalSize)) * 100.0
		}
		if progress > 100.0 {
			progress = 100.0
		}

		UpdateTransferProgress(pt.transferID, pt.bytesCur, speed, eta, progress)
		
		pt.bytesPrev = pt.bytesCur
		pt.lastUpdateTime = now
	}
}

func (pt *ProgressTracker) Finish() {
	UpdateTransferProgress(pt.transferID, pt.totalSize, 0, 0, 100.0)
}
