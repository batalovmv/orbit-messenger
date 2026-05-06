// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package service — OIDC directory-sync worker (ADR 006, phase B4).
//
// The worker ticks on a configurable interval (default 1h) and deactivates
// Orbit users whose OIDC subject is no longer present in the IdP directory.
// Conservative by design: any error from the directory client yields ZERO
// deactivations — we prefer a stale active account over a false deactivation.

package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// DirectoryClient is the IdP-side directory abstraction. Implementations
// MUST return the canonical OIDC subject for every currently-active user.
// A user missing from the returned slice is treated as deprovisioned —
// callers MUST be conservative here: prefer "still active on transient
// errors" over false deactivations (return the error, don't silently skip).
type DirectoryClient interface {
	ListActiveSubjects(ctx context.Context) ([]string, error)
}

// OIDCSyncConfig carries the parsed env-config for the sync worker.
type OIDCSyncConfig struct {
	// Enabled gates the worker; must be true for it to start.
	Enabled bool
	// Interval is how often SyncOnce is called. Defaults to 1h.
	Interval time.Duration
	// ProviderKey must match OIDCConfig.ProviderKey (e.g. "google").
	// Set by main.go after OIDC provider init.
	ProviderKey string
}

// LoadOIDCSyncConfigFromEnv reads OIDC_SYNC_* variables.
// OIDC_SYNC_ENABLED must be exactly "true" (case-insensitive) to enable.
// OIDC_SYNC_INTERVAL is a Go duration string (e.g. "30m", "2h"); defaults to 1h.
func LoadOIDCSyncConfigFromEnv(getenv func(string) string) *OIDCSyncConfig {
	cfg := &OIDCSyncConfig{
		Enabled:  strings.EqualFold(strings.TrimSpace(getenv("OIDC_SYNC_ENABLED")), "true"),
		Interval: time.Hour,
	}
	if raw := strings.TrimSpace(getenv("OIDC_SYNC_INTERVAL")); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil && d > 0 {
			cfg.Interval = d
		}
	}
	return cfg
}

// OIDCSyncWorker periodically syncs active Orbit users against the IdP
// directory and deactivates any user that has been removed from the directory.
type OIDCSyncWorker struct {
	cfg    *OIDCSyncConfig
	auth   *AuthService
	client DirectoryClient
	log    *slog.Logger
}

// NewOIDCSyncWorker constructs a worker. cfg, auth, client, and log must all
// be non-nil.
func NewOIDCSyncWorker(cfg *OIDCSyncConfig, auth *AuthService, client DirectoryClient, log *slog.Logger) *OIDCSyncWorker {
	return &OIDCSyncWorker{cfg: cfg, auth: auth, client: client, log: log}
}

// Run starts the ticker loop. Blocks until ctx is cancelled. Errors from
// individual sync passes are logged but do not stop the loop.
func (w *OIDCSyncWorker) Run(ctx context.Context) {
	w.log.InfoContext(ctx, "oidc-sync: worker started",
		"provider", w.cfg.ProviderKey,
		"interval", w.cfg.Interval,
	)
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.InfoContext(ctx, "oidc-sync: worker stopped", "provider", w.cfg.ProviderKey)
			return
		case <-ticker.C:
			n, err := w.SyncOnce(ctx)
			if err != nil {
				w.log.ErrorContext(ctx, "oidc-sync: sync pass failed — no deactivations performed",
					"provider", w.cfg.ProviderKey,
					"error", err,
				)
			} else {
				w.log.InfoContext(ctx, "oidc-sync: sync pass complete",
					"provider", w.cfg.ProviderKey,
					"deactivated", n,
				)
			}
		}
	}
}

// SyncOnce performs a single sync pass and returns the number of users
// deactivated. Exposed for testing.
//
// Conservative contract: if ListActiveSubjects returns an error, SyncOnce
// returns (0, err) and deactivates nobody.
func (w *OIDCSyncWorker) SyncOnce(ctx context.Context) (deactivated int, err error) {
	// Step 1: fetch the authoritative set of active subjects from the IdP.
	subjects, err := w.client.ListActiveSubjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("oidc-sync: list active subjects: %w", err)
	}

	// Step 2: build a fast-lookup set.
	activeSet := make(map[string]struct{}, len(subjects))
	for _, s := range subjects {
		activeSet[s] = struct{}{}
	}

	// Step 3: enumerate active Orbit users bound to this provider.
	users, err := w.auth.users.ListOIDCActiveUsers(ctx, w.cfg.ProviderKey)
	if err != nil {
		return 0, fmt.Errorf("oidc-sync: list orbit users: %w", err)
	}

	// Step 4: for each user whose subject is absent from the IdP, deactivate.
	for _, u := range users {
		if _, found := activeSet[u.Subject]; found {
			continue // still active in IdP — leave them alone
		}

		// 4a. Mark user inactive in the database.
		if dErr := w.auth.users.Deactivate(ctx, u.ID); dErr != nil {
			w.log.ErrorContext(ctx, "oidc-sync: deactivate user failed",
				"provider", w.cfg.ProviderKey,
				"user_id", u.ID,
				"subject", u.Subject,
				"error", dErr,
			)
			continue // best-effort; try remaining users
		}

		// 4b. Delete all sessions so the refresh-token flow stops working.
		if dErr := w.auth.sessions.DeleteAllByUser(ctx, u.ID); dErr != nil {
			w.log.ErrorContext(ctx, "oidc-sync: delete sessions failed",
				"provider", w.cfg.ProviderKey,
				"user_id", u.ID,
				"subject", u.Subject,
				"error", dErr,
			)
			// Still continue — deactivation already happened; the user
			// is marked inactive so login will be refused. The stale
			// sessions will expire naturally via ExpiresAt.
		}

		// 4c. Set the per-user JWT blacklist so the gateway rejects
		// any in-flight access tokens immediately (closes the 30s
		// gateway-cache window described in Day 5.2 session revoke).
		// Key: "jwt_blacklist:user:<userID>" — checked by both the
		// HTTP middleware and the WS handler in services/gateway.
		blacklistKey := "jwt_blacklist:user:" + u.ID.String()
		if bErr := w.auth.redis.Set(ctx, blacklistKey, "1", w.auth.cfg.AccessTTL).Err(); bErr != nil {
			w.log.ErrorContext(ctx, "oidc-sync: redis blacklist write failed",
				"provider", w.cfg.ProviderKey,
				"user_id", u.ID,
				"subject", u.Subject,
				"error", bErr,
			)
			// Still count as deactivated — DB is the source of truth.
		}

		w.log.InfoContext(ctx, "oidc-sync: deactivated user",
			"provider", w.cfg.ProviderKey,
			"user_id", u.ID,
			"subject", u.Subject,
		)
		deactivated++
	}

	return deactivated, nil
}
