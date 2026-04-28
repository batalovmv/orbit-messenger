// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

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
	"strings"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
)

const (
	pingInterval  = 30 * time.Second
	pongWait      = 10 * time.Second
	authTimeout   = 10 * time.Second
	authCacheTTL  = 30 * time.Second
	typingCleanup = 60 * time.Second
)

const typingExpire = 6 * time.Second

type ValidatedToken struct {
	UserID    string
	ExpiresAt time.Time
	TokenHash string
	// JTI is the session id encoded in the JWT's "jti" claim. Used by the
	// admin session-revoke flow (Day 5.2) so the hub can force-close a
	// specific connection when its session is revoked, without waiting for
	// the next token revalidation tick. Empty if the token is missing jti.
	JTI string
}

// Handler manages WebSocket connections and typing debounce.
type Handler struct {
	Hub             *Hub
	NATS            *nats.Conn
	callsServiceURL string // base URL of the calls service for internal membership checks
	internalSecret  string // shared secret for X-Internal-Token header

	typingMu       sync.Mutex
	typingDebounce map[string]time.Time   // key: chatID+userID -> last broadcast
	typingTimers   map[string]*time.Timer // key: chatID+userID -> auto stop_typing timer

	done chan struct{}
}

func NewHandler(hub *Hub, nc *nats.Conn, callsServiceURL, internalSecret string) *Handler {
	h := &Handler{
		Hub:             hub,
		NATS:            nc,
		callsServiceURL: callsServiceURL,
		internalSecret:  internalSecret,
		typingDebounce:  make(map[string]time.Time),
		typingTimers:    make(map[string]*time.Timer),
		done:            make(chan struct{}),
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
			Token     string `json:"token"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(cm.Data, &authData); err != nil || authData.Token == "" {
			c.WriteJSON(Envelope{Type: "error", Data: json.RawMessage(`{"message":"missing token"}`)})
			c.Close()
			return
		}
		// Generate a server-side session ID if the client did not supply one.
		// Older clients that haven't been updated still work; they just don't
		// participate in origin-session exclusion (they will receive their own
		// echoes, which is harmless for closeNotifications-style handlers).
		sessionID := strings.TrimSpace(authData.SessionID)
		if sessionID == "" {
			sessionID = uuid.NewString()
		}

		// Clear the auth-frame read deadline before the HTTP call to auth service,
		// so a slow auth response doesn't kill the WS connection.
		c.SetReadDeadline(time.Time{})

		// Step 2: Validate token via auth service (with Redis cache)
		// Bounded to authTimeout (10s). Note: context.Background() does not cancel on
		// client disconnect — Fiber's WebSocket handler doesn't expose a request context.
		// The timeout bounds the worst-case resource waste per stalled auth attempt.
		authCtx, authCancel := context.WithTimeout(context.Background(), authTimeout)
		tokenInfo, err := ValidateToken(authCtx, authClient, rdb, authServiceURL, authData.Token)
		authCancel()
		if err != nil || tokenInfo == nil || tokenInfo.UserID == "" {
			c.WriteJSON(Envelope{Type: "error", Data: json.RawMessage(`{"message":"invalid token"}`)})
			c.Close()
			return
		}

		// Step 3: Auth success — send confirmation
		c.WriteJSON(Envelope{Type: "auth_ok", Data: json.RawMessage(`{}`)})

		connCtx, connCancel := context.WithCancel(context.Background())
		conn := &Conn{
			WS:          c,
			UserID:      tokenInfo.UserID,
			SessionID:   sessionID,
			JTI:         tokenInfo.JTI,
			done:        make(chan struct{}),
			ctx:         connCtx,
			cancel:      connCancel,
			tokenExpiry: tokenInfo.ExpiresAt,
			tokenHash:   &tokenInfo.TokenHash,
			revalidateFn: func(ctx context.Context) error {
				return RevalidateToken(ctx, authClient, rdb, authServiceURL, authData.Token, tokenInfo.TokenHash)
			},
		}

		// Atomic check-and-register to prevent duplicate "online" events on multi-device race
		isFirstConnection := h.Hub.Register(conn)
		defer func() {
			isLastConnection := h.Hub.Unregister(conn)
			conn.cancel()          // cancel connection-scoped context
			close(conn.done)
			if isLastConnection {
				h.publishStatusChange(tokenInfo.UserID, "offline")
			}
		}()

		if isFirstConnection {
			h.publishStatusChange(tokenInfo.UserID, "online")
		}

		// Set pong handler
		c.SetPongHandler(func(string) error {
			return c.SetReadDeadline(time.Now().Add(pingInterval + pongWait))
		})

		// Start ping loop
		go h.pingLoop(conn)
		StartTokenRevalidation(conn)

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

// ValidateToken checks JWT via auth service with Redis cache.
// Mirrors the blacklist check from middleware/jwt.go to prevent revoked tokens from authenticating WS.
// Exported so the SFU proxy (Phase 6 Stage 3) can reuse the same auth-frame
// flow without duplicating cache + blacklist logic.
func ValidateToken(ctx context.Context, client *http.Client, rdb *redis.Client, authURL, token string) (*ValidatedToken, error) {
	tokenHash := sha256Hash(token)
	cacheKey := "jwt_cache:" + tokenHash
	blacklistKey := "jwt_blacklist:" + tokenHash
	expiresAt, err := parseTokenExpiry(token)
	if err != nil {
		return nil, err
	}

	if !time.Now().Before(expiresAt) {
		if err := rdb.Del(ctx, cacheKey).Err(); err != nil {
			slog.Error("WS JWT cache del failed after expiry", "error", err)
		}
		return nil, nil
	}

	// Check blacklist first — fail-closed on Redis error
	blacklisted, blErr := rdb.Exists(ctx, blacklistKey).Result()
	if blErr != nil {
		slog.Error("WS blacklist check failed, rejecting token", "error", blErr)
		return nil, fmt.Errorf("blacklist check failed")
	}
	if blacklisted > 0 {
		if err := rdb.Del(ctx, cacheKey).Err(); err != nil {
			slog.Error("WS JWT cache del failed after blacklist hit", "error", err)
		}
		return nil, nil
	}

	// Per-jti blacklist (Day 5.2 admin session revoke). Same reasoning as
	// gateway JWT middleware: the cache short-circuit below would otherwise
	// keep a revoked session valid for an entire authCacheTTL window.
	if jti := parseTokenJTI(token); jti != "" {
		jtiKey := "jwt_blacklist:jti:" + jti
		jtiBl, jtiErr := rdb.Exists(ctx, jtiKey).Result()
		if jtiErr != nil {
			slog.Error("WS jti blacklist check failed, rejecting token", "error", jtiErr)
			return nil, fmt.Errorf("jti blacklist check failed")
		}
		if jtiBl > 0 {
			if err := rdb.Del(ctx, cacheKey).Err(); err != nil {
				slog.Error("WS JWT cache del failed after jti blacklist hit", "error", err)
			}
			return nil, nil
		}
	}

	// Check cache
	if cached, err := rdb.Get(ctx, cacheKey).Result(); err == nil {
		var u struct {
			ID string `json:"id"`
		}
		if json.Unmarshal([]byte(cached), &u) == nil && u.ID != "" {
			return &ValidatedToken{
				UserID:    u.ID,
				ExpiresAt: expiresAt,
				TokenHash: tokenHash,
				JTI:       parseTokenJTI(token),
			}, nil
		}
	}

	// Call auth service
	req, err := http.NewRequestWithContext(ctx, "GET", authURL+"/auth/me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return nil, err
	}

	var user struct {
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}

	// Cache
	cuJSON, err := json.Marshal(user)
	if err != nil {
		slog.Error("WS JWT cache marshal failed", "error", err)
	} else if err := rdb.Set(ctx, cacheKey, string(cuJSON), authCacheTTL).Err(); err != nil {
		slog.Error("WS JWT cache write failed", "error", err)
	}

	return &ValidatedToken{
		UserID:    user.ID,
		ExpiresAt: expiresAt,
		TokenHash: tokenHash,
		JTI:       parseTokenJTI(token),
	}, nil
}

func RevalidateToken(ctx context.Context, client *http.Client, rdb *redis.Client, authURL, token, tokenHash string) error {
	blacklistKey := "jwt_blacklist:" + tokenHash
	blacklisted, err := rdb.Exists(ctx, blacklistKey).Result()
	if err != nil {
		slog.Error("WS blacklist revalidation failed", "error", err)
		return fmt.Errorf("blacklist check failed: %w", err)
	}
	if blacklisted > 0 {
		return fmt.Errorf("token revoked")
	}

	// Per-jti blacklist for admin session revoke (Day 5.2). The periodic
	// revalidation tick is the only chance to catch a revoked session for a
	// connection that was never targeted by the orbit.session.*.revoked
	// NATS event (e.g. publish dropped on core NATS).
	if jti := parseTokenJTI(token); jti != "" {
		jtiBl, jtiErr := rdb.Exists(ctx, "jwt_blacklist:jti:"+jti).Result()
		if jtiErr != nil {
			slog.Error("WS jti blacklist revalidation failed", "error", jtiErr)
			return fmt.Errorf("jti blacklist check failed: %w", jtiErr)
		}
		if jtiBl > 0 {
			return fmt.Errorf("session revoked")
		}
	}

	req, err := http.NewRequestWithContext(ctx, "GET", authURL+"/auth/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token rejected: %d", resp.StatusCode)
	}

	return nil
}

func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func parseTokenExpiry(token string) (time.Time, error) {
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	if _, _, err := parser.ParseUnverified(token, claims); err != nil {
		return time.Time{}, err
	}

	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil {
		return time.Time{}, fmt.Errorf("token missing exp")
	}

	return exp.Time, nil
}

// parseTokenJTI extracts the "jti" claim without verifying the signature.
// Returns "" on any parse error or missing claim. The token is already
// validated upstream (blacklist + auth /me) by the time we reach here.
func parseTokenJTI(token string) string {
	claims := jwt.MapClaims{}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	if _, _, err := parser.ParseUnverified(token, claims); err != nil {
		return ""
	}
	jti, _ := claims["jti"].(string)
	return jti
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
	case "stop_typing":
		h.handleStopTyping(conn, cm.Data)
	case "ping":
		conn.Send(Envelope{Type: EventPong, Data: json.RawMessage(`{}`)})
	case EventWebRTCOffer, EventWebRTCAnswer, EventWebRTCICECandidate:
		h.handleSignalingRelay(conn, cm.Type, cm.Data)
	case "set_online":
		h.handleSetOnline(conn, cm.Data)
	}
}

func (h *Handler) handleSetOnline(conn *Conn, data json.RawMessage) {
	var payload struct {
		IsOnline bool `json:"is_online"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}

	if payload.IsOnline {
		h.publishStatusChange(conn.UserID, "online")
	} else {
		h.publishStatusChange(conn.UserID, "recently")
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

func (h *Handler) handleStopTyping(conn *Conn, data json.RawMessage) {
	var td TypingData
	if err := json.Unmarshal(data, &td); err != nil || td.ChatID == "" {
		return
	}
	if _, err := uuid.Parse(td.ChatID); err != nil {
		return
	}

	chatID := td.ChatID
	userID := conn.UserID

	// Cancel auto-expire timer
	key := chatID + ":" + userID
	h.typingMu.Lock()
	if timer, ok := h.typingTimers[key]; ok {
		timer.Stop()
		delete(h.typingTimers, key)
	}
	delete(h.typingDebounce, key)
	h.typingMu.Unlock()

	// Publish stop_typing immediately
	stopData, _ := json.Marshal(map[string]string{
		"chat_id": chatID,
		"user_id": userID,
	})
	evt := NATSEvent{
		Event:     EventStopTyping,
		Data:      stopData,
		Timestamp: time.Now().Format(time.RFC3339),
	}
	evtJSON, _ := json.Marshal(evt)
	subject := "orbit.chat." + chatID + ".typing"
	if err := h.NATS.Publish(subject, evtJSON); err != nil {
		slog.Error("failed to publish stop_typing", "error", err)
	}
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

// handleSignalingRelay relays WebRTC signaling messages (offer/answer/ICE) directly
// between connected clients without NATS roundtrip for minimal latency.
func (h *Handler) handleSignalingRelay(conn *Conn, eventType string, data json.RawMessage) {
	var sd SignalingData
	if err := json.Unmarshal(data, &sd); err != nil || sd.CallID == "" || sd.TargetUserID == "" {
		return
	}

	// Validate UUIDs to prevent injection
	if _, err := uuid.Parse(sd.CallID); err != nil {
		return
	}
	if _, err := uuid.Parse(sd.TargetUserID); err != nil {
		return
	}

	// Verify both sender and target are active participants in the call (prevents IDOR).
	// Fail-closed: if the calls service is unreachable the frame is dropped.
	memberCtx, memberCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer memberCancel()
	if !h.checkCallMembership(memberCtx, sd.CallID, conn.UserID) {
		return
	}
	if !h.checkCallMembership(memberCtx, sd.CallID, sd.TargetUserID) {
		return
	}

	// Rate limit: reuse per-connection burst limiter (max 10 per 100ms)
	conn.mu.Lock()
	now := time.Now()
	if now.Sub(conn.lastTyping) < 100*time.Millisecond {
		conn.typingBurst++
		if conn.typingBurst > 30 { // higher limit for signaling (ICE can be bursty)
			conn.mu.Unlock()
			return
		}
	} else {
		conn.typingBurst = 0
	}
	conn.lastTyping = now
	conn.mu.Unlock()

	// Inject sender_id from the authenticated connection (never trust client-supplied value)
	// Re-marshal with sender_id injected
	relayData := make(map[string]json.RawMessage)
	json.Unmarshal(data, &relayData)
	senderJSON, _ := json.Marshal(conn.UserID)
	relayData["sender_id"] = senderJSON

	finalData, _ := json.Marshal(relayData)
	envelope := Envelope{
		Type: eventType,
		Data: finalData,
	}

	h.Hub.SendToUser(sd.TargetUserID, envelope)
}

// checkCallMembership verifies via the calls service that userID is an active
// participant in callID. Returns false on any error (fail-closed).
// SECURITY: always returns false when callsServiceURL is empty — never fail-open.
func (h *Handler) checkCallMembership(ctx context.Context, callID, userID string) bool {
	if h.callsServiceURL == "" {
		slog.Error("checkCallMembership: callsServiceURL not configured — denying signaling relay")
		return false
	}
	url := fmt.Sprintf("%s/internal/calls/%s/members/%s", h.callsServiceURL, callID, userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Error("checkCallMembership: build request", "error", err)
		return false
	}
	req.Header.Set("X-Internal-Token", h.internalSecret)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("checkCallMembership: request failed", "error", err, "call_id", callID, "user_id", userID)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("checkCallMembership: unexpected status", "status", resp.StatusCode, "call_id", callID, "user_id", userID)
		return false
	}
	var result struct {
		IsMember bool `json:"is_member"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("checkCallMembership: decode response", "error", err)
		return false
	}
	return result.IsMember
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
