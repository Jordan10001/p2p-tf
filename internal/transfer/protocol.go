package transfer

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"p2p-transfer/internal/models"
)

// HandshakeRequest is sent by the sender immediately after connecting.
type HandshakeRequest struct {
	TransferID string                `json:"transferId"`
	SenderID   string                `json:"senderId"`
	SenderName string                `json:"senderName"`
	TotalSize  int64                 `json:"totalSize"`
	Files      []models.TransferItem `json:"files"`
}

// HandshakeResponse is sent by the receiver to approve, reject, or resume the transfer.
type HandshakeResponse struct {
	Status  string           `json:"status"` // "accepted", "rejected", "error"
	Message string           `json:"message,omitempty"`
	Offsets map[string]int64 `json:"offsets,omitempty"` // Filename relative path -> bytes already received
}

// FileHeader is sent before streaming each file.
type FileHeader struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Offset int64  `json:"offset"`
}

// FileHeaderResponse tells the sender whether to proceed or skip.
type FileHeaderResponse struct {
	Status string `json:"status"` // "ok", "skip", "error"
}

// WriteJSONPacket writes a length-prefixed JSON packet to the connection.
func WriteJSONPacket(w io.Writer, val interface{}) error {
	data, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	size := uint32(len(data))
	if err := binary.Write(w, binary.BigEndian, size); err != nil {
		return fmt.Errorf("failed to write packet size: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("failed to write packet data: %w", err)
	}

	return nil
}

// ReadJSONPacket reads a length-prefixed JSON packet from the connection.
func ReadJSONPacket(r io.Reader, val interface{}) error {
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return fmt.Errorf("failed to read packet size: %w", err)
	}

	// Safety: prevent memory exhaustion if malicious client sends large size
	if size > 16*1024*1024 {
		return fmt.Errorf("packet size too large: %d bytes", size)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(r, data); err != nil {
		return fmt.Errorf("failed to read packet body: %w", err)
	}

	if err := json.Unmarshal(data, val); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}
