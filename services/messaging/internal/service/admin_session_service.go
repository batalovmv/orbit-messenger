// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
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
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// SessionHTTPClient is the minimal contract the admin sessions tab needs from
// http.Client so tests can stub out the auth round-trip.
type SessionHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// AdminSession is the safe DTO returned to the AdminPanel: token_hash and any
// other auth-internal field is stripped before it leaves the service. Field
// names match the auth response 1:1 except for is_current, which is computed
// here against the actor's jti.
type AdminSession struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	DeviceID   *string   `json:"device_id,omitempty"`
	IPAddress  *string   `json:"ip_address,omitempty"`
	UserAgent  *string   `json:"user_agent,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	IsCurrent  bool      `json:"is_current"`
}

// authSession is what auth's /internal endpoints return. token_hash is present
// in the JSON but never copied into AdminSession.
type authSession struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	DeviceID  *string   `json:"device_id,omitempty"`
	TokenHash string    `json:"token_hash,omitempty"`
	IPAddress *string   `json:"ip_address,omitempty"`
	UserAgent *string   `json:"user_agent,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionAdminService implements the admin Sessions tab (Day 5.2). Audit and
// policy live here; the auth service holds session storage and exposes
// /internal endpoints behind X-Internal-Token.
type SessionAdminService struct {
	users          store.UserStore
	audit          store.AuditStore
	nats           Publisher
	authURL        string
	internalSecret string
	httpClient     SessionHTTPClient
	timeout        time.Duration
	logger         *slog.Logger
}

type SessionAdminConfig struct {
	Users          store.UserStore
	Audit          store.AuditStore
	NATS           Publisher
	AuthURL        string
	InternalSecret string
	HTTPClient     SessionHTTPClient
	Timeout        time.Duration
	Logger         *slog.Logger
}

func NewSessionAdminService(cfg SessionAdminConfig) *SessionAdminService {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &SessionAdminService{
		users:          cfg.Users,
		audit:          cfg.Audit,
		nats:           cfg.NATS,
		authURL:        strings.TrimRight(cfg.AuthURL, "/"),
		internalSecret: cfg.InternalSecret,
		httpClient:     httpClient,
		timeout:        timeout,
		logger:         logger,
	}
}

// ListUserSessionsParams collects the list-call inputs. ActorSessionID is the
// caller's own jti (gateway forwards it as X-User-Session-ID); used to flag
// is_current on the matching row so the UI can hide the revoke button.
type ListUserSessionsParams struct {
	ActorID         uuid.UUID
	ActorRole       string
	ActorSessionID  string
	TargetUserID    string
}

// ListUserSessions returns the target user's sessions enriched with is_current.
// Gated by SysManageUsers + role hierarchy (CanModifyUser). The hierarchy
// check is symmetric with RevokeSession — without it, a plain admin could
// enumerate session metadata (IPs, user-agents) of a superadmin even though
// they could not act on those sessions. No audit row — listing is
// non-destructive and would flood audit_log.
//
// Self-inspection is allowed even when actor == target: an admin reviewing
// their own active sessions is the natural read path; CanModifyUser returns
// true for equal-role-but-same-user via the explicit self check below.
func (s *SessionAdminService) ListUserSessions(ctx context.Context, p ListUserSessionsParams) ([]AdminSession, error) {
	if !permissions.HasSysPermission(p.ActorRole, permissions.SysManageUsers) {
		return nil, apperror.Forbidden("Insufficient permissions")
	}
	targetID, err := uuid.Parse(p.TargetUserID)
	if err != nil {
		return nil, apperror.BadRequest("Invalid user ID")
	}
	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		return nil, apperror.NotFound("User not found")
	}

	if targetID != p.ActorID && !permissions.CanModifyUser(p.ActorRole, target.Role) {
		return nil, apperror.Forbidden("Cannot inspect sessions of a user with equal or higher role")
	}

	authSessions, err := s.fetchSessions(ctx, targetID)
	if err != nil {
		return nil, err
	}

	out := make([]AdminSession, 0, len(authSessions))
	for _, sess := range authSessions {
		out = append(out, AdminSession{
			ID:        sess.ID,
			UserID:    sess.UserID,
			DeviceID:  sess.DeviceID,
			IPAddress: sess.IPAddress,
			UserAgent: sess.UserAgent,
			ExpiresAt: sess.ExpiresAt,
			CreatedAt: sess.CreatedAt,
			// is_current = same JWT (matches by jti) AND same user; the user
			// guard prevents an admin from seeing "current" on a target whose
			// session id collides (in practice impossible, since session.id is
			// a UUID — but cheap to enforce).
			IsCurrent: p.ActorSessionID != "" &&
				sess.ID == p.ActorSessionID &&
				sess.UserID == p.ActorID.String(),
		})
	}
	return out, nil
}

// RevokeSessionParams collects the revoke-call inputs. ActorSessionID is the
// caller's jti — used by the own-current-session guard to prevent admins
// from revoking the JWT they're using right now (instant lockout on the next
// API call). IP/UserAgent feed audit details.
type RevokeSessionParams struct {
	ActorID        uuid.UUID
	ActorRole      string
	ActorSessionID string
	SessionID      string
	IP             string
	UserAgent      string
}

// RevokeSession revokes a single session. Order matters and is fail-closed:
//
//  1. permissions gate (SysManageUsers)
//  2. resolve session via auth GET /internal/sessions/:id (need user_id for
//     CanModifyUser + own-current check + audit details)
//  3. own-current guard — the actor cannot revoke the JWT they're currently
//     using (would 401 the very next call); they must use Settings → Logout
//  4. role hierarchy guard (CanModifyUser)
//  5. audit row written FIRST (any failure aborts before mutation)
//  6. DELETE via auth /internal/sessions/:id
//  7. NATS publish orbit.session.<id>.revoked — best-effort, the DB delete
//     alone is authoritative because ValidateAccessToken loads sessions by
//     jti on every request. NATS just collapses the close-window to <1s.
func (s *SessionAdminService) RevokeSession(ctx context.Context, p RevokeSessionParams) error {
	if !permissions.HasSysPermission(p.ActorRole, permissions.SysManageUsers) {
		return apperror.Forbidden("Insufficient permissions")
	}
	sessionID, err := uuid.Parse(p.SessionID)
	if err != nil {
		return apperror.BadRequest("Invalid session ID")
	}

	sess, err := s.fetchSession(ctx, sessionID)
	if err != nil {
		return err
	}
	// fetchSession returns a NotFound apperror on 404 already.

	// Guard: cannot revoke own current session. Compare to ActorSessionID
	// (the actor's jti from X-User-Session-ID); ActorID matches by construction
	// because gateway-injected headers are trusted post-JWT-validation, but we
	// verify both for defence-in-depth.
	if p.ActorSessionID != "" &&
		sess.ID == p.ActorSessionID &&
		sess.UserID == p.ActorID.String() {
		return apperror.BadRequest("Cannot revoke your own current session — use logout instead")
	}

	targetID, err := uuid.Parse(sess.UserID)
	if err != nil {
		return apperror.Internal("auth returned malformed session user_id")
	}

	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		// Stale session pointing at deleted user — let the revoke proceed; no
		// principal to consult for role hierarchy.
		target = &model.User{ID: targetID, Role: "member"}
	}

	if !permissions.CanModifyUser(p.ActorRole, target.Role) {
		return apperror.Forbidden("Cannot revoke a session of a user with equal or higher role")
	}

	// Audit FIRST. target_type "user" matches the existing convention for
	// admin actions on a person; the revoked session id lands in details so
	// queries like "all session revokes for user X" still resolve.
	auditDetails := map[string]interface{}{
		"session_id": sess.ID,
	}
	if sess.IPAddress != nil && *sess.IPAddress != "" {
		auditDetails["session_ip"] = *sess.IPAddress
	}
	if sess.UserAgent != nil && *sess.UserAgent != "" {
		// Bound user-agent length to keep audit_log details compact. 200 chars
		// is enough to identify a browser/OS without storing the entire UA
		// string (some Chromium UA strings exceed 500 chars).
		auditDetails["session_user_agent"] = truncate(*sess.UserAgent, 200)
	}
	if err := s.writeAudit(ctx, p.ActorID, model.AuditUserSessionRevoke, "user", targetID.String(),
		auditDetails, p.IP, p.UserAgent); err != nil {
		return apperror.Internal("audit log write failed")
	}

	if err := s.deleteSession(ctx, sessionID); err != nil {
		return err
	}

	// Best-effort WS close fanout. Failure here only widens the close window
	// from <1s to ~60s (next token revalidation tick). Don't fail the call.
	if s.nats != nil {
		s.nats.Publish(
			fmt.Sprintf("orbit.session.%s.revoked", sess.ID),
			// String literal matches gateway's ws.EventSessionRevoked. Kept
			// in sync by code review — messaging does not import the gateway
			// ws package (cross-service dep would be cyclic).
			"session_revoked",
			map[string]string{
				"session_id": sess.ID,
				"user_id":    sess.UserID,
			},
			nil, p.ActorID.String(),
		)
	}

	return nil
}

// fetchSessions calls auth GET /internal/users/:id/sessions.
func (s *SessionAdminService) fetchSessions(ctx context.Context, userID uuid.UUID) ([]authSession, error) {
	if s.authURL == "" {
		return nil, apperror.ServiceUnavailable("Sessions admin not configured")
	}
	url := fmt.Sprintf("%s/internal/users/%s/sessions", s.authURL, userID)

	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := s.doRequest(callCtx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, mapAuthError(resp.StatusCode, "list sessions")
	}
	var payload struct {
		Sessions []authSession `json:"sessions"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode auth list sessions: %w", err)
	}
	return payload.Sessions, nil
}

// fetchSession calls auth GET /internal/sessions/:id. Returns NotFound on 404.
func (s *SessionAdminService) fetchSession(ctx context.Context, sessionID uuid.UUID) (*authSession, error) {
	if s.authURL == "" {
		return nil, apperror.ServiceUnavailable("Sessions admin not configured")
	}
	url := fmt.Sprintf("%s/internal/sessions/%s", s.authURL, sessionID)

	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := s.doRequest(callCtx, http.MethodGet, url)
	if err != nil {
		return nil, err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil, apperror.NotFound("Session not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, mapAuthError(resp.StatusCode, "get session")
	}
	var sess authSession
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&sess); err != nil {
		return nil, fmt.Errorf("decode auth get session: %w", err)
	}
	return &sess, nil
}

// deleteSession calls auth DELETE /internal/sessions/:id.
func (s *SessionAdminService) deleteSession(ctx context.Context, sessionID uuid.UUID) error {
	if s.authURL == "" {
		return apperror.ServiceUnavailable("Sessions admin not configured")
	}
	url := fmt.Sprintf("%s/internal/sessions/%s", s.authURL, sessionID)

	callCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := s.doRequest(callCtx, http.MethodDelete, url)
	if err != nil {
		return err
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		// Race: someone else (the user themselves logging out, the cleanup
		// job, another admin) deleted the row between our fetch and delete.
		// Audit is already written so we report success — the operator's
		// intent matched the resulting state.
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return mapAuthError(resp.StatusCode, "delete session")
	}
	return nil
}

// doRequest builds and dispatches an internal-token-signed request. Each
// caller manages its own context lifecycle — the timeout context lives in
// the caller's scope so cancel() fires AFTER the body is fully drained,
// not while the caller is still reading it.
func (s *SessionAdminService) doRequest(ctx context.Context, method, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create auth request: %w", err)
	}
	req.Header.Set("X-Internal-Token", s.internalSecret)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, apperror.GatewayTimeout("Auth service timed out")
		}
		s.logger.Warn("admin sessions auth call failed", "error", err, "url", url, "method", method)
		return nil, apperror.BadGateway("Auth service unreachable")
	}
	return resp, nil
}

func mapAuthError(status int, op string) error {
	switch status {
	case http.StatusForbidden:
		return apperror.Internal(fmt.Sprintf("auth rejected internal token on %s", op))
	case http.StatusBadRequest:
		return apperror.BadRequest("Invalid request to auth service")
	case http.StatusServiceUnavailable:
		return apperror.ServiceUnavailable("Auth service unavailable")
	default:
		return apperror.BadGateway(fmt.Sprintf("Auth returned status %d on %s", status, op))
	}
}

func drainAndClose(body io.ReadCloser) {
	io.Copy(io.Discard, io.LimitReader(body, 4096))
	_ = body.Close()
}

func (s *SessionAdminService) writeAudit(ctx context.Context, actorID uuid.UUID, action, targetType, targetID string, details map[string]interface{}, ip, ua string) error {
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
