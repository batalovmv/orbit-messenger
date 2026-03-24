package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
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
func (s *AuthService) Bootstrap(ctx context.Context, email, password, displayName string) (*model.User, error) {
	count, err := s.users.CountAdmins(ctx)
	if err != nil {
		return nil, fmt.Errorf("count admins: %w", err)
	}
	if count > 0 {
		return nil, apperror.Forbidden("Admin account already exists")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	u := &model.User{
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         "admin",
	}
	if err := s.users.Create(ctx, u); err != nil {
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
		return nil, nil, apperror.Unauthorized("Invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, nil, apperror.Unauthorized("Invalid email or password")
	}

	if u.TOTPEnabled {
		if totpCode == "" {
			return nil, nil, apperror.BadRequest("2FA code is required")
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

	existing, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("check existing: %w", err)
	}
	if existing != nil {
		return nil, apperror.Conflict("Email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	u := &model.User{
		Email:        email,
		PasswordHash: string(hash),
		DisplayName:  displayName,
		Role:         inv.Role,
		InvitedBy:    inv.CreatedBy,
		InviteCode:   &code,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	if err := s.invites.UseInvite(ctx, code, u.ID); err != nil {
		slog.Error("failed to mark invite as used", "error", err, "code", code)
	}

	return u, nil
}

// Logout invalidates the current access token.
func (s *AuthService) Logout(ctx context.Context, tokenStr string) error {
	claims, err := s.parseToken(tokenStr)
	if err != nil {
		return apperror.Unauthorized("Invalid token")
	}

	exp, _ := claims.GetExpirationTime()
	if exp != nil {
		ttl := time.Until(exp.Time)
		if ttl > 0 {
			hash := hashToken(tokenStr)
			s.redis.Set(ctx, "jwt_blacklist:"+hash, "1", ttl)
		}
	}
	return nil
}

// Refresh rotates the refresh token atomically.
func (s *AuthService) Refresh(ctx context.Context, refreshToken, ip, userAgent string) (*TokenPair, *model.User, error) {
	hash := hashToken(refreshToken)

	sess, err := s.sessions.GetByTokenHash(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("get session: %w", err)
	}
	if sess == nil {
		return nil, nil, apperror.Unauthorized("Invalid refresh token")
	}
	if sess.ExpiresAt.Before(time.Now()) {
		return nil, nil, apperror.Unauthorized("Refresh token expired")
	}

	// Delete old session (atomic rotation)
	if err := s.sessions.DeleteByTokenHash(ctx, hash); err != nil {
		return nil, nil, fmt.Errorf("delete old session: %w", err)
	}

	u, err := s.users.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	if u == nil {
		return nil, nil, apperror.Unauthorized("User not found")
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
	if s.cfg.AdminResetKey == "" || resetKey != s.cfg.AdminResetKey {
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

	// Revoke all sessions
	if err := s.sessions.DeleteAllByUser(ctx, u.ID); err != nil {
		slog.Error("failed to delete sessions on admin reset", "error", err)
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
		return apperror.NotFound("Session not found")
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
		return apperror.NotFound("Invite not found or not owned by you")
	}
	return nil
}

// ValidateAccessToken validates a JWT access token and returns the user ID.
func (s *AuthService) ValidateAccessToken(ctx context.Context, tokenStr string) (uuid.UUID, string, error) {
	hash := hashToken(tokenStr)
	blacklisted, _ := s.redis.Exists(ctx, "jwt_blacklist:"+hash).Result()
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
	// Fetch role for the token
	u, err := s.users.GetByID(ctx, userID)
	if err == nil && u != nil {
		accessClaims["role"] = u.Role
	}

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

func generateInviteCode() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
