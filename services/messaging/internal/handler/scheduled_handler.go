// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// ScheduledHandler handles HTTP requests for scheduled messages.
type ScheduledHandler struct {
	svc    *service.ScheduledMessageService
	logger *slog.Logger
}

// NewScheduledHandler creates a new ScheduledHandler.
func NewScheduledHandler(svc *service.ScheduledMessageService, logger *slog.Logger) *ScheduledHandler {
	return &ScheduledHandler{svc: svc, logger: logger}
}

// Register registers scheduled message routes.
func (h *ScheduledHandler) Register(app fiber.Router) {
	app.Get("/chats/:id/messages/scheduled", h.ListScheduled)
	app.Post("/chats/:id/messages/scheduled", h.Schedule)
	app.Patch("/messages/:id/scheduled", h.Edit)
	app.Delete("/messages/:id/scheduled", h.Delete)
	app.Post("/messages/:id/scheduled/send-now", h.SendNow)
}

func (h *ScheduledHandler) ListScheduled(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}
	msgs, err := h.svc.ListScheduled(c.Context(), chatID, userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, msgs)
}

func (h *ScheduledHandler) Schedule(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	chatID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid chat ID"))
	}
	var body struct {
		Content          string          `json:"content"`
		Question         string          `json:"question"`
		Entities         json.RawMessage `json:"entities"`
		Solution         string          `json:"solution"`
		SolutionEntities json.RawMessage `json:"solution_entities"`
		ReplyToID        *string         `json:"reply_to_id"`
		Type             string          `json:"type"`
		MediaIDs         []string        `json:"media_ids"`
		IsSpoiler        bool            `json:"is_spoiler"`
		Options          []string        `json:"options"`
		IsAnonymous      *bool           `json:"is_anonymous"`
		IsMultiple       bool            `json:"is_multiple"`
		IsQuiz           bool            `json:"is_quiz"`
		CorrectOption    *int            `json:"correct_option"`
		ScheduledAt      string          `json:"scheduled_at"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	if len(body.SolutionEntities) > 65536 {
		return response.Error(c, apperror.BadRequest("solution_entities too large (max 64KB)"))
	}
	if body.ScheduledAt == "" {
		return response.Error(c, apperror.BadRequest("scheduled_at is required"))
	}
	scheduledAt, err := time.Parse(time.RFC3339, body.ScheduledAt)
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid scheduled_at format, use RFC3339"))
	}
	if body.Type == "" {
		body.Type = "text"
	}

	var replyToID *uuid.UUID
	if body.ReplyToID != nil && *body.ReplyToID != "" {
		parsed, err := uuid.Parse(*body.ReplyToID)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid reply_to_id"))
		}
		replyToID = &parsed
	}

	var mediaIDs []uuid.UUID
	for _, idStr := range body.MediaIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid media_id: "+idStr))
		}
		mediaIDs = append(mediaIDs, id)
	}

	var scheduledPoll *model.ScheduledPollPayload
	if body.Type == "poll" {
		question := body.Question
		if question == "" {
			question = body.Content
		}
		isAnonymous := true
		if body.IsAnonymous != nil {
			isAnonymous = *body.IsAnonymous
		}
		solution := strings.TrimSpace(body.Solution)
		var solutionPtr *string
		if solution != "" {
			solutionPtr = &solution
		}
		scheduledPoll = &model.ScheduledPollPayload{
			Question:         question,
			Options:          body.Options,
			IsAnonymous:      isAnonymous,
			IsMultiple:       body.IsMultiple,
			IsQuiz:           body.IsQuiz,
			CorrectOption:    body.CorrectOption,
			Solution:         solutionPtr,
			SolutionEntities: body.SolutionEntities,
		}
	}

	msg, err := h.svc.Schedule(c.Context(), chatID, userID, service.ScheduleMessageInput{
		Content:     body.Content,
		Entities:    body.Entities,
		ReplyToID:   replyToID,
		Type:        body.Type,
		MediaIDs:    mediaIDs,
		IsSpoiler:   body.IsSpoiler,
		Poll:        scheduledPoll,
		ScheduledAt: scheduledAt,
	})
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 201, msg)
}

func (h *ScheduledHandler) Edit(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	var body struct {
		Content     *string         `json:"content"`
		Entities    json.RawMessage `json:"entities"`
		ScheduledAt *string         `json:"scheduled_at"`
	}
	if err := c.BodyParser(&body); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}
	var scheduledAt *time.Time
	if body.ScheduledAt != nil {
		t, err := time.Parse(time.RFC3339, *body.ScheduledAt)
		if err != nil {
			return response.Error(c, apperror.BadRequest("Invalid scheduled_at format"))
		}
		scheduledAt = &t
	}
	msg, err := h.svc.Edit(c.Context(), msgID, userID, body.Content, body.Entities, scheduledAt)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, msg)
}

func (h *ScheduledHandler) Delete(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	if err := h.svc.Delete(c.Context(), msgID, userID); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, fiber.Map{"ok": true})
}

func (h *ScheduledHandler) SendNow(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, apperror.Unauthorized("Missing user context"))
	}
	msgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return response.Error(c, apperror.BadRequest("Invalid message ID"))
	}
	msg, err := h.svc.SendNow(c.Context(), msgID, userID)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, 200, msg)
}
