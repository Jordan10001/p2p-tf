package models

import "time"

// Settings holds all the user customizable configurations.
type Settings struct {
	DeviceID                         string `json:"deviceId"`
	DeviceName                       string `json:"deviceName"`
	Hostname                       string `json:"hostname"`
	TransferPort                   int    `json:"transferPort"`
	DownloadDir                    string `json:"downloadDir"`
	ShowNotifications              bool   `json:"showNotifications"`
	ShowTransferCompleteNotification bool   `json:"showTransferCompleteNotification"`
	AutoOpenFolder                 bool   `json:"autoOpenFolder"`
	AutoAccept                     bool   `json:"autoAccept"`
	EnableDiscovery                bool   `json:"enableDiscovery"`
	AutoScan                       bool   `json:"autoScan"`
	MinimizeToTray                 bool   `json:"minimizeToTray"`
	StartWithWindows               bool   `json:"startWithWindows"`
}

// Device represents a peer on the local network.
type Device struct {
	ID        string    `json:"id"`
	Hostname  string    `json:"hostname"`
	Name      string    `json:"name"`
	IP        string    `json:"ip"`
	Port      int       `json:"port"`
	Status    string    `json:"status"` // "online", "offline", "unavailable", "transferring"
	LastSeen  time.Time `json:"lastSeen"`
}

// TransferItem represents an individual file or directory item being transferred.
type TransferItem struct {
	Path string `json:"path"` // Rel path from selection root
	Name string `json:"name"`
	Size int64  `json:"size"`
	Type string `json:"type"`
}

// Transfer represents a file transfer operation in progress or finished.
type Transfer struct {
	ID         string         `json:"id"`
	Direction  string         `json:"direction"` // "send" or "receive"
	PeerID     string         `json:"peerId"`
	PeerName   string         `json:"peerName"`
	PeerIP     string         `json:"peerIp"`
	Files      []TransferItem `json:"files"`
	TotalSize  int64          `json:"totalSize"`
	BytesTrans int64          `json:"bytesTransferred"`
	Speed      float64        `json:"speed"`  // bytes per second
	ETA        int64          `json:"eta"`    // seconds remaining
	Status     string         `json:"status"` // "waiting", "preparing", "sending", "receiving", "completed", "failed", "cancelled", "paused", "resuming"
	Progress   float64        `json:"progress"`
	Timestamp  time.Time      `json:"timestamp"`
	Error      string         `json:"error,omitempty"`
}

// HistoryEntry represents a record in the transfer history log.
type HistoryEntry struct {
	ID        string    `json:"id"`
	FileName  string    `json:"fileName"`
	Sender    string    `json:"sender"`
	Receiver  string    `json:"receiver"`
	Size      int64     `json:"size"`
	Timestamp time.Time `json:"timestamp"`
	Status    string    `json:"status"` // "completed", "failed", "cancelled"
}

// WSMessage represents a websocket message structure.
type WSMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}
