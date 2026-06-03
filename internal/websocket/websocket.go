package websocket

import (
	"encoding/json"
	"net/http"
	"sync"
	"p2p-transfer/internal/logger"
	"p2p-transfer/internal/models"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow connection from the web dashboard
	},
}

// Client represents a connected websocket user.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// Hub manages all active client connections.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	handlers   map[string]func(payload json.RawMessage, client *Client)
	handlersMu sync.RWMutex
}

var globalHub = &Hub{
	clients:    make(map[*Client]bool),
	broadcast:  make(chan []byte),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	handlers:   make(map[string]func(payload json.RawMessage, client *Client)),
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			logger.Info("WebSocket client connected")
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				logger.Info("WebSocket client disconnected")
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			break
		}

		var wsMsg struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}

		if err := json.Unmarshal(message, &wsMsg); err != nil {
			logger.Error("Failed to parse incoming WebSocket message: %v", err)
			continue
		}

		c.hub.handlersMu.RLock()
		handler, ok := c.hub.handlers[wsMsg.Type]
		c.hub.handlersMu.RUnlock()

		if ok {
			go handler(wsMsg.Payload, c)
		} else {
			logger.Warn("No handler registered for WebSocket message type: %s", wsMsg.Type)
		}
	}
}

func (c *Client) writePump() {
	defer func() {
		c.conn.Close()
	}()
	for {
		message, ok := <-c.send
		if !ok {
			c.conn.WriteMessage(websocket.CloseMessage, []byte{})
			return
		}
		err := c.conn.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			return
		}
	}
}

// Send pushes raw bytes to the client's write channel in a non-blocking way.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		logger.Warn("Failed to send message: client queue full")
	}
}

// Start launches the WebSocket server on the specified port.
func Start(addr string) {
	go globalHub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Error("Failed to upgrade HTTP connection to WebSocket: %v", err)
			return
		}

		client := &Client{
			hub:  globalHub,
			conn: conn,
			send: make(chan []byte, 256),
		}
		client.hub.register <- client

		go client.writePump()
		go client.readPump()
		
		// Send initial configuration and setup data
		TriggerInitialData(client)
	})

	logger.Info("WebSocket server starting on %s", addr)
	go func() {
		if err := http.ListenAndServe(addr, nil); err != nil {
			logger.Error("WebSocket server error: %v", err)
		}
	}()
}

// Broadcast sends a message to all connected WebSocket clients.
func Broadcast(msgType string, payload interface{}) {
	msg := models.WSMessage{
		Type:    msgType,
		Payload: payload,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Error("Failed to marshal broadcast message: %v", err)
		return
	}

	globalHub.broadcast <- data
}

// RegisterHandler binds a callback to a specific incoming WebSocket message type.
func RegisterHandler(msgType string, handler func(payload json.RawMessage, client *Client)) {
	globalHub.handlersMu.Lock()
	defer globalHub.handlersMu.Unlock()
	globalHub.handlers[msgType] = handler
}

// TriggerInitialData registers a callback to send initial data when a new client connects.
var TriggerInitialData = func(client *Client) {
	// Implemented by the orchestrator in main to avoid import cycles.
}
