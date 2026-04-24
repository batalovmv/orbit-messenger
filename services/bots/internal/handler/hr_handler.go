// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

const hrDateLayout = "2006-01-02"

// HRBotService is the minimal surface of *service.BotService the HR handler
// depends on. Extracted as an interface so handler tests can mock bot lookups
// and scope checks without spinning up the full service.
type HRBotService interface {
	GetBot(ctx context.Context, id uuid.UUID) (*model.Bot, error)
	CheckBotScope(ctx context.Context, botID, chatID uuid.UUID, requiredScope int64) error
}

// HRHandler exposes REST endpoints for the HR bot time-off workflow.
// Scope for 150-employee corporate use: any authenticated user can create a
// pending request in a chat where the bot is installed; only the bot owner
// (HR manager who registered the bot) can approve or reject.
type HRHandler struct {
	svc    HRBotService
	store  store.HRRequestStore
	logger *slog.Logger
}

func NewHRHandler(svc HRBotService, hrStore store.HRRequestStore, logger *slog.Logger) *HRHandler {
	return &HRHandler{svc: svc, store: hrStore, logger: logger}
}

func (h *HRHandler) Register(router fiber.Router) {
	router.Post("/bots/:botID/hr/requests", h.create)
	router.Get("/bots/:botID/hr/requests", h.list)
	router.Patch("/bots/:botID/hr/requests/:id", h.decide)
}

type createHRRequest struct {
	ChatID      string `json:"chat_id"`
	RequestType string `json:"request_type"`
	StartDate   string `json:"start_date"`
	EndDate     string `json:"end_date"`
	Reason      string `json:"reason"`
}

func (h *HRHandler) create(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	botID, err := uuid.Parse(c.Params("botID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid bot id"))
	}

	var req createHRRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return response.Error(c, err)
	}
	if !model.IsValidHRRequestType(req.RequestType) {
		return response.Error(c, apperror.BadRequest("request_type must be vacation, sick_leave, or day_off"))
	}
	if err := validator.OptionalString(req.Reason, "reason", 500); err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat_id"))
	}

	startDate, err := time.Parse(hrDateLayout, req.StartDate)
	if err != nil {
		return response.Error(c, apperror.BadRequest("start_date must be YYYY-MM-DD"))
	}
	endDate, err := time.Parse(hrDateLayout, req.EndDate)
	if err != nil {
		return response.Error(c, apperror.BadRequest("end_date must be YYYY-MM-DD"))
	}
	if endDate.Before(startDate) {
		return response.Error(c, apperror.BadRequest("end_date must be on or after start_date"))
	}

	// Bot must be installed in chatID and have PostMessages scope (so it can
	// reply with approve/reject notifications).
	if err := h.svc.CheckBotScope(c.Context(), botID, chatID, model.ScopePostMessages); err != nil {
		return response.Error(c, err)
	}

	var reasonPtr *string
	if strings.TrimSpace(req.Reason) != "" {
		r := strings.TrimSpace(req.Reason)
		reasonPtr = &r
	}

	hrReq := &model.HRRequest{
		BotID:       botID,
		ChatID:      chatID,
		UserID:      userID,
		RequestType: req.RequestType,
		StartDate:   startDate,
		EndDate:     endDate,
		Reason:      reasonPtr,
	}
	if err := h.store.Create(c.Context(), hrReq); err != nil {
		h.logger.Error("hr: create request failed", "error", err, "user_id", userID, "bot_id", botID)
		return response.Error(c, apperror.Internal("Failed to create HR request"))
	}

	return response.JSON(c, fiber.StatusCreated, hrReq)
}

func (h *HRHandler) list(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	botID, err := uuid.Parse(c.Params("botID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid bot id"))
	}

	bot, err := h.svc.GetBot(c.Context(), botID)
	if err != nil {
		return response.Error(c, err)
	}
	if bot == nil {
		return response.Error(c, apperror.NotFound("Bot not found"))
	}

	filter := store.HRRequestFilter{BotID: botID}

	// Non-owners only see their own requests.
	if bot.OwnerID != userID {
		uid := userID
		filter.UserID = &uid
	}

	if status := c.Query("status"); status != "" {
		if status != model.HRStatusPending && status != model.HRStatusApproved && status != model.HRStatusRejected {
			return response.Error(c, apperror.BadRequest("Invalid status filter"))
		}
		filter.Status = &status
	}

	requests, err := h.store.List(c.Context(), filter)
	if err != nil {
		h.logger.Error("hr: list requests failed", "error", err, "user_id", userID, "bot_id", botID)
		return response.Error(c, apperror.Internal("Failed to list HR requests"))
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"requests": requests})
}

type decideHRRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (h *HRHandler) decide(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	botID, err := uuid.Parse(c.Params("botID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid bot id"))
	}
	reqID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request id"))
	}

	bot, err := h.svc.GetBot(c.Context(), botID)
	if err != nil {
		return response.Error(c, err)
	}
	if bot == nil {
		return response.Error(c, apperror.NotFound("Bot not found"))
	}
	if bot.OwnerID != userID {
		return response.Error(c, apperror.Forbidden("Only the bot owner can decide HR requests"))
	}

	var body decideHRRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	var approve bool
	switch body.Decision {
	case "approve":
		approve = true
	case "reject":
		approve = false
	default:
		return response.Error(c, apperror.BadRequest("decision must be approve or reject"))
	}
	if err := validator.OptionalString(body.Note, "note", 500); err != nil {
		return response.Error(c, err)
	}

	// Verify the request belongs to this bot (prevents cross-bot tampering).
	existing, err := h.store.GetByID(c.Context(), reqID)
	if err != nil {
		if errors.Is(err, model.ErrHRRequestNotFound) {
			return response.Error(c, apperror.NotFound("HR request not found"))
		}
		return response.Error(c, apperror.Internal("Failed to load HR request"))
	}
	if existing.BotID != botID {
		return response.Error(c, apperror.NotFound("HR request not found"))
	}

	updated, err := h.store.Decide(c.Context(), reqID, userID, approve, strings.TrimSpace(body.Note))
	if err != nil {
		switch {
		case errors.Is(err, model.ErrHRRequestNotFound):
			return response.Error(c, apperror.NotFound("HR request not found"))
		case errors.Is(err, model.ErrHRRequestAlreadyFinal):
			return response.Error(c, apperror.Conflict("HR request is already decided"))
		}
		h.logger.Error("hr: decide failed", "error", err, "request_id", reqID, "approver_id", userID)
		return response.Error(c, apperror.Internal("Failed to update HR request"))
	}

	return response.JSON(c, fiber.StatusOK, updated)
}
