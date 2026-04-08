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
}

type AuthService struct {
	users    store.UserStore
	sessions store.SessionStore
	invites  store.InviteStore
	redis    *redis.Client
	cfg      *Config
}

func NewAuthService(users store.UserStore, sessions store.SessionStore, invites store.InviteStore, rdb *redis.Client, cfg *Config) *AuthService {
	return &AuthService{users: users, sessions: sessions, invites: invites, redis: rdb, cfg: cfg}
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
	}

	pair, err := s.createTokenPair(ctx, u.ID, ip, userAgent)
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

	return u, nil
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

	pair, err := s.createTokenPair(ctx, u.ID, ip, userAgent)
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
		return apperror.Forbidden("Invalid reset key")
	}

	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	if u == nil || u.Role != "admin" {
		return apperror.NotFound("Admin not found")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), 12)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := s.users.UpdatePassword(ctx, u.ID, string(hash)); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

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

	return s.users.UpdateTOTP(ctx, userID, u.TOTPSecret, true)
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

	return s.users.UpdateTOTP(ctx, userID, nil, false)
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

func (s *AuthService) createTokenPair(ctx context.Context, userID uuid.UUID, ip, userAgent string) (*TokenPair, error) {
	// Create access token
	now := time.Now()
	accessClaims := jwt.MapClaims{
		"sub":  userID.String(),
		"role": "member",
		"iat":  now.Unix(),
		"exp":  now.Add(s.cfg.AccessTTL).Unix(),
	}
	// Fetch role for the token — fail if DB is unavailable or user deleted
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("fetch user role: %w", err)
	}
	if u == nil {
		return nil, apperror.Unauthorized("User not found")
	}
	accessClaims["role"] = u.Role

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString([]byte(s.cfg.JWTSecret))
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	// Create refresh token (random string)
	refreshBytes := make([]byte, 32)
	if _, err := rand.Read(refreshBytes); err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshStr := hex.EncodeToString(refreshBytes)

	// Store session in DB
	sess := &model.Session{
		UserID:    userID,
		TokenHash: hashToken(refreshStr),
		IPAddress: &ip,
		UserAgent: &userAgent,
		ExpiresAt: now.Add(s.cfg.RefreshTTL),
	}
	if err := s.sessions.Create(ctx, sess); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
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
