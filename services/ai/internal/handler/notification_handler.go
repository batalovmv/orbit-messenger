// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/ai/internal/model"
	"github.com/mst-corp/orbit/services/ai/internal/service"
	"github.com/mst-corp/orbit/services/ai/internal/store"
)

// NotificationHandler serves the notification classification endpoints.
type NotificationHandler struct {
	svc    *service.AIService
	store  store.NotificationStore
	logger *slog.Logger
}

func NewNotificationHandler(svc *service.AIService, ns store.NotificationStore, logger *slog.Logger) *NotificationHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationHandler{svc: svc, store: ns, logger: logger}
}

// Register wires notification classification routes onto the given router.
func (h *NotificationHandler) Register(router fiber.Router) {
	router.Post("/ai/classify-notification", h.classifyNotification)
	router.Post("/ai/notification-priority/feedback", h.notificationFeedback)
	router.Get("/ai/notification-priority/stats", h.notificationStats)
}

// POST /ai/classify-notification
func (h *NotificationHandler) classifyNotification(c *fiber.Ctx) error {
	var req model.ClassifyNotificationRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	// Validate required fields.
	if strings.TrimSpace(req.SenderID) == "" {
		return response.Error(c, apperror.BadRequest("sender_id is required"))
	}
	if _, err := uuid.Parse(req.SenderID); err != nil {
		return response.Error(c, apperror.BadRequest("sender_id must be a valid UUID"))
	}
	switch req.SenderRole {
	case "admin", "manager", "member", "readonly", "bot":
		// ok
	default:
		return response.Error(c, apperror.BadRequest("sender_role must be one of: admin, manager, member, readonly, bot"))
	}
	switch req.ChatType {
	case "direct", "group":
		// ok
	default:
		return response.Error(c, apperror.BadRequest("chat_type must be one of: direct, group"))
	}
	if strings.TrimSpace(req.MessageText) == "" {
		return response.Error(c, apperror.BadRequest("message_text is required"))
	}

	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	result, err := h.svc.ClassifyNotification(c.Context(), userID.String(), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, result)
}

// POST /ai/notification-priority/feedback
func (h *NotificationHandler) notificationFeedback(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req model.NotificationFeedbackRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if strings.TrimSpace(req.MessageID) == "" {
		return response.Error(c, apperror.BadRequest("message_id is required"))
	}
	if _, err := uuid.Parse(req.MessageID); err != nil {
		return response.Error(c, apperror.BadRequest("message_id must be a valid UUID"))
	}

	validPriorities := map[string]bool{"urgent": true, "important": true, "normal": true, "low": true}
	if !validPriorities[req.ClassifiedPriority] {
		return response.Error(c, apperror.BadRequest("classified_priority must be one of: urgent, important, normal, low"))
	}
	if !validPriorities[req.UserOverride] {
		return response.Error(c, apperror.BadRequest("user_override_priority must be one of: urgent, important, normal, low"))
	}

	if err := h.store.SaveFeedback(c.Context(), userID.String(), req.MessageID, req.ClassifiedPriority, req.UserOverride); err != nil {
		return response.Error(c, apperror.Internal(err.Error()))
	}

	return c.SendStatus(fiber.StatusNoContent)
}

// GET /ai/notification-priority/stats
func (h *NotificationHandler) notificationStats(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	stats, err := h.store.GetStats(c.Context(), userID.String(), 7)
	if err != nil {
		return response.Error(c, apperror.Internal(err.Error()))
	}
	return response.JSON(c, fiber.StatusOK, stats)
}
