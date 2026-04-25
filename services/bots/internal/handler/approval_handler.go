// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

// ApprovalBotService is the minimal BotService surface needed by ApprovalHandler.
type ApprovalBotService interface {
	GetBot(ctx context.Context, id uuid.UUID) (*model.Bot, error)
	CheckBotScope(ctx context.Context, botID, chatID uuid.UUID, requiredScope int64) error
}

// ApprovalHandler exposes REST endpoints for the generic approval workflow.
// Any user in a bot-enabled chat can create a request; only the bot owner can
// approve or reject; the requester or owner can cancel.
type ApprovalHandler struct {
	svc    ApprovalBotService
	store  store.ApprovalStore
	logger *slog.Logger
}

// NewApprovalHandler constructs an ApprovalHandler.
func NewApprovalHandler(svc ApprovalBotService, approvalStore store.ApprovalStore, logger *slog.Logger) *ApprovalHandler {
	return &ApprovalHandler{svc: svc, store: approvalStore, logger: logger}
}

// Register mounts the approval routes onto the given router.
func (h *ApprovalHandler) Register(router fiber.Router) {
	router.Post("/bots/:botID/approvals", h.create)
	router.Get("/bots/:botID/approvals", h.list)
	router.Patch("/bots/:botID/approvals/:id", h.decide)
	router.Post("/bots/:botID/approvals/:id/cancel", h.cancel)
}

type createApprovalRequest struct {
	ChatID       string          `json:"chat_id"`
	ApprovalType string          `json:"approval_type"`
	Subject      string          `json:"subject"`
	Payload      []byte          `json:"payload"`
}

func (h *ApprovalHandler) create(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	botID, err := uuid.Parse(c.Params("botID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid bot id"))
	}

	var req createApprovalRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return response.Error(c, err)
	}
	if err := validator.RequireString(req.ApprovalType, "approval_type", 1, 64); err != nil {
		return response.Error(c, err)
	}
	if err := validator.RequireString(req.Subject, "subject", 1, 200); err != nil {
		return response.Error(c, err)
	}

	chatID, err := uuid.Parse(req.ChatID)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat_id"))
	}

	if err := h.svc.CheckBotScope(c.Context(), botID, chatID, model.ScopePostMessages); err != nil {
		return response.Error(c, err)
	}

	approval := &model.ApprovalRequest{
		BotID:        botID,
		ChatID:       chatID,
		RequesterID:  userID,
		ApprovalType: strings.TrimSpace(req.ApprovalType),
		Subject:      strings.TrimSpace(req.Subject),
		Payload:      req.Payload,
	}
	if err := h.store.Create(c.Context(), approval); err != nil {
		h.logger.Error("approvals: create failed", "error", err, "user_id", userID, "bot_id", botID)
		return response.Error(c, apperror.Internal("Failed to create approval request"))
	}

	return response.JSON(c, fiber.StatusCreated, approval)
}

func (h *ApprovalHandler) list(c *fiber.Ctx) error {
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

	filter := store.ApprovalFilter{BotID: botID}

	// Non-owners only see their own requests.
	if bot.OwnerID != userID {
		uid := userID
		filter.RequesterID = &uid
	}

	if rawChatID := c.Query("chat_id"); rawChatID != "" {
		chatID, parseErr := uuid.Parse(rawChatID)
		if parseErr != nil {
			return response.Error(c, apperror.BadRequest("Invalid chat_id filter"))
		}
		filter.ChatID = &chatID
	}

	if status := c.Query("status"); status != "" {
		switch status {
		case model.ApprovalStatusPending, model.ApprovalStatusApproved,
			model.ApprovalStatusRejected, model.ApprovalStatusCancelled:
			filter.Status = &status
		default:
			return response.Error(c, apperror.BadRequest("Invalid status filter"))
		}
	}

	requests, err := h.store.List(c.Context(), filter)
	if err != nil {
		h.logger.Error("approvals: list failed", "error", err, "user_id", userID, "bot_id", botID)
		return response.Error(c, apperror.Internal("Failed to list approval requests"))
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"requests": requests})
}

type decideApprovalRequest struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (h *ApprovalHandler) decide(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	botID, err := uuid.Parse(c.Params("botID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid bot id"))
	}
	approvalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid approval id"))
	}

	bot, err := h.svc.GetBot(c.Context(), botID)
	if err != nil {
		return response.Error(c, err)
	}
	if bot == nil {
		return response.Error(c, apperror.NotFound("Bot not found"))
	}
	if bot.OwnerID != userID {
		return response.Error(c, apperror.Forbidden("Only the bot owner can decide approval requests"))
	}

	var body decideApprovalRequest
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	switch body.Decision {
	case "approve":
		body.Decision = model.ApprovalStatusApproved
	case "reject":
		body.Decision = model.ApprovalStatusRejected
	default:
		return response.Error(c, apperror.BadRequest("decision must be approve or reject"))
	}
	if err := validator.OptionalString(body.Note, "note", 500); err != nil {
		return response.Error(c, err)
	}

	// Verify the request belongs to this bot (prevents cross-bot tampering).
	existing, err := h.store.GetByID(c.Context(), approvalID)
	if err != nil {
		if errors.Is(err, model.ErrApprovalNotFound) {
			return response.Error(c, apperror.NotFound("Approval request not found"))
		}
		return response.Error(c, apperror.Internal("Failed to load approval request"))
	}
	if existing.BotID != botID {
		return response.Error(c, apperror.NotFound("Approval request not found"))
	}

	updated, err := h.store.Decide(c.Context(), approvalID, userID, body.Decision, strings.TrimSpace(body.Note))
	if err != nil {
		switch {
		case errors.Is(err, model.ErrApprovalNotFound):
			return response.Error(c, apperror.NotFound("Approval request not found"))
		case errors.Is(err, model.ErrApprovalAlreadyFinal):
			return response.Error(c, apperror.Conflict("Approval request is already decided or cancelled"))
		case errors.Is(err, model.ErrApprovalVersionConflict):
			return response.Error(c, apperror.Conflict("Approval request was modified concurrently, please retry"))
		}
		h.logger.Error("approvals: decide failed", "error", err, "approval_id", approvalID, "decider_id", userID)
		return response.Error(c, apperror.Internal("Failed to decide approval request"))
	}

	return response.JSON(c, fiber.StatusOK, updated)
}

func (h *ApprovalHandler) cancel(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	botID, err := uuid.Parse(c.Params("botID"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid bot id"))
	}
	approvalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid approval id"))
	}

	bot, err := h.svc.GetBot(c.Context(), botID)
	if err != nil {
		return response.Error(c, err)
	}
	if bot == nil {
		return response.Error(c, apperror.NotFound("Bot not found"))
	}

	// Verify the request belongs to this bot before attempting cancel.
	existing, err := h.store.GetByID(c.Context(), approvalID)
	if err != nil {
		if errors.Is(err, model.ErrApprovalNotFound) {
			return response.Error(c, apperror.NotFound("Approval request not found"))
		}
		return response.Error(c, apperror.Internal("Failed to load approval request"))
	}
	if existing.BotID != botID {
		return response.Error(c, apperror.NotFound("Approval request not found"))
	}

	isOwner := bot.OwnerID == userID
	updated, err := h.store.Cancel(c.Context(), approvalID, userID, isOwner)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrApprovalNotFound):
			return response.Error(c, apperror.NotFound("Approval request not found"))
		case errors.Is(err, model.ErrApprovalAlreadyFinal):
			return response.Error(c, apperror.Conflict("Approval request is already decided or cancelled"))
		case errors.Is(err, model.ErrApprovalForbidden):
			return response.Error(c, apperror.Forbidden("Only the requester or bot owner can cancel"))
		}
		h.logger.Error("approvals: cancel failed", "error", err, "approval_id", approvalID, "caller_id", userID)
		return response.Error(c, apperror.Internal("Failed to cancel approval request"))
	}

	return response.JSON(c, fiber.StatusOK, updated)
}
