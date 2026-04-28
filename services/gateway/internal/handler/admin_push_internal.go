// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/gateway/internal/push"
)

// adminPushDispatcher is the contract the internal admin-push endpoint needs
// from the push package. Defined here so dispatcher_test can stub it without
// reaching into webpush internals.
type adminPushDispatcher interface {
	SendToUserWithReport(ctx context.Context, userID string, payload []byte) (*push.Report, error)
	Enabled() bool
}

// AdminPushInternalConfig wires the internal "test push" endpoint exposed to
// the messaging admin handler. It is gated behind X-Internal-Token because
// it bypasses the user-facing JWT path entirely; the SysManageSettings check
// happens upstream in messaging before this is called.
type AdminPushInternalConfig struct {
	Dispatcher adminPushDispatcher
	Logger     *slog.Logger
	// Timeout caps the entire dispatch (fetch subscriptions + per-device
	// sends). Default 10s. Bounded by the caller's deadline if shorter.
	Timeout time.Duration
}

// adminTestPushRequest is the wire body the messaging service POSTs.
//
// Title/body are short rendered strings the SW will display via the default
// notification path. Payload bytes are constructed here (not by messaging)
// so the gateway is the single source of truth for the SW push schema —
// keeps the SW push contract from drifting across services.
type adminTestPushRequest struct {
	UserID string `json:"user_id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

const (
	adminPushDefaultTitle = "Orbit test push"
	adminPushDefaultBody  = "If you can read this, push delivery to this device works."
	adminPushMaxTitleLen  = 200
	adminPushMaxBodyLen   = 1000
)

// RegisterAdminPushInternalRoute mounts POST /push/dispatch-test on the
// provided fiber group. The caller is expected to mount the group at
// /internal with RequireInternalToken middleware already applied — keeps
// the auth+prefix wiring in main.go and this handler idempotent across tests.
// Effective path: POST /internal/push/dispatch-test.
func RegisterAdminPushInternalRoute(group fiber.Router, cfg AdminPushInternalConfig) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	group.Post("/push/dispatch-test", func(c *fiber.Ctx) error {
		if cfg.Dispatcher == nil || !cfg.Dispatcher.Enabled() {
			return response.Error(c, apperror.ServiceUnavailable("Push dispatcher disabled"))
		}

		var req adminTestPushRequest
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return response.Error(c, apperror.BadRequest("Invalid JSON body"))
		}
		if req.UserID == "" {
			return response.Error(c, apperror.BadRequest("user_id required"))
		}
		if len(req.Title) > adminPushMaxTitleLen {
			return response.Error(c, apperror.BadRequest("title too long"))
		}
		if len(req.Body) > adminPushMaxBodyLen {
			return response.Error(c, apperror.BadRequest("body too long"))
		}

		title := req.Title
		if title == "" {
			title = adminPushDefaultTitle
		}
		body := req.Body
		if body == "" {
			body = adminPushDefaultBody
		}

		// Standard PushData shape that the SW's default path renders via
		// showNotification. is_silent=false ensures it actually pings the
		// user — defeats the purpose of a "test" if the device suppresses it.
		payload, err := json.Marshal(map[string]interface{}{
			"title": title,
			"body":  body,
			"data": map[string]interface{}{
				"is_silent": false,
				"priority":  "normal",
			},
		})
		if err != nil {
			logger.Error("marshal admin test push payload", "error", err)
			return response.Error(c, apperror.Internal("payload encoding failed"))
		}

		ctx, cancel := context.WithTimeout(c.Context(), timeout)
		defer cancel()

		report, err := cfg.Dispatcher.SendToUserWithReport(ctx, req.UserID, payload)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return response.Error(c, apperror.GatewayTimeout("Push dispatch timed out"))
			}
			logger.Warn("admin test push dispatch failed",
				"error", err, "user_id", req.UserID)
			return response.Error(c, apperror.BadGateway("Push dispatch failed"))
		}

		return response.JSON(c, fiber.StatusOK, report)
	})
}
