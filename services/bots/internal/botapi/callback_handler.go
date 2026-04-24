// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/mst-corp/orbit/pkg/apperror"
)

func (h *BotAPIHandler) answerCallbackQuery(c *fiber.Ctx) error {
	bot, err := currentBot(c)
	if err != nil {
		return botError(c, err)
	}
	if err := h.checkRateLimit(c, bot.ID.String()); err != nil {
		return botError(c, err)
	}

	if h.redis == nil {
		return botError(c, apperror.Internal("Redis is not configured"))
	}

	var req AnswerCallbackQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return botError(c, apperror.BadRequest("Invalid request body"))
	}
	if strings.TrimSpace(req.CallbackQueryID) == "" {
		return botError(c, apperror.BadRequest("callback_query_id is required"))
	}

	payload, err := json.Marshal(map[string]any{
		"text":       req.Text,
		"show_alert": req.ShowAlert,
	})
	if err != nil {
		return botError(c, err)
	}

	if err := h.redis.Set(c.Context(), "callback_ack:"+req.CallbackQueryID, payload, 60*time.Second).Err(); err != nil {
		return botError(c, err)
	}

	return botSuccess(c, true)
}
