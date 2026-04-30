// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type SearchHandler struct {
	searchSvc    *service.SearchService
	historyStore store.SearchHistoryStore
	logger       *slog.Logger
}

func NewSearchHandler(searchSvc *service.SearchService, logger *slog.Logger, opts ...interface{}) *SearchHandler {
	h := &SearchHandler{
		searchSvc: searchSvc,
		logger:    logger,
	}
	for _, opt := range opts {
		if hs, ok := opt.(store.SearchHistoryStore); ok {
			h.historyStore = hs
		}
	}
	return h
}

func (h *SearchHandler) Register(app fiber.Router) {
	app.Get("/search/history", h.GetSearchHistory)
	app.Post("/search/history", h.SaveSearchHistory)
	app.Delete("/search/history", h.ClearSearchHistory)
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
			case "text", "photo", "video", "file", "files", "voice", "videonote", "video_note", "sticker", "gif", "system", "link", "links":
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
		return response.JSON(c, fiber.StatusOK, fiber.Map{
			"results":  []map[string]interface{}{},
			"total":    0,
			"query":    query,
			"scope":    scope,
			"degraded": true,
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

func (h *SearchHandler) GetSearchHistory(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	if h.historyStore == nil {
		return response.JSON(c, fiber.StatusOK, fiber.Map{"history": []interface{}{}})
	}

	limit := c.QueryInt("limit", 10)
	entries, err := h.historyStore.List(c.Context(), uid, limit)
	if err != nil {
		return response.Error(c, err)
	}

	if entries == nil {
		entries = []model.SearchHistoryEntry{}
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"history": entries})
}

func (h *SearchHandler) SaveSearchHistory(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	if h.historyStore == nil {
		return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
	}

	var req struct {
		Query string `json:"query"`
		Scope string `json:"scope"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.Error(c, apperror.BadRequest("Invalid request body"))
	}

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return response.Error(c, apperror.BadRequest("query is required"))
	}

	scope := req.Scope
	if scope == "" {
		scope = "global"
	}

	if err := h.historyStore.Save(c.Context(), uid, query, scope); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}

func (h *SearchHandler) ClearSearchHistory(c *fiber.Ctx) error {
	uid, err := getUserID(c)
	if err != nil {
		return response.Error(c, err)
	}

	if h.historyStore == nil {
		return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
	}

	if err := h.historyStore.Clear(c.Context(), uid); err != nil {
		return response.Error(c, err)
	}

	return response.JSON(c, fiber.StatusOK, fiber.Map{"ok": true})
}
