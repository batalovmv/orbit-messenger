// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
)

const writeTimeout = 10 * time.Second
const sendWriteTimeout = 5 * time.Second
const sendQueueCapacity = 256

var (
	errConnClosed    = errors.New("ws: connection closed")
	errSendQueueFull = errors.New("ws: send queue full")
)

// Conn represents a single WebSocket connection.
type Conn struct {
	WS *websocket.Conn
	// UserID identifies the authenticated user. SessionID identifies a single
	// device/tab so events that originate on this connection (e.g. read receipts
	// the user just performed locally) can be excluded from the cross-device
	// fanout. SessionID is opaque, generated client-side at app start; if the
	// client does not supply one, the auth handler assigns a server-side UUID.
	UserID    string
	SessionID string
	mu        sync.Mutex
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	sendFn  func(interface{}) error
	closeFn func(code int, text string) error
	send    chan interface{}

	tokenExpiry  time.Time
	tokenHash    *string
	revalidateFn func(context.Context) error
	closeOnce    sync.Once
	closeErr     error
	writerOnce   sync.Once

	// Per-connection typing rate limit fields (protected by mu)
	lastTyping  time.Time
	typingBurst int
}

// Send sends a JSON message to the client, thread-safe.
func (c *Conn) Send(msg interface{}) error {
	if c.send != nil {
		return c.enqueue(msg)
	}

	return c.write(msg)
}

func (c *Conn) enqueue(msg interface{}) error {
	select {
	case <-c.done:
		return errConnClosed
	default:
	}

	select {
	case c.send <- msg:
		return nil
	default:
		return errSendQueueFull
	}
}

func (c *Conn) write(msg interface{}) error {
	if c.sendFn != nil {
		return c.sendFn(msg)
	}

	if c.WS == nil {
		return errors.New("ws: connection not initialized")
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

func (c *Conn) Context() context.Context {
	return c.ctx
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

func (c *Conn) ensureWriter() {
	if c.send == nil {
		return
	}

	c.writerOnce.Do(func() {
		go c.writerLoop()
	})
}

func (c *Conn) writerLoop() {
	for {
		select {
		case <-c.done:
			c.drainSendQueue()
			return
		case msg := <-c.send:
			if err := c.writeQueued(msg); err != nil {
				closeText := "write error"
				var netErr net.Error
				if errors.As(err, &netErr) && netErr.Timeout() {
					closeText = "write timeout"
				}
				go func() {
					if closeErr := c.Close(closeCodePolicyViolation, closeText); closeErr != nil {
						slog.Warn("ws: close after write failure failed", "user_id", c.UserID, "error", closeErr)
					}
				}()
				return
			}
		}
	}
}

func (c *Conn) writeQueued(msg interface{}) error {
	if c.sendFn != nil {
		return c.sendFn(msg)
	}
	if c.WS == nil {
		return errConnClosed
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.WS.SetWriteDeadline(time.Now().Add(sendWriteTimeout))
	return c.WS.WriteMessage(websocket.TextMessage, data)
}

func (c *Conn) drainSendQueue() {
	for {
		select {
		case <-c.send:
		default:
			return
		}
	}
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
	mu          sync.RWMutex
	conns       map[string][]*Conn // userID -> connections (multi-device)
	onCountDiff func(delta int)    // optional metrics observer (+1/-1 per register/unregister)
}

func NewHub() *Hub {
	return &Hub{
		conns: make(map[string][]*Conn),
	}
}

// SetMetricsCallback wires a gauge-style observer. Kept optional so the
// hub does not depend on the metrics package directly.
func (h *Hub) SetMetricsCallback(cb func(delta int)) {
	h.mu.Lock()
	h.onCountDiff = cb
	h.mu.Unlock()
}

// Register adds a connection to the hub. Returns true if this is the first connection for the user.
func (h *Hub) Register(conn *Conn) bool {
	h.mu.Lock()
	if conn.send == nil && conn.WS != nil {
		conn.send = make(chan interface{}, sendQueueCapacity)
	}
	conn.ensureWriter()
	isFirst := len(h.conns[conn.UserID]) == 0
	h.conns[conn.UserID] = append(h.conns[conn.UserID], conn)
	cb := h.onCountDiff
	userTotal := len(h.conns[conn.UserID])
	h.mu.Unlock()
	if cb != nil {
		cb(1)
	}
	slog.Info("ws: user connected", "user_id", conn.UserID, "total", userTotal)
	return isFirst
}

// Unregister removes a connection from the hub. Returns true if this was the last connection for the user.
func (h *Hub) Unregister(conn *Conn) bool {
	h.mu.Lock()
	conns := h.conns[conn.UserID]
	removed := false
	for i, c := range conns {
		if c == conn {
			h.conns[conn.UserID] = append(conns[:i], conns[i+1:]...)
			removed = true
			break
		}
	}
	isLast := len(h.conns[conn.UserID]) == 0
	if isLast {
		delete(h.conns, conn.UserID)
	}
	cb := h.onCountDiff
	h.mu.Unlock()
	if cb != nil && removed {
		cb(-1)
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
			if errors.Is(err, errSendQueueFull) || errors.Is(err, errConnClosed) {
				// Disconnect instead of silently dropping fanout frames. Until TASK-46
				// lands JetStream replay, a slow client may briefly miss events and
				// must reconnect to resync from the HTTP history endpoints.
				go func(conn *Conn) {
					if closeErr := conn.Close(closeCodePolicyViolation, "slow consumer"); closeErr != nil {
						slog.Warn("ws: slow consumer close failed", "user_id", userID, "error", closeErr)
					}
				}(c)
				continue
			}
			slog.Error("ws: send error", "user_id", userID, "error", err)
		}
	}
}

// SendToUserExceptSession fans out to every connection for userID except the
// one whose SessionID matches excludeSessionID. Used for cross-device sync
// events (read receipts, future typing/draft sync) where the originating
// device must not receive its own echo. excludeSessionID == "" sends to all.
func (h *Hub) SendToUserExceptSession(userID, excludeSessionID string, msg interface{}) {
	h.mu.RLock()
	src := h.conns[userID]
	snapshot := make([]*Conn, len(src))
	copy(snapshot, src)
	h.mu.RUnlock()

	for _, c := range snapshot {
		if excludeSessionID != "" && c.SessionID == excludeSessionID {
			continue
		}
		if err := c.Send(msg); err != nil {
			if errors.Is(err, errSendQueueFull) || errors.Is(err, errConnClosed) {
				go func(conn *Conn) {
					if closeErr := conn.Close(closeCodePolicyViolation, "slow consumer"); closeErr != nil {
						slog.Warn("ws: slow consumer close failed", "user_id", userID, "error", closeErr)
					}
				}(c)
				continue
			}
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

// CloseUserConnections closes every active connection for the user with a policy-violation frame.
func (h *Hub) CloseUserConnections(userID string) {
	h.mu.RLock()
	src := h.conns[userID]
	snapshot := make([]*Conn, len(src))
	copy(snapshot, src)
	h.mu.RUnlock()

	for _, conn := range snapshot {
		if err := conn.Close(closeCodePolicyViolation, "account deactivated"); err != nil {
			slog.Warn("ws: close user connection failed", "user_id", userID, "error", err)
		}
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
