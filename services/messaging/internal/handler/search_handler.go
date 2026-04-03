package handler

import (
	"log/slog"
	"strings"
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
	if len([]rune(query)) < 1 {
		return response.Error(c, apperror.BadRequest("query must not be empty"))
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
		if raw := firstNonEmpty(c.Query("from"), c.Query("from_user_id")); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid from"))
			}
			fromUserID = &id
		}

		var dateFrom *time.Time
		if raw := firstNonEmpty(c.Query("after"), c.Query("date_from")); raw != "" {
			t, err := parseSearchTime(raw, false)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid after: use RFC3339 or YYYY-MM-DD format"))
			}
			dateFrom = &t
		}

		var dateTo *time.Time
		if raw := firstNonEmpty(c.Query("before"), c.Query("date_to")); raw != "" {
			t, err := parseSearchTime(raw, true)
			if err != nil {
				return response.Error(c, apperror.BadRequest("Invalid before: use RFC3339 or YYYY-MM-DD format"))
			}
			dateTo = &t
		}

		var msgType *string
		if raw := c.Query("type"); raw != "" {
			switch strings.ToLower(raw) {
			case "text", "photo", "video", "file", "files", "voice", "video_note", "sticker", "gif", "system", "link", "links":
				canonicalType := canonicalizeMessageType(raw)
				msgType = &canonicalType
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

func canonicalizeMessageType(value string) string {
	switch strings.ToLower(value) {
	case "files":
		return "file"
	case "links":
		return "link"
	default:
		return strings.ToLower(value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func parseSearchTime(value string, shouldUseEndOfDay bool) (time.Time, error) {
	if timestamp, err := time.Parse(time.RFC3339, value); err == nil {
		return timestamp, nil
	}

	dateOnly, err := time.Parse("2006-01-02", value)
	if err != nil {
		return time.Time{}, err
	}

	if shouldUseEndOfDay {
		return dateOnly.Add(24*time.Hour - time.Millisecond), nil
	}

	return dateOnly, nil
}
