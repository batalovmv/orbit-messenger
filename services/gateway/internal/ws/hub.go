package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
)

const writeTimeout = 10 * time.Second

// Conn represents a single WebSocket connection.
type Conn struct {
	WS      *websocket.Conn
	UserID  string
	mu      sync.Mutex
	done    chan struct{}
	sendFn  func(interface{}) error
	closeFn func(code int, text string) error

	tokenExpiry  time.Time
	tokenHash    *string
	revalidateFn func(context.Context) error
	closeOnce    sync.Once
	closeErr     error

	// Per-connection typing rate limit fields (protected by mu)
	lastTyping  time.Time
	typingBurst int
}

// Send sends a JSON message to the client, thread-safe.
func (c *Conn) Send(msg interface{}) error {
	if c.sendFn != nil {
		return c.sendFn(msg)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.WS.SetWriteDeadline(time.Now().Add(writeTimeout))
	return c.WS.WriteMessage(websocket.TextMessage, data)
}

func (c *Conn) Done() <-chan struct{} {
	return c.done
}

func (c *Conn) TokenExpiry() time.Time {
	return c.tokenExpiry
}

func (c *Conn) Revalidate(ctx context.Context) error {
	if c.revalidateFn == nil {
		return nil
	}
	return c.revalidateFn(ctx)
}

func (c *Conn) Close(code int, text string) error {
	c.closeOnce.Do(func() {
		if c.closeFn != nil {
			c.closeErr = c.closeFn(code, text)
			return
		}
		if c.WS == nil {
			return
		}

		c.mu.Lock()
		defer c.mu.Unlock()
		c.WS.SetWriteDeadline(time.Now().Add(writeTimeout))
		_ = c.WS.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, text))
		c.closeErr = c.WS.Close()
	})
	return c.closeErr
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

// Register adds a connection to the hub. Returns true if this is the first connection for the user.
func (h *Hub) Register(conn *Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	isFirst := len(h.conns[conn.UserID]) == 0
	h.conns[conn.UserID] = append(h.conns[conn.UserID], conn)
	slog.Info("ws: user connected", "user_id", conn.UserID, "total", len(h.conns[conn.UserID]))
	return isFirst
}

// Unregister removes a connection from the hub. Returns true if this was the last connection for the user.
func (h *Hub) Unregister(conn *Conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	conns := h.conns[conn.UserID]
	for i, c := range conns {
		if c == conn {
			h.conns[conn.UserID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	isLast := len(h.conns[conn.UserID]) == 0
	if isLast {
		delete(h.conns, conn.UserID)
	}
	slog.Info("ws: user disconnected", "user_id", conn.UserID)
	return isLast
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
	src := h.conns[userID]
	// Deep copy to avoid data race with concurrent Unregister
	snapshot := make([]*Conn, len(src))
	copy(snapshot, src)
	h.mu.RUnlock()

	for _, c := range snapshot {
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
