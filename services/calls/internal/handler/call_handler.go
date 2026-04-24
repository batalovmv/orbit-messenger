// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"crypto/subtle"
	"fmt"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/calls/internal/model"
	"github.com/mst-corp/orbit/services/calls/internal/service"
)

// CallHandler handles HTTP requests for calls.
type CallHandler struct {
	svc          *service.CallService
	logger       *slog.Logger
	turnURL      string
	turnUser     string
	turnPassword string
}

// NewCallHandler creates a new CallHandler.
func NewCallHandler(svc *service.CallService, logger *slog.Logger, turnURL, turnUser, turnPassword string) *CallHandler {
	return &CallHandler{
		svc:          svc,
		logger:       logger,
		turnURL:      turnURL,
		turnUser:     turnUser,
		turnPassword: turnPassword,
	}
}

// Register wires routes onto the fiber.Router.
func (h *CallHandler) Register(app fiber.Router) {
	app.Post("/calls", h.CreateCall)
	app.Get("/calls/history", h.ListCallHistory)
	app.Get("/calls/:id", h.GetCall)
	app.Put("/calls/:id/accept", h.AcceptCall)
	app.Put("/calls/:id/decline", h.DeclineCall)
	app.Put("/calls/:id/end", h.EndCall)
	app.Post("/calls/:id/participants", h.AddParticipant)
	app.Delete("/calls/:id/participants/:uid", h.RemoveParticipant)
	app.Put("/calls/:id/mute", h.ToggleMute)
	app.Put("/calls/:id/screen-share/start", h.StartScreenShare)
	app.Put("/calls/:id/screen-share/stop", h.StopScreenShare)
	app.Get("/calls/:id/ice-servers", h.GetICEServers)
	// Group-call lifecycle (Stage 3): join/leave a group call as a SFU peer.
	// The actual SFU media plane lives behind the GET /calls/:id/sfu-ws WS
	// endpoint registered by SFUHandler — these REST helpers exist so the
	// frontend can refresh participant state and publish NATS events without
	// having to wait for the WS handshake to land.
	app.Post("/calls/:id/join", h.JoinGroupCall)
	app.Delete("/calls/:id/leave", h.LeaveGroupCall)
	app.Post("/calls/:id/rating", h.RateCall)
}

func getUserID(c *fiber.Ctx) (uuid.UUID, error) {
	idStr := c.Get("X-User-ID")
	if idStr == "" {
		return uuid.Nil, apperror.Unauthorized("Missing user context")
	}
	return uuid.Parse(idStr)
}

// RequireInternalToken middleware verifies the internal token header.
func RequireInternalToken(secret string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Get("X-Internal-Token")
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(secret)) != 1 {
			return response.Error(c, apperror.Forbidden("Invalid internal token"))
		}
		return c.Next()
	}
}

func (h *CallHandler) CreateCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	var req struct {
		ChatID    string   `json:"chat_id"`
		Type      string   `json:"type"`
		Mode      string   `json:"mode"`
		MemberIDs []string `json:"member_ids"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat_id"))
	}

	if !model.ValidCallTypes[req.Type] {
		return response.Error(c, apperror.BadRequest("Invalid call type, must be voice or video"))
	}
	if !model.ValidCallModes[req.Mode] {
		return response.Error(c, apperror.BadRequest("Invalid call mode, must be p2p or group"))
	}

	if req.Mode == "group" && len(req.MemberIDs) == 0 {
		return response.Error(c, apperror.BadRequest("member_ids is required for group calls"))
	}
	if len(req.MemberIDs) > 50 {
		return response.Error(c, apperror.BadRequest("member_ids must not exceed 50 items"))
	}
	for i, id := range req.MemberIDs {
		if _, err := uuid.Parse(id); err != nil {
			return response.Error(c, apperror.BadRequest(fmt.Sprintf("member_ids[%d] is not a valid UUID", i)))
		}
	}

	call, err := h.svc.CreateCall(c.Context(), uid, service.CreateCallRequest{
		ChatID:    chatID,
		Type:      req.Type,
		Mode:      req.Mode,
		MemberIDs: req.MemberIDs,
	})
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusCreated, call)
}

func (h *CallHandler) AcceptCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	call, err := h.svc.AcceptCall(c.Context(), callID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, call)
}

func (h *CallHandler) DeclineCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	if err := h.svc.DeclineCall(c.Context(), callID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "declined"})
}

func (h *CallHandler) EndCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	if err := h.svc.EndCall(c.Context(), callID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "ended"})
}

func (h *CallHandler) GetCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	call, err := h.svc.GetCall(c.Context(), callID, uid)
	if err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, call)
}

func (h *CallHandler) ListCallHistory(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	cursor := c.Query("cursor")
	limit := c.QueryInt("limit", 50)
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	calls, nextCursor, hasMore, err := h.svc.ListCallHistory(c.Context(), uid, cursor, limit)
	if err != nil {
		return response.Error(c, err)
	}

	return response.Paginated(c, calls, nextCursor, hasMore)
}

func (h *CallHandler) AddParticipant(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	var req struct {
		UserID string `json:"user_id"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	targetUID, err := uuid.Parse(req.UserID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user_id"))
	}

	if err := h.svc.AddParticipant(c.Context(), callID, targetUID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "added"})
}

func (h *CallHandler) RemoveParticipant(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	targetUID, err := uuid.Parse(c.Params("uid"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid user ID"))
	}

	if err := h.svc.RemoveParticipant(c.Context(), callID, targetUID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "removed"})
}

func (h *CallHandler) ToggleMute(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	var req struct {
		Muted bool `json:"muted"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := h.svc.ToggleMute(c.Context(), callID, uid, req.Muted); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"muted": req.Muted})
}

func (h *CallHandler) StartScreenShare(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	if err := h.svc.StartScreenShare(c.Context(), callID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"screen_sharing": true})
}

func (h *CallHandler) StopScreenShare(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	if err := h.svc.StopScreenShare(c.Context(), callID, uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"screen_sharing": false})
}

func (h *CallHandler) GetICEServers(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}

	servers := h.svc.GetICEServers(h.turnURL, h.turnUser, h.turnPassword, uid)
	return response.JSON(c, fiber.StatusOK, fiber.Map{"ice_servers": servers})
}

func (h *CallHandler) JoinGroupCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}
	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}
	if err := h.svc.JoinGroupCall(c.Context(), callID, uid); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "joined"})
}

func (h *CallHandler) RateCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}
	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}

	var req struct {
		Rating  int    `json:"rating"`
		Comment string `json:"comment"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if req.Rating < 1 || req.Rating > 5 {
		return response.Error(c, apperror.BadRequest("rating must be between 1 and 5"))
	}
	if len([]rune(req.Comment)) > 1000 {
		return response.Error(c, apperror.BadRequest("comment is too long"))
	}

	if err := h.svc.RateCall(c.Context(), callID, uid, req.Rating, req.Comment); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "rated"})
}

func (h *CallHandler) LeaveGroupCall(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Invalid user ID"))
	}
	callID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid call ID"))
	}
	if err := h.svc.LeaveGroupCall(c.Context(), callID, uid, true); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, fiber.Map{"status": "left"})
}
