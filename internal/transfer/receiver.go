package transfer

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"p2p-transfer/internal/config"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"
	"p2p-transfer/internal/utils"
	"p2p-transfer/internal/websocket"
)

var (
	tcpListener       net.Listener
	tcpListenerMu     sync.Mutex
	activeConnections = make(map[string]net.Conn)
	connectionsMu     sync.Mutex
)

// StartTCPReceiver runs the TCP server for accepting file transfers.
func StartTCPReceiver(port int) error {
	tcpListenerMu.Lock()
	defer tcpListenerMu.Unlock()

	if tcpListener != nil {
		tcpListener.Close()
	}

	addr := fmt.Sprintf(":%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start TCP listener on port %d: %w", port, err)
	}
	tcpListener = l

	logger.Info("TCP Receiver started on %s", addr)

	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				// Listener was closed
				return
			}
			go handleIncomingConnection(conn)
		}
	}()

	return nil
}

// StopTCPReceiver shuts down the TCP receiver listener.
func StopTCPReceiver() {
	tcpListenerMu.Lock()
	defer tcpListenerMu.Unlock()

	if tcpListener != nil {
		tcpListener.Close()
		tcpListener = nil
		logger.Info("TCP Receiver stopped")
	}

	// Close all ongoing receiver connections
	connectionsMu.Lock()
	for id, conn := range activeConnections {
		conn.Close()
		delete(activeConnections, id)
	}
	connectionsMu.Unlock()
}

func registerConn(id string, conn net.Conn) {
	connectionsMu.Lock()
	activeConnections[id] = conn
	connectionsMu.Unlock()
}

func unregisterConn(id string) {
	connectionsMu.Lock()
	delete(activeConnections, id)
	connectionsMu.Unlock()
}

func handleIncomingConnection(conn net.Conn) {
	defer conn.Close()

	// 1. Read the handshake request
	var req HandshakeRequest
	if err := ReadJSONPacket(conn, &req); err != nil {
		logger.Error("Receiver handshake failed: %v", err)
		return
	}

	// Sanitize paths in the handshake request
	for i, f := range req.Files {
		cleanPath := f.Path
		if filepath.IsAbs(cleanPath) || filepath.VolumeName(cleanPath) != "" || strings.HasPrefix(cleanPath, "/") || strings.HasPrefix(cleanPath, "\\") {
			cleanPath = filepath.Base(cleanPath)
		} else {
			cleanPath = filepath.Clean(cleanPath)
		}
		req.Files[i].Path = cleanPath
	}

	registerConn(req.TransferID, conn)
	defer unregisterConn(req.TransferID)

	cfg := config.Get()

	// 2. Validate Peer IP
	remoteIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	logger.Info("Incoming connection from %s (%s)", req.SenderName, remoteIP)

	// 3. Determine if we should accept or ask
	accepted := false
	offsets := make(map[string]int64)

	// Gather resume offsets if we do accept
	gatherOffsets := func() {
		for _, file := range req.Files {
			offset := GetResumeOffset(req.TransferID, file.Path)
			if offset > 0 {
				offsets[file.Path] = offset
				logger.Info("Resuming %s from offset %d bytes", file.Path, offset)
			}
		}
	}

	if cfg.AutoAccept {
		accepted = true
		gatherOffsets()
	} else {
		// Ask user via WebSocket
		acceptChan := make(chan bool, 1)
		AddPendingAccept(req.TransferID, acceptChan)

		// Broadcast request to all clients
		websocket.Broadcast("incoming_request", map[string]interface{}{
			"transferId": req.TransferID,
			"peerId":     req.SenderID,
			"peerName":   req.SenderName,
			"peerIp":     remoteIP,
			"files":      req.Files,
			"totalSize":  req.TotalSize,
		})

		logger.Info("Waiting for user acceptance of transfer: %s", req.TransferID)

		// Wait with a timeout (e.g. 60 seconds)
		select {
		case approved := <-acceptChan:
			accepted = approved
			if accepted {
				gatherOffsets()
			}
		case <-time.After(60 * time.Second):
			ResolvePendingAccept(req.TransferID, false)
			accepted = false
			logger.Warn("Incoming transfer timed out waiting for authorization")
		}
	}

	// 4. Send handshake response
	if !accepted {
		resp := HandshakeResponse{Status: "rejected", Message: "Transfer request rejected"}
		WriteJSONPacket(conn, resp)
		logger.Info("Transfer request %s rejected", req.TransferID)
		return
	}

	resp := HandshakeResponse{
		Status:  "accepted",
		Offsets: offsets,
	}
	if err := WriteJSONPacket(conn, resp); err != nil {
		logger.Error("Failed to send handshake response: %v", err)
		return
	}

	// 5. Initialize the Transfer model
	transferModel := &models.Transfer{
		ID:         req.TransferID,
		Direction:  "receive",
		PeerID:     req.SenderID,
		PeerName:   req.SenderName,
		PeerIP:     remoteIP,
		Files:      req.Files,
		TotalSize:  req.TotalSize,
		BytesTrans: 0,
		Status:     "receiving",
		Progress:   0.0,
		Timestamp:  time.Now(),
	}

	// Pre-populate already received bytes in progress calculation
	var initialBytes int64 = 0
	for _, offset := range offsets {
		initialBytes += offset
	}
	transferModel.BytesTrans = initialBytes
	if req.TotalSize > 0 {
		transferModel.Progress = (float64(initialBytes) / float64(req.TotalSize)) * 100.0
	}

	RegisterActiveTransfer(transferModel)
	logger.Info("Starting transfer receiver for ID: %s", req.TransferID)

	// Setup tracking channel for local cancellation
	cancelCh := make(chan struct{})
	RegisterCancelChannel(req.TransferID, cancelCh)
	defer UnregisterCancelChannel(req.TransferID)

	// Run goroutine to handle local cancellation by closing connection
	go func() {
		select {
		case <-cancelCh:
			conn.Close()
		case <-time.After(1 * time.Hour): // safety timeout
		}
	}()

	progressTracker := NewProgressTracker(req.TransferID, req.TotalSize, initialBytes)

	// 6. Receive files chunk by chunk
	home, _ := os.UserHomeDir()
	resumeDir := filepath.Join(home, ".p2p-transfer", "resume", req.TransferID)

	for range req.Files {
		var fileHeader FileHeader
		if err := ReadJSONPacket(conn, &fileHeader); err != nil {
			UpdateTransferStatus(req.TransferID, "failed", fmt.Sprintf("Failed to read file header: %v", err))
			return
		}
		// Sanitize the path received from the peer to remove volume labels and leading slashes
		cleanPath := fileHeader.Path
		if filepath.IsAbs(cleanPath) || filepath.VolumeName(cleanPath) != "" || strings.HasPrefix(cleanPath, "/") || strings.HasPrefix(cleanPath, "\\") {
			cleanPath = filepath.Base(cleanPath)
		} else {
			cleanPath = filepath.Clean(cleanPath)
		}
		fileHeader.Path = cleanPath
		// Sanitize path to prevent traversal
		sanitizedPath, err := utils.SanitizePath(cfg.DownloadDir, fileHeader.Path)
		if err != nil {
			WriteJSONPacket(conn, FileHeaderResponse{Status: "error"})
			UpdateTransferStatus(req.TransferID, "failed", fmt.Sprintf("Security violation: %v", err))
			return
		}

		// Ensure parent directories exist
		if err := os.MkdirAll(filepath.Dir(sanitizedPath), 0755); err != nil {
			WriteJSONPacket(conn, FileHeaderResponse{Status: "error"})
			UpdateTransferStatus(req.TransferID, "failed", fmt.Sprintf("Failed to create directories: %v", err))
			return
		}

		// Check if we already finished this file in a previous session
		// (if size on disk == total size and no .part file)
		if _, err := os.Stat(sanitizedPath); err == nil && fileHeader.Offset == 0 {
			// If file exists and we started from 0 offset, rename it automatically
			dir := filepath.Dir(sanitizedPath)
			base := filepath.Base(sanitizedPath)
			uniqueName := utils.GetUniqueFileName(dir, base)
			sanitizedPath = filepath.Join(dir, uniqueName)
		}

		// Prepare partial file path
		partFileDir := filepath.Join(resumeDir, filepath.Dir(fileHeader.Path))
		os.MkdirAll(partFileDir, 0755)
		partFilePath := filepath.Join(resumeDir, fileHeader.Path+".part")

		// Open part file for writing/appending
		var partFile *os.File
		if fileHeader.Offset > 0 {
			partFile, err = os.OpenFile(partFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0644)
		} else {
			partFile, err = os.OpenFile(partFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		}

		if err != nil {
			WriteJSONPacket(conn, FileHeaderResponse{Status: "error"})
			UpdateTransferStatus(req.TransferID, "failed", fmt.Sprintf("Failed to create local file: %v", err))
			return
		}

		if err := WriteJSONPacket(conn, FileHeaderResponse{Status: "ok"}); err != nil {
			partFile.Close()
			logger.Error("Failed to write file header response: %v", err)
			return
		}

		// Stream content of this file
		remainingBytes := fileHeader.Size - fileHeader.Offset
		buffer := make([]byte, 256*1024) // 256KB buffer

		var written int64 = 0
		for written < remainingBytes {
			toRead := int64(len(buffer))
			if remainingBytes-written < toRead {
				toRead = remainingBytes - written
			}

			n, err := conn.Read(buffer[:toRead])
			if n > 0 {
				_, writeErr := partFile.Write(buffer[:n])
				if writeErr != nil {
					partFile.Close()
					UpdateTransferStatus(req.TransferID, "failed", fmt.Sprintf("Write to disk failed: %v", writeErr))
					return
				}
				written += int64(n)
				progressTracker.Update(n)
			}
			if err != nil {
				partFile.Close()
				if err == io.EOF && written < remainingBytes {
					UpdateTransferStatus(req.TransferID, "failed", "Connection lost prematurely")
				} else {
					UpdateTransferStatus(req.TransferID, "failed", fmt.Sprintf("Read failed: %v", err))
				}
				return
			}
		}

		partFile.Close()

		// Final rename from .part to target path
		// Check again in case target path was created in parallel
		dir := filepath.Dir(sanitizedPath)
		base := filepath.Base(sanitizedPath)
		finalUniqueName := utils.GetUniqueFileName(dir, base)
		finalPath := filepath.Join(dir, finalUniqueName)

		if err := os.Rename(partFilePath, finalPath); err != nil {
			UpdateTransferStatus(req.TransferID, "failed", fmt.Sprintf("Failed to rename partial file: %v", err))
			return
		}
	}

	progressTracker.Finish()
	CleanResumeDirectory(req.TransferID)
	UpdateTransferStatus(req.TransferID, "completed", "")

	logger.Info("Transfer receiver completed for ID: %s", req.TransferID)

	// Post transfer actions
	if cfg.ShowTransferCompleteNotification {
		websocket.Broadcast("toast", map[string]string{
			"type":    "success",
			"message": fmt.Sprintf("Successfully received %d items from %s", len(req.Files), req.SenderName),
		})
	}

	if cfg.AutoOpenFolder {
		// Open the download directory in Windows Explorer
		exec.Command("explorer", cfg.DownloadDir).Start()
	}
}
