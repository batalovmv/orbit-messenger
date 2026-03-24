package ws

import (
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/gofiber/contrib/websocket"
)

// Conn represents a single WebSocket connection.
type Conn struct {
	WS     *websocket.Conn
	UserID string
	mu     sync.Mutex
	done   chan struct{}
}

// Send sends a JSON message to the client, thread-safe.
func (c *Conn) Send(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.WS.WriteMessage(websocket.TextMessage, data)
}

// Hub manages WebSocket connections indexed by user ID.
type Hub struct {
	mu    sync.RWMutex
	conns map[string][]*Conn // userID -> connections (multi-device)
}

func NewHub() *Hub {
	return &Hub{
		conns: make(map[string][]*Conn),
	}
}

// Register adds a connection to the hub.
func (h *Hub) Register(conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.conns[conn.UserID] = append(h.conns[conn.UserID], conn)
	slog.Info("ws: user connected", "user_id", conn.UserID, "total", len(h.conns[conn.UserID]))
}

// Unregister removes a connection from the hub.
func (h *Hub) Unregister(conn *Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.conns[conn.UserID]
	for i, c := range conns {
		if c == conn {
			h.conns[conn.UserID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(h.conns[conn.UserID]) == 0 {
		delete(h.conns, conn.UserID)
	}
	slog.Info("ws: user disconnected", "user_id", conn.UserID)
}

// IsOnline checks if a user has any active connections.
func (h *Hub) IsOnline(userID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.conns[userID]) > 0
}

// SendToUser sends a message to all connections of a specific user.
func (h *Hub) SendToUser(userID string, msg interface{}) {
	h.mu.RLock()
	conns := h.conns[userID]
	h.mu.RUnlock()

	for _, c := range conns {
		if err := c.Send(msg); err != nil {
			slog.Error("ws: send error", "user_id", userID, "error", err)
		}
	}
}

// SendToUsers sends a message to multiple users (excluding one).
func (h *Hub) SendToUsers(userIDs []string, msg interface{}, excludeUserID string) {
	for _, uid := range userIDs {
		if uid == excludeUserID {
			continue
		}
		h.SendToUser(uid, msg)
	}
}

// OnlineUserIDs returns the list of currently connected user IDs.
func (h *Hub) OnlineUserIDs() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	return ids
}
