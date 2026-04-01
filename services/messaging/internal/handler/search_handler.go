package handler

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

type SearchHandler struct {
	searchSvc *service.SearchService
	logger    *slog.Logger
}

func NewSearchHandler(searchSvc *service.SearchService, logger *slog.Logger) *SearchHandler {
	return &SearchHandler{
		searchSvc: searchSvc,
		logger:    logger,
	}
}

func (h *SearchHandler) Register(app fiber.Router) {
	app.Get("/search", h.Search)
}

func (h *SearchHandler) Search(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	query := c.Query("q")
	if len(query) < 2 {
		return response.Error(c, apperror.BadRequest("query must be at least 2 characters"))
	}

	scope := c.Query("scope", "messages")
	switch scope {
	case "messages", "users", "chats":
		// valid
	default:
		return response.Error(c, apperror.BadRequest("scope must be one of: messages, users, chats"))
	}

	limit := c.QueryInt("limit", 20)
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := c.QueryInt("offset", 0)
	if offset < 0 {
		offset = 0
	}

	var hits []map[string]interface{}
	var total int

	switch scope {
	case "messages":
		var chatID *uuid.UUID
		if raw := c.Query("chat_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid chat_id"))
			}
			chatID = &id
		}

		var fromUserID *uuid.UUID
		if raw := c.Query("from_user_id"); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid from_user_id"))
			}
			fromUserID = &id
		}

		var dateFrom *time.Time
		if raw := c.Query("date_from"); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid date_from: use RFC3339 format"))
			}
			dateFrom = &t
		}

		var dateTo *time.Time
		if raw := c.Query("date_to"); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid date_to: use RFC3339 format"))
			}
			dateTo = &t
		}

		var msgType *string
		if raw := c.Query("type"); raw != "" {
			switch raw {
			case "text", "photo", "video", "file", "voice", "video_note", "sticker", "gif", "system":
				msgType = &raw
			default:
				return response.Error(c, apperror.BadRequest("Invalid message type"))
			}
		}

		var hasMedia *bool
		if raw := c.Query("has_media"); raw != "" {
			b := raw == "true" || raw == "1"
			hasMedia = &b
		}

		hits, total, err = h.searchSvc.SearchMessages(
			c.Context(), uid, query,
			chatID, fromUserID,
			dateFrom, dateTo,
			msgType, hasMedia,
			limit, offset,
		)

	case "users":
		hits, total, err = h.searchSvc.SearchUsers(c.Context(), query, limit)

	case "chats":
		hits, total, err = h.searchSvc.SearchChats(c.Context(), uid, query, limit)
	}

	if err != nil {
		h.logger.Error("search failed", "error", err, "scope", scope, "query", query, "user_id", uid)
		// Return empty results instead of 500 when Meilisearch is down or misconfigured.
		// Root cause: Meilisearch may not have indexed data or may be unreachable.
		// The error is logged server-side for ops debugging.
		return response.JSON(c, fiber.StatusOK, fiber.Map{
			"results": []map[string]interface{}{},
			"total":   0,
			"query":   query,
			"scope":   scope,
		})
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{
		"results": hits,
		"total":   total,
		"query":   query,
		"scope":   scope,
	})
}
