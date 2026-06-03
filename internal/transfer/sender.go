package transfer

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
	"p2p-transfer/internal/config"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"
	"p2p-transfer/internal/websocket"

	"github.com/google/uuid"
)

// StartSendTransfer initiates a file transfer to a remote device.
func StartSendTransfer(peer models.Device, items []models.TransferItem) {
	go func() {
		cfg := config.Get()
		transferID := uuid.New().String()

		// 1. Resolve files (expand directories if any)
		var resolvedFiles []models.TransferItem
		var totalSize int64 = 0
		relativePaths := make(map[string]string) // Key: absolute path, Value: network relative path

		for _, item := range items {
			info, err := os.Stat(item.Path)
			if err != nil {
				logger.Error("Failed to stat file %s: %v", item.Path, err)
				continue
			}

			if info.IsDir() {
				folderFiles, err := ScanFolder(item.Path)
				if err != nil {
					logger.Error("Failed to scan directory %s: %v", item.Path, err)
					continue
				}
				resolvedFiles = append(resolvedFiles, folderFiles...)
				baseFolder := filepath.Dir(item.Path)
				for _, f := range folderFiles {
					totalSize += f.Size
					relPath, err := filepath.Rel(baseFolder, f.Path)
					if err != nil {
						relPath = f.Name
					}
					relativePaths[f.Path] = relPath
				}
			} else {
				resolvedFiles = append(resolvedFiles, models.TransferItem{
					Path: item.Path,
					Name: item.Name,
					Size: item.Size,
					Type: "file",
				})
				totalSize += item.Size
				// For a single file, the transmission path is just its name!
				relativePaths[item.Path] = item.Name
			}
		}

		if len(resolvedFiles) == 0 {
			logger.Warn("No valid files to send to %s", peer.Name)
			websocket.Broadcast("toast", map[string]string{
				"type":    "error",
				"message": "No valid files selected for transfer",
			})
			return
		}

		// Prepare files list for the network handshake using relative/transmission paths
		networkFiles := make([]models.TransferItem, len(resolvedFiles))
		for i, f := range resolvedFiles {
			networkFiles[i] = f
			networkFiles[i].Path = relativePaths[f.Path]
		}

		// Initialize Transfer model using the relative paths for standard tracking
		transferModel := &models.Transfer{
			ID:         transferID,
			Direction:  "send",
			PeerID:     peer.ID,
			PeerName:   peer.Name,
			PeerIP:     peer.IP,
			Files:      networkFiles,
			TotalSize:  totalSize,
			BytesTrans: 0,
			Status:     "waiting",
			Progress:   0.0,
			Timestamp:  time.Now(),
		}

		RegisterActiveTransfer(transferModel)
		logger.Info("Starting transfer sender %s to %s (%s)", transferID, peer.Name, peer.IP)

		// 2. Connect to receiver
		addr := fmt.Sprintf("%s:%d", peer.IP, peer.Port)
		UpdateTransferStatus(transferID, "preparing", "")

		conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Failed to connect to peer: %v", err))
			return
		}
		defer conn.Close()

		registerConn(transferID, conn)
		defer unregisterConn(transferID)

		// Setup local cancellation handler
		cancelCh := make(chan struct{})
		RegisterCancelChannel(transferID, cancelCh)
		defer UnregisterCancelChannel(transferID)

		go func() {
			select {
			case <-cancelCh:
				conn.Close()
			case <-time.After(1 * time.Hour): // safety timeout
			}
		}()

		// 3. Send handshake request with clean network relative paths
		req := HandshakeRequest{
			TransferID: transferID,
			SenderID:   cfg.DeviceID,
			SenderName: cfg.DeviceName,
			TotalSize:  totalSize,
			Files:      networkFiles,
		}

		if err := WriteJSONPacket(conn, req); err != nil {
			UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Handshake request failed: %v", err))
			return
		}

		// 4. Read handshake response
		var resp HandshakeResponse
		if err := ReadJSONPacket(conn, &resp); err != nil {
			UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Failed to read peer handshake response: %v", err))
			return
		}

		if resp.Status != "accepted" {
			errMsg := resp.Message
			if errMsg == "" {
				errMsg = "Declined by recipient"
			}
			UpdateTransferStatus(transferID, "failed", errMsg)
			return
		}

		// Calculate initial bytes transferred based on resume offsets
		var initialBytes int64 = 0
		if resp.Offsets != nil {
			for _, offset := range resp.Offsets {
				initialBytes += offset
			}
		}

		transferModel.BytesTrans = initialBytes
		if totalSize > 0 {
			transferModel.Progress = (float64(initialBytes) / float64(totalSize)) * 100.0
		}
		UpdateTransferStatus(transferID, "sending", "")

		progressTracker := NewProgressTracker(transferID, totalSize, initialBytes)

		// 5. Send files chunk by chunk
		for _, file := range resolvedFiles {
			offset := int64(0)
			relPath := relativePaths[file.Path]
			if resp.Offsets != nil {
				if o, ok := resp.Offsets[relPath]; ok {
					offset = o
				}
			}

			// Open local file using absolute path
			localFile, err := os.Open(file.Path)
			if err != nil {
				UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Failed to open local file %s: %v", file.Name, err))
				return
			}

			// Seek to the required offset for resuming
			if offset > 0 {
				_, err = localFile.Seek(offset, io.SeekStart)
				if err != nil {
					localFile.Close()
					UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Failed to seek file: %v", err))
					return
				}
			}

			// Write file header to connection using clean relative path
			fileHeader := FileHeader{
				Path:   relPath,
				Size:   file.Size,
				Offset: offset,
			}

			if err := WriteJSONPacket(conn, fileHeader); err != nil {
				localFile.Close()
				UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Failed to send file header: %v", err))
				return
			}

			// Read file header response
			var headerResp FileHeaderResponse
			if err := ReadJSONPacket(conn, &headerResp); err != nil {
				localFile.Close()
				UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Failed to read header response: %v", err))
				return
			}

			if headerResp.Status != "ok" {
				localFile.Close()
				UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Receiver rejected file: %s", file.Name))
				return
			}

			// Stream bytes
			remainingBytes := file.Size - offset
			buffer := make([]byte, 256*1024) // 256KB chunks

			var sent int64 = 0
			for sent < remainingBytes {
				toRead := int64(len(buffer))
				if remainingBytes-sent < toRead {
					toRead = remainingBytes - sent
				}

				n, err := localFile.Read(buffer[:toRead])
				if n > 0 {
					_, writeErr := conn.Write(buffer[:n])
					if writeErr != nil {
						localFile.Close()
						UpdateTransferStatus(transferID, "failed", fmt.Sprintf("Failed to send data: %v", writeErr))
						return
					}
					sent += int64(n)
					progressTracker.Update(n)
				}
				if err != nil {
					localFile.Close()
					if err != io.EOF {
						UpdateTransferStatus(transferID, "failed", fmt.Sprintf("File read failed: %v", err))
						return
					}
					break
				}
			}

			localFile.Close()
		}

		progressTracker.Finish()
		UpdateTransferStatus(transferID, "completed", "")
		logger.Info("Transfer sender completed for ID: %s", transferID)

		if cfg.ShowTransferCompleteNotification {
			websocket.Broadcast("toast", map[string]string{
				"type":    "success",
				"message": fmt.Sprintf("Successfully sent %d items to %s", len(items), peer.Name),
			})
		}
	}()
}
