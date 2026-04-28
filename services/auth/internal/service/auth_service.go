// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

type Config struct {
	JWTSecret     string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	TOTPIssuer    string
	AdminResetKey string
	FrontendURL   string

	// Welcome flow (mig 069). Optional. When both are set, Register POSTs to
	// `${MessagingURL}/internal/users/{id}/join-default-chats` with
	// `X-Internal-Token: ${InternalSecret}` after a successful user create
	// so the new user is auto-added to chats marked is_default_for_new_users.
	// Best-effort: a failure here is logged but does not roll back the user.
	MessagingURL   string
	InternalSecret string
}

// HTTPClient is the minimal http.Client interface AuthService uses to talk
// to messaging. Defined here so tests can inject a mock without depending
// on a real network.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type AuthService struct {
	users      store.UserStore
	sessions   store.SessionStore
	invites    store.InviteStore
	redis      *redis.Client
	cfg        *Config
	logger     *slog.Logger
	httpClient HTTPClient
}

func NewAuthService(users store.UserStore, sessions store.SessionStore, invites store.InviteStore, rdb *redis.Client, cfg *Config, logger *slog.Logger) *AuthService {
	return &AuthService{
		users: users, sessions: sessions, invites: invites,
		redis: rdb, cfg: cfg, logger: logger,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// WithHTTPClient overrides the default http.Client. Used by tests to
// avoid hitting the network.
func (s *AuthService) WithHTTPClient(c HTTPClient) *AuthService {
	s.httpClient = c
	return s
}

// Bootstrap creates the first admin account. Fails if any admin already exists.
// Uses CreateIfNoAdmins for atomic check-and-insert to prevent race conditions.
func (s *AuthService) Bootstrap(ctx context.Context, email, password, displayName string) (*model.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	u := &model.User{
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         "superadmin",
	}
	if err := s.users.CreateIfNoAdmins(ctx, u); err != nil {
		if errors.Is(err, store.ErrAdminExists) {
			return nil, apperror.Forbidden("Admin account already exists")
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"-"`
}

// Login authenticates a user and returns a token pair.
func (s *AuthService) Login(ctx context.Context, email, password, totpCode, ip, userAgent string) (*TokenPair, *model.User, error) {
	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		// Dummy bcrypt to prevent timing-based user enumeration
		bcrypt.CompareHashAndPassword([]byte("$2a$12$000000000000000000000uGINKk0mFSfiitMnVEs0oOeswgyXIwHi"), []byte(password))
		return nil, nil, apperror.Unauthorized("Invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil, apperror.Unauthorized("Invalid email or password")
	}

	if !u.IsActive {
		return nil, nil, apperror.Forbidden("Account is deactivated")
	}

	if u.TOTPEnabled {
		if totpCode == "" {
			return nil, nil, &apperror.AppError{Code: "2fa_required", Message: "2FA code is required", Status: 400}
		}
		if u.TOTPSecret == nil || !totp.Validate(totpCode, *u.TOTPSecret) {
			return nil, nil, apperror.Unauthorized("Invalid 2FA code")
		}
		// Prevent replay: mark code as used for 90s (covers ±1 TOTP window)
		totpUsedKey := fmt.Sprintf("totp_used:%s:%s", u.ID.String(), totpCode)
		set, redisErr := s.redis.SetNX(ctx, totpUsedKey, "1", 90*time.Second).Result()
		if redisErr != nil || !set {
			return nil, nil, apperror.Unauthorized("Invalid 2FA code")
		}
	}

	pair, err := s.createTokenPair(ctx, u.ID, nil, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}
	return pair, u, nil
}

// Register creates a new user account using an invite code.
// Atomically claims the invite slot first, then creates the user.
func (s *AuthService) Register(ctx context.Context, code, email, password, displayName string) (*model.User, error) {
	inv, err := s.invites.GetByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("get invite: %w", err)
	}
	if inv == nil || !inv.IsActive || inv.UseCount >= inv.MaxUses {
		return nil, apperror.BadRequest("Invalid or expired invite code")
	}
	if inv.ExpiresAt != nil && inv.ExpiresAt.Before(time.Now()) {
		return nil, apperror.BadRequest("Invite code has expired")
	}
	if inv.Email != nil && *inv.Email != email {
		return nil, apperror.BadRequest("This invite is locked to a different email")
	}

	// Atomically claim the invite slot BEFORE creating the user.
	// UseInvite uses WHERE use_count < max_uses — if two requests race,
	// only one will succeed; the other gets ErrNoRows.
	// Returns the authoritative role from the locked row (not the stale snapshot).
	atomicRole, err := s.invites.UseInvite(ctx, code, uuid.Nil, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.BadRequest("Invalid or expired invite code")
		}
		return nil, fmt.Errorf("use invite: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	u := &model.User{
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         atomicRole,
		InvitedBy:    inv.CreatedBy,
		InviteCode:   &code,
	}
	if err := s.users.Create(ctx, u); err != nil {
		// Best-effort: roll back the invite usage on user creation failure
		if rbErr := s.invites.RollbackUsage(ctx, code); rbErr != nil {
			slog.Error("failed to rollback invite usage", "error", rbErr, "code", code)
		}
		// Map DB unique constraint violation to 409 Conflict instead of 500
		if isUniqueViolation(err) {
			return nil, apperror.Conflict("Email already registered")
		}
		return nil, fmt.Errorf("create user: %w", err)
	}

	// Update invite used_by with the real user ID (was uuid.Nil during claim)
	if ubErr := s.invites.UpdateUsedBy(ctx, code, u.ID); ubErr != nil {
		slog.Error("failed to update invite used_by", "error", ubErr, "code", code, "user_id", u.ID)
	}

	// Welcome flow (mig 069). Best-effort — a failure here does not roll back
	// the user. The admin "Backfill default chats" action is the safety net
	// for users that miss this hop (messaging unreachable, etc.).
	s.joinDefaultChatsBestEffort(ctx, u.ID)

	return u, nil
}

// joinDefaultChatsBestEffort calls the messaging service to add the user to
// every chat marked is_default_for_new_users. One retry on transport errors
// (100 ms backoff) — beyond that we give up and log so an operator can run
// the admin Backfill button later. Returns no error: registration must not
// fail because of this side-channel.
func (s *AuthService) joinDefaultChatsBestEffort(ctx context.Context, userID uuid.UUID) {
	if s.cfg == nil || s.cfg.MessagingURL == "" || s.cfg.InternalSecret == "" {
		// Welcome flow disabled — typically in unit tests or in a deployment
		// that has not finished migrating to the new shape. Stay silent.
		return
	}
	url := strings.TrimRight(s.cfg.MessagingURL, "/") +
		"/internal/users/" + userID.String() + "/join-default-chats"

	doOnce := func(attempt int) (status int, err error) {
		// Independent context — Register's caller may cancel ctx as soon as it
		// has the response, but we still want this side-channel to land.
		callCtx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, nil)
		if err != nil {
			return 0, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("X-Internal-Token", s.cfg.InternalSecret)
		req.Header.Set("X-User-ID", userID.String())
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return 0, fmt.Errorf("attempt %d: %w", attempt, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 == 2 {
			return resp.StatusCode, nil
		}
		return resp.StatusCode, fmt.Errorf("attempt %d: unexpected status %d", attempt, resp.StatusCode)
	}

	if status, err := doOnce(1); err == nil {
		slog.InfoContext(ctx, "welcome-flow: joined user to default chats",
			"user_id", userID, "status", status)
		return
	}

	time.Sleep(100 * time.Millisecond)

	if status, err := doOnce(2); err != nil {
		slog.ErrorContext(ctx, "welcome-flow: messaging /internal/.../join-default-chats failed twice; admin backfill required",
			"user_id", userID, "error", err, "last_status", status)
	} else {
		slog.InfoContext(ctx, "welcome-flow: joined user to default chats on retry",
			"user_id", userID, "status", status)
	}
}

// Logout invalidates the current access token and deletes the refresh session.
func (s *AuthService) Logout(ctx context.Context, tokenStr, refreshToken string) error {
	claims, err := s.parseToken(tokenStr)
	if err != nil {
		return apperror.Unauthorized("Invalid token")
	}

	exp, _ := claims.GetExpirationTime()
	// Fail-closed: always blacklist the token. If exp is missing or already past,
	// use a minimum TTL of 1 second so the Redis key is written (and immediately expires).
	ttl := time.Second
	if exp != nil {
		remaining := time.Until(exp.Time)
		if remaining > ttl {
			ttl = remaining
		}
	}
	hash := hashToken(tokenStr)
	if err := s.redis.Set(ctx, "jwt_blacklist:"+hash, "1", ttl).Err(); err != nil {
		slog.Error("failed to blacklist token in Redis", "error", err)
		return fmt.Errorf("failed to blacklist token: %w", err)
	}

	// Delete the refresh session from DB so the token cannot be reused.
	if refreshToken == "" {
		slog.Warn("logout called without refresh token, access blacklisted but refresh session not revoked", "token_hash", hash)
		return apperror.BadRequest("Refresh token is required for complete logout")
	} else {
		refreshHash := hashToken(refreshToken)
		if err := s.sessions.DeleteByTokenHash(ctx, refreshHash); err != nil {
			slog.Error("failed to delete refresh session on logout", "error", err)
			return fmt.Errorf("failed to delete refresh session: %w", err)
		}
	}

	return nil
}

// Refresh rotates the refresh token atomically using DELETE ... RETURNING.
func (s *AuthService) Refresh(ctx context.Context, refreshToken, ip, userAgent string) (*TokenPair, *model.User, error) {
	hash := hashToken(refreshToken)

	// Atomic: delete and return in one query — prevents replay attacks
	sess, err := s.sessions.DeleteAndReturnByTokenHash(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("delete session: %w", err)
	}
	if sess == nil {
		return nil, nil, apperror.Unauthorized("Invalid refresh token")
	}
	if sess.ExpiresAt.Before(time.Now()) {
		return nil, nil, apperror.Unauthorized("Refresh token expired")
	}

	u, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, nil, apperror.Unauthorized("User not found")
	}
	if !u.IsActive {
		return nil, nil, apperror.Forbidden("Account is deactivated")
	}

	pair, err := s.createTokenPair(ctx, u.ID, sess.DeviceID, ip, userAgent)
	if err != nil {
		return nil, nil, err
	}
	return pair, u, nil
}

// GetMe returns the user for a valid access token.
func (s *AuthService) GetMe(ctx context.Context, userID uuid.UUID) (*model.User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, apperror.NotFound("User not found")
	}
	return u, nil
}

// ResetAdmin resets an admin password using the admin reset key.
func (s *AuthService) ResetAdmin(ctx context.Context, resetKey, email, newPassword string) error {
	if s.cfg.AdminResetKey == "" || subtle.ConstantTimeCompare([]byte(resetKey), []byte(s.cfg.AdminResetKey)) != 1 {
		s.logger.Warn("reset_admin: invalid key attempted", "email", email)
		return apperror.Forbidden("Invalid reset key")
	}

	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u == nil || (u.Role != "admin" && u.Role != "superadmin") {
		s.logger.Warn("reset_admin: target not admin", "email", email)
		return apperror.NotFound("Admin not found")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := s.users.UpdatePassword(ctx, u.ID, string(hash)); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	s.logger.Info("reset_admin: password reset successful", "user_id", u.ID, "email", email, "role", u.Role)

	// Revoke all sessions (fail-closed: password reset must invalidate all tokens)
	if err := s.sessions.DeleteAllByUser(ctx, u.ID); err != nil {
		slog.Error("failed to delete sessions on admin reset", "error", err, "user_id", u.ID)
		return fmt.Errorf("revoke sessions: %w", err)
	}

	// Invalidate all existing access tokens by setting a per-user "invalid before" timestamp.
	// ValidateAccessToken checks this and rejects tokens issued before this moment.
	// TTL = AccessTTL so the key auto-expires when all old tokens are naturally expired.
	invalidateKey := "user_tokens_invalid_before:" + u.ID.String()
	if err := s.redis.Set(ctx, invalidateKey, fmt.Sprintf("%d", time.Now().Unix()), s.cfg.AccessTTL).Err(); err != nil {
		slog.Error("failed to set token invalidation timestamp", "error", err, "user_id", u.ID)
		return fmt.Errorf("invalidate access tokens: %w", err)
	}

	return nil
}

// ListSessions returns all active sessions for a user.
func (s *AuthService) ListSessions(ctx context.Context, userID uuid.UUID) ([]model.Session, error) {
	return s.sessions.ListByUser(ctx, userID)
}

// RevokeSession deletes a session (with ownership check).
func (s *AuthService) RevokeSession(ctx context.Context, sessionID, userID uuid.UUID) error {
	err := s.sessions.DeleteByID(ctx, sessionID, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NotFound("Session not found")
		}
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// Setup2FA generates a TOTP secret and returns the provisioning URI.
func (s *AuthService) Setup2FA(ctx context.Context, userID uuid.UUID) (string, string, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", "", fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return "", "", apperror.NotFound("User not found")
	}
	if u.TOTPEnabled {
		return "", "", apperror.Conflict("2FA is already enabled")
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.cfg.TOTPIssuer,
		AccountName: u.Email,
	})
	if err != nil {
		return "", "", fmt.Errorf("generate totp: %w", err)
	}

	secret := key.Secret()
	if err := s.users.UpdateTOTP(ctx, userID, &secret, false); err != nil {
		return "", "", fmt.Errorf("save totp secret: %w", err)
	}

	return secret, key.URL(), nil
}

// Verify2FA confirms the TOTP code and enables 2FA.
func (s *AuthService) Verify2FA(ctx context.Context, userID uuid.UUID, code string) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return apperror.NotFound("User not found")
	}
	if u.TOTPEnabled {
		return apperror.Conflict("2FA is already enabled")
	}
	if u.TOTPSecret == nil {
		return apperror.BadRequest("Run 2FA setup first")
	}

	if !totp.Validate(code, *u.TOTPSecret) {
		return apperror.BadRequest("Invalid 2FA code")
	}
	// Prevent replay during 2FA setup verification
	totpUsedKey := fmt.Sprintf("totp_used:%s:%s", userID.String(), code)
	set, redisErr := s.redis.SetNX(ctx, totpUsedKey, "1", 90*time.Second).Result()
	if redisErr != nil || !set {
		return apperror.BadRequest("Invalid 2FA code")
	}

	// Atomically enable TOTP and revoke all sessions in one DB transaction.
	// This closes the TOCTOU window where a concurrent Login could create a new
	// session between revocation and UpdateTOTP. If the transaction fails, neither
	// change is persisted — the account stays in the safe pre-2FA state.
	if err := s.users.EnableTOTPAndRevokeSessions(ctx, userID, *u.TOTPSecret); err != nil {
		return fmt.Errorf("enable 2fa: %w", err)
	}

	// Invalidate all existing access tokens via per-user "invalid before" timestamp.
	// Done after the DB transaction — if this fails, sessions are already revoked and
	// TOTP is enabled, so the account is secure. Access tokens will expire naturally.
	invalidateKey := "user_tokens_invalid_before:" + userID.String()
	if err := s.redis.Set(ctx, invalidateKey, fmt.Sprintf("%d", time.Now().Unix()), s.cfg.AccessTTL).Err(); err != nil {
		slog.Error("failed to set token invalidation timestamp on 2FA enable", "error", err, "user_id", userID)
		// Non-fatal: sessions are revoked and TOTP is enabled. Access tokens expire naturally (AccessTTL).
	}

	return nil
}

// Disable2FA disables 2FA after password confirmation.
func (s *AuthService) Disable2FA(ctx context.Context, userID uuid.UUID, password string) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return apperror.NotFound("User not found")
	}
	if !u.TOTPEnabled {
		return apperror.BadRequest("2FA is not enabled")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return apperror.Unauthorized("Invalid password")
	}

	if err := s.users.UpdateTOTP(ctx, userID, nil, false); err != nil {
		return fmt.Errorf("disable totp: %w", err)
	}

	// Revoke all sessions so any attacker who had access loses it immediately.
	if err := s.sessions.DeleteAllByUser(ctx, userID); err != nil {
		s.logger.Error("failed to revoke sessions on 2fa disable", "error", err, "user_id", userID)
		// Non-fatal: 2FA is disabled. Sessions expire naturally via AccessTTL.
	}

	return nil
}

// ValidateInvite checks if an invite code is valid.
func (s *AuthService) ValidateInvite(ctx context.Context, code string) (*model.Invite, error) {
	inv, err := s.invites.GetByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("get invite: %w", err)
	}
	if inv == nil || !inv.IsActive || inv.UseCount >= inv.MaxUses {
		return nil, apperror.NotFound("Invalid or expired invite code")
	}
	if inv.ExpiresAt != nil && inv.ExpiresAt.Before(time.Now()) {
		return nil, apperror.NotFound("Invite code has expired")
	}
	return inv, nil
}

// CreateInvite creates a new invite code (admin only).
func (s *AuthService) CreateInvite(ctx context.Context, createdBy uuid.UUID, email *string, role string, maxUses int, expiresAt *time.Time) (*model.Invite, error) {
	code, err := generateInviteCode()
	if err != nil {
		return nil, fmt.Errorf("generate code: %w", err)
	}

	inv := &model.Invite{
		Code:      code,
		CreatedBy: &createdBy,
		Email:     email,
		Role:      role,
		MaxUses:   maxUses,
		ExpiresAt: expiresAt,
	}
	if err := s.invites.Create(ctx, inv); err != nil {
		return nil, fmt.Errorf("create invite: %w", err)
	}
	return inv, nil
}

// ListInvites returns all invites (admin only).
func (s *AuthService) ListInvites(ctx context.Context) ([]model.Invite, error) {
	return s.invites.ListAll(ctx)
}

// RevokeInvite deactivates an invite (admin only, with created_by check).
func (s *AuthService) RevokeInvite(ctx context.Context, inviteID, createdBy uuid.UUID) error {
	err := s.invites.Revoke(ctx, inviteID, createdBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperror.NotFound("Invite not found or not owned by you")
		}
		return fmt.Errorf("revoke invite: %w", err)
	}
	return nil
}

// ValidateAccessToken validates a JWT access token and returns the user ID.
func (s *AuthService) ValidateAccessToken(ctx context.Context, tokenStr string) (uuid.UUID, string, error) {
	hash := hashToken(tokenStr)
	blacklisted, err := s.redis.Exists(ctx, "jwt_blacklist:"+hash).Result()
	if err != nil {
		// Fail closed: if Redis is down, reject tokens to prevent revoked tokens from being accepted.
		slog.Error("redis blacklist check failed, rejecting token", "error", err)
		return uuid.Nil, "", apperror.Internal("Token validation temporarily unavailable")
	}
	if blacklisted > 0 {
		return uuid.Nil, "", apperror.Unauthorized("Token has been revoked")
	}

	claims, err := s.parseToken(tokenStr)
	if err != nil {
		return uuid.Nil, "", apperror.Unauthorized("Invalid token")
	}

	sub, _ := claims.GetSubject()
	userID, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, "", apperror.Unauthorized("Invalid token subject")
	}

	jti, _ := claims["jti"].(string)
	if jti == "" {
		return uuid.Nil, "", apperror.Unauthorized("Invalid token session")
	}

	sessionID, err := uuid.Parse(jti)
	if err != nil {
		return uuid.Nil, "", apperror.Unauthorized("Invalid token session")
	}

	// Per-jti blacklist (Day 5.2 admin session revoke). Written BEFORE the
	// DB delete in InternalRevokeSession so direct hits to /auth/me (which
	// bypass the gateway middleware that ALSO checks this key) cannot
	// validate during the small window between the blacklist write and the
	// row delete. Fail-closed on Redis error.
	jtiBlacklisted, err := s.redis.Exists(ctx, "jwt_blacklist:jti:"+sessionID.String()).Result()
	if err != nil {
		slog.Error("redis jti blacklist check failed, rejecting token", "error", err)
		return uuid.Nil, "", apperror.Internal("Token validation temporarily unavailable")
	}
	if jtiBlacklisted > 0 {
		return uuid.Nil, "", apperror.Unauthorized("Session revoked")
	}

	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("get session: %w", err)
	}
	if sess == nil || sess.UserID != userID {
		return uuid.Nil, "", apperror.Unauthorized("Session revoked")
	}

	// Check per-user invalidation timestamp (set by ResetAdmin to revoke all access tokens).
	// Fail-closed: Redis error = reject token.
	invalidateKey := "user_tokens_invalid_before:" + userID.String()
	invalidateTS, err := s.redis.Get(ctx, invalidateKey).Result()
	if err != nil && err != redis.Nil {
		slog.Error("redis user token invalidation check failed, rejecting token", "error", err)
		return uuid.Nil, "", apperror.Internal("Token validation temporarily unavailable")
	}
	if err == nil {
		var threshold int64
		if _, scanErr := fmt.Sscanf(invalidateTS, "%d", &threshold); scanErr == nil {
			iat, _ := claims["iat"].(float64)
			if int64(iat) <= threshold {
				return uuid.Nil, "", apperror.Unauthorized("Token has been revoked")
			}
		}
	}

	role, _ := claims["role"].(string)
	return userID, role, nil
}

// --- internal helpers ---

func (s *AuthService) createTokenPair(ctx context.Context, userID uuid.UUID, deviceID *uuid.UUID, ip, userAgent string) (*TokenPair, error) {
	now := time.Now()
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("fetch user role: %w", err)
	}
	if u == nil {
		return nil, apperror.Unauthorized("User not found")
	}

	// Create refresh token (random string)
	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshStr := hex.EncodeToString(refreshBytes)

	// Store session in DB — device_id stays NULL when no device is registered.
	// IP/UA may be empty when the request omits headers or the gateway doesn't
	// forward them; pass NULL instead of "" so the inet column can store it.
	var ipPtr, uaPtr *string
	if ip != "" {
		ipPtr = &ip
	}
	if userAgent != "" {
		uaPtr = &userAgent
	}
	sess := &model.Session{
		UserID:    userID,
		DeviceID:  deviceID,
		TokenHash: hashToken(refreshStr),
		IPAddress: ipPtr,
		UserAgent: uaPtr,
		ExpiresAt: now.Add(s.cfg.RefreshTTL),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	accessClaims := jwt.MapClaims{
		"sub":  userID.String(),
		"role": u.Role,
		"iat":  now.Unix(),
		"exp":  now.Add(s.cfg.AccessTTL).Unix(),
		"jti":  sess.ID.String(),
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		ExpiresIn:    int(s.cfg.AccessTTL.Seconds()),
		RefreshToken: refreshStr,
	}, nil
}

func (s *AuthService) parseToken(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// isUniqueViolation checks if the error is a PostgreSQL unique constraint violation (23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func generateInviteCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// InternalListUserSessions returns all sessions for a user. Internal-only: the
// caller (messaging) has already enforced SysManageUsers + audit. No role gate
// here.
func (s *AuthService) InternalListUserSessions(ctx context.Context, targetID uuid.UUID) ([]model.Session, error) {
	return s.sessions.ListByUser(ctx, targetID)
}

// InternalGetSession returns a single session row, or (nil, nil) when missing.
// Used by messaging to resolve target user_id before applying guards/audit.
func (s *AuthService) InternalGetSession(ctx context.Context, sessionID uuid.UUID) (*model.Session, error) {
	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

// InternalRevokeSession deletes one session row by id and writes a per-jti
// Redis blacklist entry so the gateway JWT cache (30s TTL) cannot keep the
// access token alive between revoke and cache expiry.
//
// Why per-jti and not per-user-blacklist:
//   - per-user kills the user's OTHER devices too, contradicting the "single
//     device revoke" UX (the whole point of this endpoint).
//   - per-jti is the exact granularity of the JWT being revoked.
//
// Why blacklist at all if the DB delete is authoritative:
//   - ValidateAccessToken does call sessions.GetByID(jti) on every request,
//     so HTTP requests that reach auth are correctly rejected.
//   - BUT gateway middleware caches the auth /me result in Redis with TTL =
//     30s. On a cache hit, gateway never calls auth — meaning a revoked
//     session can keep authenticating for up to 30s after delete.
//   - Same shape on the WS upgrade cache path.
//   - jti blacklist closes that window: gateway middleware checks
//     jwt_blacklist:jti:<sid> in addition to its existing per-token-hash
//     blacklist, on every request including cache hits.
//
// TTL is bounded by remaining JWT lifetime (sess.ExpiresAt - now), capped at
// AccessTTL — once the token expires nothing can present it anyway.
func (s *AuthService) InternalRevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	sess, err := s.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get session: %w", err)
	}
	if sess == nil {
		return apperror.NotFound("Session not found")
	}

	// Per-jti blacklist FIRST so any concurrent request that reaches the
	// gateway between this point and the DB delete is already rejected.
	ttl := time.Until(sess.ExpiresAt)
	if ttl > s.cfg.AccessTTL {
		ttl = s.cfg.AccessTTL
	}
	if ttl > 0 {
		blacklistKey := "jwt_blacklist:jti:" + sessionID.String()
		if err := s.redis.Set(ctx, blacklistKey, "1", ttl).Err(); err != nil {
			return fmt.Errorf("jti blacklist write: %w", err)
		}
	}

	if err := s.sessions.DeleteByID(ctx, sessionID, sess.UserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, store.ErrNotFound) {
			return apperror.NotFound("Session not found")
		}
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// GetNotificationPriorityMode returns the user's persisted notification priority mode.
func (s *AuthService) GetNotificationPriorityMode(ctx context.Context, userID uuid.UUID) (string, error) {
	mode, err := s.users.GetNotificationPriorityMode(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apperror.NotFound("User not found")
		}
		return "", fmt.Errorf("get notification priority mode: %w", err)
	}
	return mode, nil
}

// UpdateNotificationPriorityMode updates the user's notification priority mode.
func (s *AuthService) UpdateNotificationPriorityMode(ctx context.Context, userID uuid.UUID, mode string) error {
	if err := s.users.UpdateNotificationPriorityMode(ctx, userID, mode); err != nil {
		return fmt.Errorf("update notification priority mode: %w", err)
	}

	if s.redis != nil {
		cacheKey := fmt.Sprintf("user_priority_mode:%s", userID.String())
		if err := s.redis.Set(ctx, cacheKey, mode, 5*time.Minute).Err(); err != nil {
			s.logger.Warn("failed to cache notification priority mode", "user_id", userID, "error", err)
		}
	}

	return nil
}
