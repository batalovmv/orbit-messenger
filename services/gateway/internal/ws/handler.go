package ws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

const (
	pingInterval   = 30 * time.Second
	pongWait       = 10 * time.Second
	authTimeout    = 10 * time.Second
	authCacheTTL   = 30 * time.Second
	typingCleanup  = 60 * time.Second
)

const typingExpire = 6 * time.Second

// Handler manages WebSocket connections and typing debounce.
type Handler struct {
	Hub  *Hub
	NATS *nats.Conn

	typingMu       sync.Mutex
	typingDebounce map[string]time.Time   // key: chatID+userID -> last broadcast
	typingTimers   map[string]*time.Timer // key: chatID+userID -> auto stop_typing timer

	done chan struct{}
}

func NewHandler(hub *Hub, nc *nats.Conn) *Handler {
	h := &Handler{
		Hub:            hub,
		NATS:           nc,
		typingDebounce: make(map[string]time.Time),
		typingTimers:   make(map[string]*time.Timer),
		done:           make(chan struct{}),
	}
	// Periodically clean stale typing entries (#13 memory leak fix)
	go h.typingCleanupLoop()
	return h
}

// Close stops the typingCleanupLoop goroutine and cancels all pending typing timers.
func (h *Handler) Close() {
	close(h.done)
	h.typingMu.Lock()
	for k, t := range h.typingTimers {
		t.Stop()
		delete(h.typingTimers, k)
	}
	h.typingMu.Unlock()
}

// Upgrade returns a Fiber handler that upgrades to WebSocket.
// Auth happens via the first "auth" frame — token is NOT in the URL.
func (h *Handler) Upgrade(authServiceURL string, rdb *redis.Client) fiber.Handler {
	authClient := &http.Client{Timeout: authTimeout}

	return websocket.New(func(c *websocket.Conn) {
		// Step 1: Wait for "auth" frame with token
		c.SetReadDeadline(time.Now().Add(authTimeout))
		_, msg, err := c.ReadMessage()
		if err != nil {
			slog.Warn("ws: no auth frame received", "error", err)
			c.WriteJSON(Envelope{Type: "error", Data: json.RawMessage(`{"message":"auth timeout"}`)})
			c.Close()
			return
		}

		var cm ClientMessage
		if err := json.Unmarshal(msg, &cm); err != nil || cm.Type != "auth" {
			c.WriteJSON(Envelope{Type: "error", Data: json.RawMessage(`{"message":"expected auth frame"}`)})
			c.Close()
			return
		}

		var authData struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(cm.Data, &authData); err != nil || authData.Token == "" {
			c.WriteJSON(Envelope{Type: "error", Data: json.RawMessage(`{"message":"missing token"}`)})
			c.Close()
			return
		}

		// Clear the auth-frame read deadline before the HTTP call to auth service,
		// so a slow auth response doesn't kill the WS connection.
		c.SetReadDeadline(time.Time{})

		// Step 2: Validate token via auth service (with Redis cache)
		// Bounded to authTimeout (10s). Note: context.Background() does not cancel on
		// client disconnect — Fiber's WebSocket handler doesn't expose a request context.
		// The timeout bounds the worst-case resource waste per stalled auth attempt.
		authCtx, authCancel := context.WithTimeout(context.Background(), authTimeout)
		uid, err := validateToken(authCtx, authClient, rdb, authServiceURL, authData.Token)
		authCancel()
		if err != nil || uid == "" {
			c.WriteJSON(Envelope{Type: "error", Data: json.RawMessage(`{"message":"invalid token"}`)})
			c.Close()
			return
		}

		// Step 3: Auth success — send confirmation
		c.WriteJSON(Envelope{Type: "auth_ok", Data: json.RawMessage(`{}`)})

		conn := &Conn{
			WS:     c,
			UserID: uid,
			done:   make(chan struct{}),
		}

		// Atomic check-and-register to prevent duplicate "online" events on multi-device race
		isFirstConnection := h.Hub.Register(conn)
		defer func() {
			isLastConnection := h.Hub.Unregister(conn)
			close(conn.done)
			if isLastConnection {
				h.publishStatusChange(uid, "offline")
			}
		}()

		if isFirstConnection {
			h.publishStatusChange(uid, "online")
		}

		// Set pong handler
		c.SetPongHandler(func(string) error {
			return c.SetReadDeadline(time.Now().Add(pingInterval + pongWait))
		})

		// Start ping loop
		go h.pingLoop(conn)

		// Read loop
		c.SetReadLimit(64 * 1024) // 64KB max WS message size — prevents memory exhaustion
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

// validateToken checks JWT via auth service with Redis cache.
// Mirrors the blacklist check from middleware/jwt.go to prevent revoked tokens from authenticating WS.
func validateToken(ctx context.Context, client *http.Client, rdb *redis.Client, authURL, token string) (string, error) {
	tokenHash := sha256Hash(token)
	cacheKey := "jwt_cache:" + tokenHash
	blacklistKey := "jwt_blacklist:" + tokenHash

	// Check blacklist first — fail-closed on Redis error
	blacklisted, blErr := rdb.Exists(ctx, blacklistKey).Result()
	if blErr != nil {
		slog.Error("WS blacklist check failed, rejecting token", "error", blErr)
		return "", fmt.Errorf("blacklist check failed")
	}
	if blacklisted > 0 {
		if err := rdb.Del(ctx, cacheKey).Err(); err != nil {
			slog.Error("WS JWT cache del failed after blacklist hit", "error", err)
		}
		return "", nil
	}

	// Check cache
	if cached, err := rdb.Get(ctx, cacheKey).Result(); err == nil {
		var u struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(cached), &u) == nil && u.ID != "" {
			return u.ID, nil
		}
	}

	// Call auth service
	req, err := http.NewRequestWithContext(ctx, "GET", authURL+"/auth/me", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}

	var user struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return "", err
	}

	// Cache
	cuJSON, err := json.Marshal(user)
	if err != nil {
		slog.Error("WS JWT cache marshal failed", "error", err)
	} else if err := rdb.Set(ctx, cacheKey, string(cuJSON), authCacheTTL).Err(); err != nil {
		slog.Error("WS JWT cache write failed", "error", err)
	}

	return user.ID, nil
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func (h *Handler) pingLoop(conn *Conn) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			conn.mu.Lock()
			conn.WS.SetWriteDeadline(time.Now().Add(writeTimeout))
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
	// Validate ChatID is a proper UUID — prevents NATS subject injection
	if _, err := uuid.Parse(td.ChatID); err != nil {
		return
	}

	// Global per-connection rate limit: max 10 typing events per second across all chats
	conn.mu.Lock()
	now := time.Now()
	if now.Sub(conn.lastTyping) < 100*time.Millisecond {
		conn.typingBurst++
		if conn.typingBurst > 10 {
			conn.mu.Unlock()
			return
		}
	} else {
		conn.typingBurst = 0
	}
	conn.lastTyping = now
	conn.mu.Unlock()

	// Debounce: max 1 broadcast per 3s per user per chat
	key := td.ChatID + ":" + conn.UserID
	h.typingMu.Lock()
	last, exists := h.typingDebounce[key]
	if exists && now.Sub(last) < 3*time.Second {
		h.typingMu.Unlock()
		return
	}
	h.typingDebounce[key] = now
	h.typingMu.Unlock()

	// Publish typing event to NATS
	typingData, _ := json.Marshal(map[string]string{
		"chat_id": td.ChatID,
		"user_id": conn.UserID,
	})
	event := NATSEvent{
		Event:     EventTyping,
		Data:      typingData,
		Timestamp: now.Format(time.RFC3339),
	}
	eventJSON, _ := json.Marshal(event)
	subject := "orbit.chat." + td.ChatID + ".typing"
	if err := h.NATS.Publish(subject, eventJSON); err != nil {
		slog.Error("failed to publish typing", "error", err)
	}

	// Auto-expire: send stop_typing after 6s if no new typing received
	h.typingMu.Lock()
	if timer, ok := h.typingTimers[key]; ok {
		timer.Stop()
	}
	chatID := td.ChatID
	userID := conn.UserID
	h.typingTimers[key] = time.AfterFunc(typingExpire, func() {
		// Check if handler is closing to prevent nil-map panic
		select {
		case <-h.done:
			return
		default:
		}

		stopData, _ := json.Marshal(map[string]string{
			"chat_id": chatID,
			"user_id": userID,
		})
		stopEvt := NATSEvent{
			Event:     EventStopTyping,
			Data:      stopData,
			Timestamp: time.Now().Format(time.RFC3339),
		}
		stopJSON, _ := json.Marshal(stopEvt)
		stopSubject := "orbit.chat." + chatID + ".typing"
		if err := h.NATS.Publish(stopSubject, stopJSON); err != nil {
			slog.Error("failed to publish stop_typing", "error", err)
		}

		h.typingMu.Lock()
		if h.typingTimers != nil {
			delete(h.typingTimers, chatID+":"+userID)
		}
		h.typingMu.Unlock()
	})
	h.typingMu.Unlock()
}

// typingCleanupLoop periodically removes stale entries from typingDebounce map.
func (h *Handler) typingCleanupLoop() {
	ticker := time.NewTicker(typingCleanup)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			h.typingMu.Lock()
			now := time.Now()
			for k, v := range h.typingDebounce {
				if now.Sub(v) > typingCleanup {
					delete(h.typingDebounce, k)
				}
			}
			h.typingMu.Unlock()
		case <-h.done:
			return
		}
	}
}

func (h *Handler) publishStatusChange(userID, status string) {
	// Defense-in-depth: validate userID format to prevent NATS subject injection
	if _, err := uuid.Parse(userID); err != nil {
		slog.Error("invalid userID for status change", "user_id", userID)
		return
	}
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
