package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/nats-io/nats.go"
)

const (
	pingInterval = 30 * time.Second
	pongWait     = 10 * time.Second
)

// Handler manages WebSocket connections and typing debounce.
type Handler struct {
	Hub  *Hub
	NATS *nats.Conn

	typingMu       sync.Mutex
	typingDebounce map[string]time.Time // key: chatID+userID -> last broadcast
}

func NewHandler(hub *Hub, nc *nats.Conn) *Handler {
	return &Handler{
		Hub:            hub,
		NATS:           nc,
		typingDebounce: make(map[string]time.Time),
	}
}

// Upgrade returns a Fiber middleware that checks for WebSocket upgrade.
func (h *Handler) Upgrade() fiber.Handler {
	return websocket.New(func(c *websocket.Conn) {
		userID := c.Locals("user_id")
		if userID == nil {
			c.Close()
			return
		}
		uid, ok := userID.(string)
		if !ok || uid == "" {
			c.Close()
			return
		}

		conn := &Conn{
			WS:     c,
			UserID: uid,
			done:   make(chan struct{}),
		}

		h.Hub.Register(conn)
		defer func() {
			h.Hub.Unregister(conn)
			close(conn.done)
			h.publishStatusChange(uid, "offline")
		}()

		h.publishStatusChange(uid, "online")

		// Set pong handler
		c.SetPongHandler(func(string) error {
			return c.SetReadDeadline(time.Now().Add(pingInterval + pongWait))
		})

		// Start ping loop
		go h.pingLoop(conn)

		// Read loop
		c.SetReadDeadline(time.Now().Add(pingInterval + pongWait))
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				break
			}
			h.handleClientMessage(conn, msg)
		}
	})
}

func (h *Handler) pingLoop(conn *Conn) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			conn.mu.Lock()
			err := conn.WS.WriteMessage(websocket.PingMessage, nil)
			conn.mu.Unlock()
			if err != nil {
				return
			}
		case <-conn.done:
			return
		}
	}
}

func (h *Handler) handleClientMessage(conn *Conn, msg []byte) {
	var cm ClientMessage
	if err := json.Unmarshal(msg, &cm); err != nil {
		return
	}

	switch cm.Type {
	case "typing":
		h.handleTyping(conn, cm.Data)
	case "ping":
		conn.Send(Envelope{Type: EventPong, Data: json.RawMessage(`{}`)})
	}
}

func (h *Handler) handleTyping(conn *Conn, data json.RawMessage) {
	var td TypingData
	if err := json.Unmarshal(data, &td); err != nil || td.ChatID == "" {
		return
	}

	// Debounce: max 1 broadcast per 3s per user per chat
	key := td.ChatID + ":" + conn.UserID
	h.typingMu.Lock()
	last, exists := h.typingDebounce[key]
	now := time.Now()
	if exists && now.Sub(last) < 3*time.Second {
		h.typingMu.Unlock()
		return
	}
	h.typingDebounce[key] = now
	h.typingMu.Unlock()

	// Publish typing event to NATS
	event := NATSEvent{
		Event:     EventTyping,
		Data:      data,
		Timestamp: now.Format(time.RFC3339),
	}
	eventJSON, _ := json.Marshal(event)
	subject := "orbit.chat." + td.ChatID + ".typing"
	if err := h.NATS.Publish(subject, eventJSON); err != nil {
		slog.Error("failed to publish typing", "error", err)
	}
}

func (h *Handler) publishStatusChange(userID, status string) {
	sd := StatusData{
		UserID: userID,
		Status: status,
	}
	if status == "offline" {
		sd.LastSeen = time.Now().Format(time.RFC3339)
	}
	data, _ := json.Marshal(sd)
	event := NATSEvent{
		Event:     EventUserStatus,
		Data:      data,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	eventJSON, _ := json.Marshal(event)
	subject := "orbit.user." + userID + ".status"
	if err := h.NATS.Publish(subject, eventJSON); err != nil {
		slog.Error("failed to publish status", "error", err)
	}
}
