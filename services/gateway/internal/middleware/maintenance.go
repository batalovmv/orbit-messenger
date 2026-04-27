// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

// MaintenanceConfig wires the middleware to the messaging service which
// owns the feature_flags table and to a logger.
type MaintenanceConfig struct {
	// MessagingURL points at the messaging service. The middleware fetches
	// `<MessagingURL>/public/system/config` periodically. Required.
	MessagingURL string
	// PollInterval controls how often the middleware refreshes its in-memory
	// snapshot. Defaults to 10s. Maintenance toggling is rare, so this trades
	// a small propagation lag (≤ 10s) for resilience against messaging
	// service blips.
	PollInterval time.Duration
}

type maintenanceState struct {
	Active      bool   `json:"active"`
	Message     string `json:"message"`
	BlockWrites bool   `json:"block_writes"`
}

type publicConfig struct {
	Maintenance maintenanceState `json:"maintenance"`
}

// MaintenanceMiddleware blocks mutating requests when maintenance mode is
// active AND `block_writes=true` AND the caller is not a superadmin.
//
// Reads are always allowed: the banner is rendered client-side and we want
// users to be able to refresh their state, view existing chats, log in/out,
// etc. Writes are blocked at the gateway only — services downstream remain
// reachable for service-to-service traffic, scheduled-message cron, and
// integration webhooks (which arrive on dedicated paths upstream of this
// middleware).
//
// Failure mode: if the messaging service is unreachable on first poll, we
// fail OPEN — maintenance is treated as off. Operationally, the alternative
// (fail closed) means a single messaging hiccup locks every operator out
// of the very service they need to fix the situation.
func MaintenanceMiddleware(cfg MaintenanceConfig) fiber.Handler {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 10 * time.Second
	}
	holder := newMaintenanceHolder(cfg)
	holder.start()

	return func(c *fiber.Ctx) error {
		if !isMutating(c.Method()) {
			return c.Next()
		}
		st := holder.load()
		if !st.Active || !st.BlockWrites {
			return c.Next()
		}
		// Superadmins bypass the gate so they can run the very admin endpoints
		// that disable maintenance mode. Role comes from the JWT middleware
		// upstream of this one — for unauthenticated requests it is empty,
		// which means they ARE blocked (correct: they cannot be admins).
		role := strings.ToLower(strings.TrimSpace(c.Get("X-User-Role")))
		if role == "superadmin" {
			return c.Next()
		}

		c.Set("Retry-After", "30")
		msg := st.Message
		if msg == "" {
			msg = "Service is in maintenance mode"
		}
		return response.Error(c, apperror.ServiceUnavailable(msg))
	}
}

func isMutating(method string) bool {
	switch method {
	case fiber.MethodPost, fiber.MethodPut, fiber.MethodPatch, fiber.MethodDelete:
		return true
	default:
		return false
	}
}

// maintenanceHolder is a tiny background poller. Using atomic.Pointer avoids
// taking a mutex on the hot request path.
type maintenanceHolder struct {
	cfg    MaintenanceConfig
	client *http.Client
	state  atomic.Pointer[maintenanceState]
	once   sync.Once
}

func newMaintenanceHolder(cfg MaintenanceConfig) *maintenanceHolder {
	h := &maintenanceHolder{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
	}
	zero := maintenanceState{}
	h.state.Store(&zero)
	return h
}

func (h *maintenanceHolder) load() maintenanceState {
	if p := h.state.Load(); p != nil {
		return *p
	}
	return maintenanceState{}
}

func (h *maintenanceHolder) start() {
	h.once.Do(func() {
		// First refresh runs synchronously with a short timeout so the very
		// first request after boot has accurate data when the messaging
		// service is reachable. Failure is non-fatal.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = h.refresh(ctx)
		cancel()
		go h.loop()
	})
}

func (h *maintenanceHolder) loop() {
	t := time.NewTicker(h.cfg.PollInterval)
	defer t.Stop()
	for range t.C {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := h.refresh(ctx); err != nil {
			slog.Warn("maintenance poller refresh failed", "error", err)
		}
		cancel()
	}
}

func (h *maintenanceHolder) refresh(ctx context.Context) error {
	url := strings.TrimRight(h.cfg.MessagingURL, "/") + "/public/system/config"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	if err != nil {
		return err
	}
	var cfg publicConfig
	if err := json.Unmarshal(body, &cfg); err != nil {
		return err
	}
	h.state.Store(&cfg.Maintenance)
	return nil
}
