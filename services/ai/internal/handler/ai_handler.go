package handler

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
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
