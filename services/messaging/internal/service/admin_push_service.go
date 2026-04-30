// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// PushTestHTTPClient is the minimal contract the inspector needs from
// http.Client so tests can stub out the gateway round-trip.
type PushTestHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// PushTestDeviceReport mirrors the shape gateway returns. We do not import
// the gateway types here because messaging must compile without the gateway
// package as a dependency (separate go.mod / cyclic concerns).
type PushTestDeviceReport struct {
	DeviceID     string `json:"device_id"`
	UserAgent    string `json:"user_agent,omitempty"`
	EndpointHost string `json:"endpoint_host"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

type PushTestReport struct {
	UserID      string                 `json:"user_id"`
	Email       string                 `json:"email,omitempty"`
	DisplayName string                 `json:"display_name,omitempty"`
	DeviceCount int                    `json:"device_count"`
	Sent        int                    `json:"sent"`
	Failed      int                    `json:"failed"`
	Stale       int                    `json:"stale"`
	Devices     []PushTestDeviceReport `json:"devices"`
}

// PushAdminService handles the Day 5 Push Inspector admin endpoint. Kept
// separate from AdminService because it carries an HTTP dependency on the
// gateway (no other admin method needs to reach back to gateway).
type PushAdminService struct {
	users          store.UserStore
	audit          store.AuditStore
	gatewayURL     string
	internalSecret string
	httpClient     PushTestHTTPClient
	timeout        time.Duration
	logger         *slog.Logger
}

type PushAdminConfig struct {
	Users          store.UserStore
	Audit          store.AuditStore
	GatewayURL     string
	InternalSecret string
	HTTPClient     PushTestHTTPClient
	// Timeout caps the entire gateway round-trip including its per-device
	// dispatch loop. Default 10s. Caller's ctx deadline still wins if shorter.
	Timeout time.Duration
	Logger  *slog.Logger
}

func NewPushAdminService(cfg PushAdminConfig) *PushAdminService {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		// Outer transport budget. MUST exceed the call timeout below by
		// enough to drain the gateway response body (otherwise the http
		// client tears down the conn before we read the report).
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		// Hierarchy of budgets, slowest → fastest:
		//   messaging callGateway ctx           = 12s  (this value)
		//   gateway admin_push_internal handler = 10s  (default in handler)
		//   webpush per-attempt (defaultRequestTimeout) = 5s × up-to-3 retries
		// The 2s gap between messaging (12s) and gateway (10s) is response
		// RTT slack — without it, messaging declares a timeout AND the
		// gateway delivers the push, producing a "you got nothing back but
		// the user got two pushes when you re-clicked" failure mode.
		timeout = 12 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &PushAdminService{
		users:          cfg.Users,
		audit:          cfg.Audit,
		gatewayURL:     strings.TrimRight(cfg.GatewayURL, "/"),
		internalSecret: cfg.InternalSecret,
		httpClient:     httpClient,
		timeout:        timeout,
		logger:         logger,
	}
}

// SendTestPushParams is the unified input for SendTestPush. UserID and Email
// are mutually exclusive at the handler boundary; the service rejects both
// being set.
type SendTestPushParams struct {
	ActorID   uuid.UUID
	ActorRole string
	UserID    string
	Email     string
	Title     string
	Body      string
	IP        string
	UserAgent string
}

const (
	pushTestMaxTitleLen = 200
	pushTestMaxBodyLen  = 1000
)

// SendTestPush resolves the target user (by id or email), records audit
// intent, then calls the gateway dispatch-test endpoint and returns the
// per-device report. Audit always lands BEFORE dispatch — partial failure
// after audit is preferable to a successful push with no audit trail.
func (s *PushAdminService) SendTestPush(ctx context.Context, p SendTestPushParams) (*PushTestReport, error) {
	if p.ActorRole != "superadmin" {
		return nil, apperror.Forbidden("Push inspector requires superadmin")
	}
	if len(p.Title) > pushTestMaxTitleLen {
		return nil, apperror.BadRequest("title too long")
	}
	if len(p.Body) > pushTestMaxBodyLen {
		return nil, apperror.BadRequest("body too long")
	}

	target, err := s.resolveTarget(ctx, p.UserID, p.Email)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, apperror.NotFound("User not found")
	}
	if !target.IsActive {
		return nil, apperror.BadRequest("User is deactivated")
	}

	// Audit the *intent* (actor, target, title preview) — never the per-device
	// outcomes. Provider error bodies can echo opaque IDs, and we don't want
	// those in audit_log noise. The summary counts could be appended after
	// dispatch but that would mean two audit rows or a write-after-read race.
	auditDetails := map[string]interface{}{
		"target_user_id": target.ID.String(),
	}
	if title := truncate(p.Title, 100); title != "" {
		auditDetails["title"] = title
	}
	if err := s.writeAudit(ctx, p.ActorID, model.AuditPushTestSent, "user", target.ID.String(),
		auditDetails, p.IP, p.UserAgent); err != nil {
		return nil, apperror.Internal("audit log write failed")
	}

	report, err := s.callGateway(ctx, target.ID, p.Title, p.Body)
	if err != nil {
		return nil, err
	}

	report.UserID = target.ID.String()
	report.Email = target.Email
	report.DisplayName = target.DisplayName
	return report, nil
}

func (s *PushAdminService) resolveTarget(ctx context.Context, userID, email string) (*model.User, error) {
	hasID := userID != ""
	hasEmail := email != ""
	if hasID && hasEmail {
		return nil, apperror.BadRequest("provide user_id OR email, not both")
	}
	if !hasID && !hasEmail {
		return nil, apperror.BadRequest("user_id or email required")
	}
	if hasID {
		id, err := uuid.Parse(userID)
		if err != nil {
			return nil, apperror.BadRequest("Invalid user_id")
		}
		u, err := s.users.GetByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get user by id: %w", err)
		}
		return u, nil
	}
	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("get user by email: %w", err)
	}
	return u, nil
}

func (s *PushAdminService) callGateway(ctx context.Context, targetID uuid.UUID, title, body string) (*PushTestReport, error) {
	if s.gatewayURL == "" {
		return nil, apperror.ServiceUnavailable("Push inspector not configured")
	}

	reqBody, err := json.Marshal(map[string]string{
		"user_id": targetID.String(),
		"title":   title,
		"body":    body,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal gateway request: %w", err)
	}

	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost,
		s.gatewayURL+"/internal/push/dispatch-test", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create gateway request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", s.internalSecret)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return nil, apperror.GatewayTimeout("Gateway dispatch timed out")
		}
		s.logger.Warn("admin test push gateway call failed", "error", err)
		return nil, apperror.BadGateway("Gateway unreachable")
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		// Read body for diagnostic logging only; do NOT echo it to caller —
		// gateway errors may include internal-only context.
		var diag bytes.Buffer
		_, _ = io.Copy(&diag, io.LimitReader(resp.Body, 4096))
		s.logger.Warn("admin test push gateway returned non-200",
			"status", resp.StatusCode, "body", diag.String())
		if resp.StatusCode == http.StatusServiceUnavailable {
			return nil, apperror.ServiceUnavailable("Push dispatcher disabled on gateway")
		}
		return nil, apperror.BadGateway(fmt.Sprintf("Gateway returned status %d", resp.StatusCode))
	}

	var report PushTestReport
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&report); err != nil {
		return nil, fmt.Errorf("decode gateway report: %w", err)
	}
	return &report, nil
}

func (s *PushAdminService) writeAudit(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details map[string]interface{}, ip, ua string) error {
	tID := targetID
	entry := &model.AuditEntry{
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   &tID,
	}
	if len(details) > 0 {
		entry.Details, _ = json.Marshal(details)
	}
	if ip != "" {
		entry.IPAddress = &ip
	}
	if ua != "" {
		entry.UserAgent = &ua
	}
	if err := s.audit.Log(ctx, entry); err != nil {
		s.logger.Error("audit log write failed",
			"error", err, "action", action, "actor_id", actorID)
		return err
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
