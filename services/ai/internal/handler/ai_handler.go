// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"fmt"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/pkg/validator"
	"github.com/mst-corp/orbit/services/ai/internal/model"
	"github.com/mst-corp/orbit/services/ai/internal/service"
)

type AIHandler struct {
	svc    *service.AIService
	logger *slog.Logger
}

func NewAIHandler(svc *service.AIService, logger *slog.Logger) *AIHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &AIHandler{svc: svc, logger: logger}
}

// Register wires the 6 AI endpoints onto the given router. Expected to be
// called on a group that already applied RequireInternalToken so every
// handler can trust X-User-ID.
func (h *AIHandler) Register(router fiber.Router) {
	router.Post("/ai/summarize", h.summarize)
	router.Post("/ai/translate", h.translate)
	router.Post("/ai/reply-suggest", h.suggestReply)
	router.Post("/ai/transcribe", h.transcribe)
	router.Post("/ai/search", h.search)
	router.Post("/ai/ask", h.ask)
	router.Get("/ai/usage", h.usage)
	router.Get("/ai/capabilities", h.capabilities)
}

// GET /ai/capabilities — frontend uses this to hide UI for unconfigured
// providers (e.g. the Transcribe button when OPENAI_API_KEY is empty).
// Auth: any authenticated user — the response carries no secrets.
func (h *AIHandler) capabilities(c *fiber.Ctx) error {
	if _, err := getUserID(c); err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, model.CapabilitiesResponse{
		AnthropicConfigured: h.svc.AnthropicConfigured(),
		WhisperConfigured:   h.svc.WhisperConfigured(),
	})
}

// POST /ai/ask — single-turn Claude call used by the @orbit-ai mention
// bot in messaging. Request body: {chat_id, prompt}; response:
// {reply}.
func (h *AIHandler) ask(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req model.AskRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return response.Error(c, err)
	}
	if err := validator.RequireString(req.Prompt, "prompt", 1, 4096); err != nil {
		return response.Error(c, err)
	}

	reply, err := h.svc.Ask(c.Context(), userID.String(), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, model.AskResponse{Reply: reply})
}

// POST /ai/summarize — SSE stream of text chunks
func (h *AIHandler) summarize(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req model.SummarizeRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return response.Error(c, err)
	}
	if req.TimeRange != "1h" && req.TimeRange != "6h" && req.TimeRange != "24h" && req.TimeRange != "7d" {
		return response.Error(c, apperror.BadRequest("time_range must be one of: 1h, 6h, 24h, 7d"))
	}
	if err := validator.RequireString(req.Language, "language", 2, 10); err != nil {
		return response.Error(c, err)
	}

	events, err := h.svc.Summarize(c.Context(), userID.String(), req)
	if err != nil {
		return response.Error(c, err)
	}
	return streamClaudeEvents(c, events)
}

// POST /ai/translate — SSE stream of text chunks
func (h *AIHandler) translate(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req model.TranslateRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if len(req.MessageIDs) == 0 {
		return response.Error(c, apperror.BadRequest("message_ids is required"))
	}
	if len(req.MessageIDs) > 50 {
		return response.Error(c, apperror.BadRequest("message_ids must not exceed 50 items"))
	}
	for i, id := range req.MessageIDs {
		if err := validator.RequireUUID(id, fmt.Sprintf("message_ids[%d]", i)); err != nil {
			return response.Error(c, err)
		}
	}
	if err := validator.RequireString(req.TargetLanguage, "target_language", 2, 10); err != nil {
		return response.Error(c, err)
	}
	if req.ResponseFormat != "" && req.ResponseFormat != "json_map" {
		return response.Error(c, apperror.BadRequest("response_format must be empty or \"json_map\""))
	}

	events, err := h.svc.Translate(c.Context(), userID.String(), req)
	if err != nil {
		return response.Error(c, err)
	}
	return streamClaudeEvents(c, events)
}

// POST /ai/reply-suggest — non-streaming, returns {suggestions: [..., ..., ...]}
func (h *AIHandler) suggestReply(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req model.ReplySuggestRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
		return response.Error(c, err)
	}

	suggestions, err := h.svc.SuggestReply(c.Context(), userID.String(), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, model.ReplySuggestResponse{Suggestions: suggestions})
}

// POST /ai/transcribe — non-streaming Whisper call
func (h *AIHandler) transcribe(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req model.TranscribeRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := validator.RequireUUID(req.MediaID, "media_id"); err != nil {
		return response.Error(c, err)
	}

	result, err := h.svc.Transcribe(c.Context(), userID.String(), req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, result)
}

// POST /ai/search — returns 501 Not Implemented for Phase 8A (embeddings
// and pgvector are deferred to Phase 8A.2). Endpoint exists so the frontend
// can call it without 404 and the UI button can hide itself gracefully.
func (h *AIHandler) search(c *fiber.Ctx) error {
	_, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	var req model.SearchRequest
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	if err := validator.RequireString(req.Query, "query", 1, 512); err != nil {
		return response.Error(c, err)
	}
	if req.ChatID != "" {
		if err := validator.RequireUUID(req.ChatID, "chat_id"); err != nil {
			return response.Error(c, err)
		}
	}

	result, err := h.svc.SemanticSearch(c.Context(), "", req)
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, result)
}

// GET /ai/usage — per-user usage stats for the last 30 days
func (h *AIHandler) usage(c *fiber.Ctx) error {
	userID, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	stats, err := h.svc.GetUsage(c.Context(), userID.String())
	if err != nil {
		return response.Error(c, err)
	}
	return response.JSON(c, fiber.StatusOK, stats)
}
