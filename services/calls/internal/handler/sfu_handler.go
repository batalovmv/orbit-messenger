// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/calls/internal/service"
	sfu "github.com/mst-corp/orbit/services/calls/internal/webrtc"
)

// SFUHandler bridges the calls service WebSocket endpoint to the SFU package.
// One handler is shared across all WS connections; per-connection state lives
// on the *sfu.Peer that we create on upgrade.
type SFUHandler struct {
	svc    *service.CallService
	sfu    *sfu.SFU
	logger *slog.Logger
}

// NewSFUHandler returns a new handler. svc is needed for membership checks
// + JoinGroupCall / LeaveGroupCall side effects (NATS publish, db updates).
func NewSFUHandler(svc *service.CallService, s *sfu.SFU, logger *slog.Logger) *SFUHandler {
	return &SFUHandler{svc: svc, sfu: s, logger: logger}
}

// Register wires the SFU WebSocket route. The handler is mounted under the
// internal-token middleware in main.go just like the rest of the call API.
func (h *SFUHandler) Register(app fiber.Router) {
	// Reject upgrades that don't have a valid call_id before paying the
	// cost of a WS handshake.
	app.Use("/calls/:id/sfu-ws", func(c *fiber.Ctx) error {
		if !websocket.IsWebSocketUpgrade(c) {
			return fiber.ErrUpgradeRequired
		}
		if _, err := uuid.Parse(c.Params("id")); err != nil {
			return fiber.ErrBadRequest
		}
		// Pre-resolve user ID from the X-User-ID header (set by gateway).
		uid := c.Get("X-User-ID")
		if uid == "" {
			return fiber.ErrUnauthorized
		}
		if _, err := uuid.Parse(uid); err != nil {
			return fiber.ErrUnauthorized
		}
		c.Locals("call_id", c.Params("id"))
		c.Locals("user_id", uid)
		return c.Next()
	})

	app.Get("/calls/:id/sfu-ws", websocket.New(h.handle))
}

// handle services one WebSocket connection from join to disconnect.
func (h *SFUHandler) handle(c *websocket.Conn) {
	callID, err := uuid.Parse(c.Locals("call_id").(string))
	if err != nil {
		_ = c.Close()
		return
	}
	userID, err := uuid.Parse(c.Locals("user_id").(string))
	if err != nil {
		_ = c.Close()
		return
	}
	logger := h.logger.With("call_id", callID.String(), "user_id", userID.String())

	// Validate that this user is allowed in the call. JoinGroupCall handles
	// chat-membership + active-call checks AND emits the call_participant_joined
	// NATS event so other peers see the new participant in their UI.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := h.svc.JoinGroupCall(ctx, callID, userID); err != nil {
		cancel()
		logger.Warn("sfu: join rejected", "error", err)
		_ = c.WriteJSON(map[string]any{"event": "error", "data": err.Error()})
		_ = c.Close()
		return
	}
	cancel()

	room := h.sfu.GetOrCreateRoom(callID)
	peer, err := sfu.NewPeer(userID, c, room, logger)
	if err != nil {
		logger.Error("sfu: new peer", "error", err)
		_ = c.Close()
		// Roll back the participant insert so the DB doesn't drift.
		_ = h.svc.LeaveGroupCall(context.Background(), callID, userID, false)
		return
	}
	room.AddPeer(peer)

	// On exit: tear down peer + leave the call. The "last leaver" check is
	// handled by LeaveGroupCall which auto-ends the call when no participants
	// remain (matches the P2P EndCall semantics).
	defer func() {
		room.RemovePeer(userID)
		// If this was the last peer, drop the room.
		if room.PeerCount() == 0 {
			h.sfu.CloseRoom(callID)
		}
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel2()
		if err := h.svc.LeaveGroupCall(ctx2, callID, userID, true); err != nil {
			logger.Warn("sfu: leave call cleanup", "error", err)
		}
	}()

	// Read loop: relay client signaling messages into the peer.
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			if !errors.Is(err, websocket.ErrCloseSent) {
				logger.Info("sfu: ws read end", "error", err)
			}
			return
		}

		var msg sfu.SignalMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			logger.Warn("sfu: bad signal frame", "error", err)
			continue
		}

		switch msg.Event {
		case "answer":
			if err := peer.HandleAnswer(msg.Data); err != nil {
				logger.Warn("sfu: handle answer", "error", err)
				return
			}
		case "candidate":
			if err := peer.AddICECandidate(msg.Data); err != nil {
				logger.Warn("sfu: add ice", "error", err)
			}
		default:
			logger.Warn("sfu: unknown signal event", "event", msg.Event)
		}
	}
}
