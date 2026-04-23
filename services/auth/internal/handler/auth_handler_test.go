package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/service"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

// --- Mock Stores ---

type mockUserStore struct {
	users                    map[uuid.UUID]*model.User
	byEmail                  map[string]*model.User
	adminCount               int
	notificationPriorityMode map[uuid.UUID]string
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{
		users:                    make(map[uuid.UUID]*model.User),
		byEmail:                  make(map[string]*model.User),
		notificationPriorityMode: make(map[uuid.UUID]string),
	}
}

func (m *mockUserStore) Create(_ context.Context, u *model.User) error {
	// Simulate DB unique constraint on email (code 23505)
	if _, exists := m.byEmail[u.Email]; exists {
		return &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	}
	u.ID = uuid.New()
	u.Status = "offline"
	u.IsActive = true
	u.TOTPEnabled = false
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	m.users[u.ID] = u
	m.byEmail[u.Email] = u
	if u.Role == "admin" || u.Role == "superadmin" {
		m.adminCount++
	}
	return nil
}

func (m *mockUserStore) GetByID(_ context.Context, id uuid.UUID) (*model.User, error) {
	return m.users[id], nil
}

func (m *mockUserStore) GetByEmail(_ context.Context, email string) (*model.User, error) {
	return m.byEmail[email], nil
}

func (m *mockUserStore) Update(_ context.Context, u *model.User) error {
	m.users[u.ID] = u
	return nil
}

func (m *mockUserStore) CreateIfNoAdmins(_ context.Context, u *model.User) error {
	if m.adminCount > 0 {
		return store.ErrAdminExists
	}
	u.ID = uuid.New()
	u.Status = "offline"
	u.IsActive = true
	u.TOTPEnabled = false
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	m.users[u.ID] = u
	m.byEmail[u.Email] = u
	m.adminCount++
	return nil
}

func (m *mockUserStore) CountAdmins(_ context.Context) (int, error) {
	return m.adminCount, nil
}

func (m *mockUserStore) UpdatePassword(_ context.Context, id uuid.UUID, hash string) error {
	if u, ok := m.users[id]; ok {
		u.PasswordHash = hash
	}
	return nil
}

func (m *mockUserStore) UpdateTOTP(_ context.Context, id uuid.UUID, secret *string, enabled bool) error {
	if u, ok := m.users[id]; ok {
		u.TOTPSecret = secret
		u.TOTPEnabled = enabled
	}
	return nil
}

func (m *mockUserStore) GetNotificationPriorityMode(_ context.Context, userID uuid.UUID) (string, error) {
	if mode, ok := m.notificationPriorityMode[userID]; ok {
		return mode, nil
	}
	return "all", nil
}

func (m *mockUserStore) UpdateNotificationPriorityMode(_ context.Context, userID uuid.UUID, mode string) error {
	m.notificationPriorityMode[userID] = mode
	if u, ok := m.users[userID]; ok {
		u.NotificationPriorityMode = mode
	}
	return nil
}

type mockSessionStore struct {
	sessions map[uuid.UUID]*model.Session
	byToken  map[string]*model.Session
}

func newMockSessionStore() *mockSessionStore {
	return &mockSessionStore{
		sessions: make(map[uuid.UUID]*model.Session),
		byToken:  make(map[string]*model.Session),
	}
}

func (m *mockSessionStore) Create(_ context.Context, s *model.Session) error {
	s.ID = uuid.New()
	s.CreatedAt = time.Now()
	m.sessions[s.ID] = s
	m.byToken[s.TokenHash] = s
	return nil
}

func (m *mockSessionStore) GetByID(_ context.Context, id uuid.UUID) (*model.Session, error) {
	return m.sessions[id], nil
}

func (m *mockSessionStore) ListByUser(_ context.Context, userID uuid.UUID) ([]model.Session, error) {
	var result []model.Session
	for _, s := range m.sessions {
		if s.UserID == userID {
			result = append(result, *s)
		}
	}
	return result, nil
}

func (m *mockSessionStore) DeleteByID(_ context.Context, id uuid.UUID, userID uuid.UUID) error {
	s, ok := m.sessions[id]
	if !ok || s.UserID != userID {
		return store.ErrNotFound
	}
	delete(m.byToken, s.TokenHash)
	delete(m.sessions, id)
	return nil
}

func (m *mockSessionStore) GetByTokenHash(_ context.Context, hash string) (*model.Session, error) {
	return m.byToken[hash], nil
}

func (m *mockSessionStore) DeleteByTokenHash(_ context.Context, hash string) error {
	if s, ok := m.byToken[hash]; ok {
		delete(m.sessions, s.ID)
		delete(m.byToken, hash)
	}
	return nil
}

func (m *mockSessionStore) DeleteAndReturnByTokenHash(_ context.Context, hash string) (*model.Session, error) {
	s, ok := m.byToken[hash]
	if !ok {
		return nil, nil
	}
	delete(m.sessions, s.ID)
	delete(m.byToken, hash)
	return s, nil
}

func (m *mockSessionStore) DeleteAllByUser(_ context.Context, userID uuid.UUID) error {
	for id, s := range m.sessions {
		if s.UserID == userID {
			delete(m.byToken, s.TokenHash)
			delete(m.sessions, id)
		}
	}
	return nil
}

type mockInviteStore struct {
	invites map[string]*model.Invite
	byID    map[uuid.UUID]*model.Invite
}

func newMockInviteStore() *mockInviteStore {
	return &mockInviteStore{
		invites: make(map[string]*model.Invite),
		byID:    make(map[uuid.UUID]*model.Invite),
	}
}

func (m *mockInviteStore) Create(_ context.Context, inv *model.Invite) error {
	inv.ID = uuid.New()
	inv.IsActive = true
	inv.UseCount = 0
	inv.CreatedAt = time.Now()
	m.invites[inv.Code] = inv
	m.byID[inv.ID] = inv
	return nil
}

func (m *mockInviteStore) GetByCode(_ context.Context, code string) (*model.Invite, error) {
	return m.invites[code], nil
}

func (m *mockInviteStore) GetByID(_ context.Context, id uuid.UUID) (*model.Invite, error) {
	return m.byID[id], nil
}

func (m *mockInviteStore) ListAll(_ context.Context) ([]model.Invite, error) {
	var result []model.Invite
	for _, inv := range m.invites {
		result = append(result, *inv)
	}
	return result, nil
}

func (m *mockInviteStore) UseInvite(_ context.Context, code string, userID uuid.UUID, email string) (string, error) {
	inv, ok := m.invites[code]
	if !ok || !inv.IsActive || inv.UseCount >= inv.MaxUses {
		return "", store.ErrNotFound
	}
	inv.UseCount++
	inv.UsedBy = &userID
	now := time.Now()
	inv.UsedAt = &now
	return inv.Role, nil
}

func (m *mockInviteStore) RollbackUsage(_ context.Context, code string) error {
	inv, ok := m.invites[code]
	if ok && inv.UseCount > 0 {
		inv.UseCount--
	}
	return nil
}

func (m *mockInviteStore) Revoke(_ context.Context, id uuid.UUID, createdBy uuid.UUID) error {
	inv, ok := m.byID[id]
	if !ok || (inv.CreatedBy != nil && *inv.CreatedBy != createdBy) {
		return store.ErrNotFound
	}
	inv.IsActive = false
	return nil
}

func (m *mockInviteStore) UpdateUsedBy(_ context.Context, code string, userID uuid.UUID) error {
	if inv, ok := m.invites[code]; ok {
		inv.UsedBy = &userID
	}
	return nil
}

// --- Test Setup ---

func setupTestApp(t *testing.T) (*fiber.App, *service.AuthService, *mockUserStore) {
	app, svc, userStore, _ := setupInspectableTestApp(t)
	return app, svc, userStore
}

func setupInspectableTestApp(t *testing.T) (*fiber.App, *service.AuthService, *mockUserStore, *mockSessionStore) {
	t.Helper()

	userStore := newMockUserStore()
	sessionStore := newMockSessionStore()
	inviteStore := newMockInviteStore()

	cfg := &service.Config{
		JWTSecret:     "test-jwt-secret-32-chars-minimum!!",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    720 * time.Hour,
		TOTPIssuer:    "OrbitTest",
		AdminResetKey: "test-reset-key",
		FrontendURL:   "http://localhost:3000",
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	svc := service.NewAuthService(userStore, sessionStore, inviteStore, rdb, cfg)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler := NewAuthHandler(svc, logger, "", testBootstrapSecret)

	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})
	handler.Register(app)

	return app, svc, userStore, sessionStore
}

// testBootstrapSecret is the value every test uses to authorize /auth/bootstrap.
const testBootstrapSecret = "test-bootstrap-secret"

// bootstrapHeaders returns the header map that unlocks /auth/bootstrap in tests.
func bootstrapHeaders() map[string]string {
	return map[string]string{"X-Bootstrap-Secret": testBootstrapSecret}
}

func doRequest(app *fiber.App, method, path string, body interface{}, headers map[string]string) *http.Response {
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(jsonBytes)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, _ := app.Test(req, -1)
	return resp
}

func parseResponse(resp *http.Response) map[string]interface{} {
	body, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(body, &result)
	return result
}

// --- Tests ---

func TestBootstrap_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(resp)
	if result["email"] != "admin@orbit.test" {
		t.Errorf("expected email admin@orbit.test, got %v", result["email"])
	}
	if result["role"] != "superadmin" {
		t.Errorf("expected role superadmin, got %v", result["role"])
	}
}

func TestBootstrap_AlreadyExists(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// First bootstrap
	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	// Second bootstrap should fail
	resp := doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin2@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin2",
	}, bootstrapHeaders())

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestBootstrap_ValidationFail(t *testing.T) {
	app, _, _ := setupTestApp(t)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing email", map[string]string{"password": "12345678", "display_name": "X"}},
		{"invalid email", map[string]string{"email": "notanemail", "password": "12345678", "display_name": "X"}},
		{"short password", map[string]string{"email": "a@b.com", "password": "123", "display_name": "X"}},
		{"missing name", map[string]string{"email": "a@b.com", "password": "12345678"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doRequest(app, "POST", "/auth/bootstrap", tt.body, bootstrapHeaders())
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

// TestBootstrap_RequiresSecretHeader verifies the CRITICAL fix from slot-02:
// the /auth/bootstrap endpoint must refuse requests that do not carry the
// configured X-Bootstrap-Secret header.
func TestBootstrap_RequiresSecretHeader(t *testing.T) {
	app, _, _ := setupTestApp(t)

	body := map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}

	// Missing header → 403
	resp := doRequest(app, "POST", "/auth/bootstrap", body, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing header: expected 403, got %d", resp.StatusCode)
	}

	// Wrong secret → 403
	resp = doRequest(app, "POST", "/auth/bootstrap", body, map[string]string{
		"X-Bootstrap-Secret": "wrong-secret",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong secret: expected 403, got %d", resp.StatusCode)
	}

	// Correct secret → 201
	resp = doRequest(app, "POST", "/auth/bootstrap", body, bootstrapHeaders())
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("correct secret: expected 201, got %d", resp.StatusCode)
	}
}

// TestBootstrap_DisabledWhenSecretEmpty verifies that if BOOTSTRAP_SECRET is
// not configured, the endpoint is hard-disabled even with any header supplied.
func TestBootstrap_DisabledWhenSecretEmpty(t *testing.T) {
	userStore := newMockUserStore()
	sessionStore := newMockSessionStore()
	inviteStore := newMockInviteStore()

	cfg := &service.Config{
		JWTSecret:     "test-jwt-secret-32-chars-minimum!!",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    720 * time.Hour,
		TOTPIssuer:    "OrbitTest",
		AdminResetKey: "test-reset-key",
		FrontendURL:   "http://localhost:3000",
	}
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := service.NewAuthService(userStore, sessionStore, inviteStore, rdb, cfg)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	h := NewAuthHandler(svc, logger, "", "") // empty bootstrap secret
	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	h.Register(app)

	resp := doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, map[string]string{"X-Bootstrap-Secret": "anything"})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when secret disabled, got %d", resp.StatusCode)
	}
}

func TestLogin_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// Bootstrap first
	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	// Login
	resp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "admin@orbit.test",
		"password": "securepassword123",
	}, nil)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(resp)
	if result["access_token"] == nil || result["access_token"] == "" {
		t.Error("expected access_token in response")
	}
	if result["user"] == nil {
		t.Error("expected user in response")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	app, _, _ := setupTestApp(t)

	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	resp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "admin@orbit.test",
		"password": "wrongpassword",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLogin_NonexistentUser(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "nobody@orbit.test",
		"password": "12345678",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGetMe_WithValidToken(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// Bootstrap + login
	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	loginResp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "admin@orbit.test",
		"password": "securepassword123",
	}, nil)
	loginResult := parseResponse(loginResp)
	token := loginResult["access_token"].(string)

	// Get me
	resp := doRequest(app, "GET", "/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(resp)
	if result["email"] != "admin@orbit.test" {
		t.Errorf("expected admin@orbit.test, got %v", result["email"])
	}
}

func TestLogin_AccessTokenContainsSessionJTI(t *testing.T) {
	app, _, _, sessionStore := setupInspectableTestApp(t)

	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	loginResp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "admin@orbit.test",
		"password": "securepassword123",
	}, nil)
	if loginResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(loginResp.Body)
		t.Fatalf("expected 200, got %d: %s", loginResp.StatusCode, body)
	}

	loginResult := parseResponse(loginResp)
	token, ok := loginResult["access_token"].(string)
	if !ok || token == "" {
		t.Fatal("expected access_token in response")
	}

	parsed, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return []byte("test-jwt-secret-32-chars-minimum!!"), nil
	})
	if err != nil {
		t.Fatalf("parse jwt: %v", err)
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok || !parsed.Valid {
		t.Fatal("expected valid jwt claims")
	}

	jti, _ := claims["jti"].(string)
	if jti == "" {
		t.Fatal("expected jti claim in access token")
	}

	sessionID, err := uuid.Parse(jti)
	if err != nil {
		t.Fatalf("parse jti as uuid: %v", err)
	}

	if sessionStore.sessions[sessionID] == nil {
		t.Fatalf("expected session %s referenced by jti to exist", sessionID)
	}
}

func TestGetMe_NoAuth(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "GET", "/auth/me", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGetMe_InvalidToken(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "GET", "/auth/me", nil, map[string]string{
		"Authorization": "Bearer invalid-token",
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestInviteValidate_NotFound(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "POST", "/auth/invite/validate", map[string]string{
		"code": "nonexistent",
	}, nil)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestResetAdmin_WrongKey(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "POST", "/auth/reset-admin", map[string]string{
		"reset_key":    "wrong-key",
		"email":        "admin@orbit.test",
		"new_password": "newpassword123",
	}, nil)

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// --- helper: bootstrap admin + login, return access token ---

func mustUserIDFromToken(t *testing.T, token string) uuid.UUID {
	t.Helper()
	parsed, _, err := new(jwt.Parser).ParseUnverified(token, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("unexpected token claims type")
	}
	sub, ok := claims["sub"].(string)
	if !ok || sub == "" {
		t.Fatal("missing sub claim")
	}
	userID, err := uuid.Parse(sub)
	if err != nil {
		t.Fatalf("parse sub UUID: %v", err)
	}
	return userID
}

func bootstrapAndLogin(t *testing.T, app *fiber.App) string {
	t.Helper()
	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	loginResp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "admin@orbit.test",
		"password": "securepassword123",
	}, nil)
	result := parseResponse(loginResp)
	token, ok := result["access_token"].(string)
	if !ok || token == "" {
		t.Fatal("bootstrapAndLogin: no access_token in login response")
	}
	return token
}

// --- Register tests ---

func TestRegister_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// Admin must exist before creating an invite.
	adminToken := bootstrapAndLogin(t, app)

	// Admin creates an invite.
	invResp := doRequest(app, "POST", "/auth/invites", map[string]interface{}{
		"role":     "member",
		"max_uses": 1,
	}, map[string]string{"Authorization": "Bearer " + adminToken})

	if invResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(invResp.Body)
		t.Fatalf("expected 201 from invite creation, got %d: %s", invResp.StatusCode, body)
	}
	invResult := parseResponse(invResp)
	code, ok := invResult["code"].(string)
	if !ok || code == "" {
		t.Fatal("no invite code in response")
	}

	// Register with the invite code.
	resp := doRequest(app, "POST", "/auth/register", map[string]string{
		"invite_code":  code,
		"email":        "newuser@orbit.test",
		"password":     "securepassword123",
		"display_name": "New User",
	}, nil)

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}

	result := parseResponse(resp)
	if result["email"] != "newuser@orbit.test" {
		t.Errorf("expected email newuser@orbit.test, got %v", result["email"])
	}
}

func TestRegister_InvalidInvite(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "POST", "/auth/register", map[string]string{
		"invite_code":  "badcode1",
		"email":        "newuser@orbit.test",
		"password":     "securepassword123",
		"display_name": "New User",
	}, nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	app, _, _ := setupTestApp(t)

	adminToken := bootstrapAndLogin(t, app)

	// Create invite with max_uses=2 so we can attempt registration twice.
	invResp := doRequest(app, "POST", "/auth/invites", map[string]interface{}{
		"role":     "member",
		"max_uses": 2,
	}, map[string]string{"Authorization": "Bearer " + adminToken})

	invResult := parseResponse(invResp)
	code := invResult["code"].(string)

	// First registration succeeds.
	doRequest(app, "POST", "/auth/register", map[string]string{
		"invite_code":  code,
		"email":        "dup@orbit.test",
		"password":     "securepassword123",
		"display_name": "User",
	}, nil)

	// Second registration with same email should conflict.
	resp := doRequest(app, "POST", "/auth/register", map[string]string{
		"invite_code":  code,
		"email":        "dup@orbit.test",
		"password":     "securepassword123",
		"display_name": "User",
	}, nil)

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

// --- Logout tests ---

func TestLogout_NoToken(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// requireAuth middleware runs first and returns 401 when no token is present.
	resp := doRequest(app, "POST", "/auth/logout", nil, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- Refresh tests ---

func TestRefresh_NoRefreshCookie(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// No cookie and no body refresh_token → 400 (missing refresh token).
	resp := doRequest(app, "POST", "/auth/refresh", nil, nil)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Login 2FA tests ---

// setup2FAUser bootstraps an admin, enables TOTP on its store entry directly,
// and returns the admin email and the generated TOTP secret.
func setup2FAUser(t *testing.T, userStore *mockUserStore) (email, secret string) {
	t.Helper()

	email = "admin@orbit.test"

	// Generate a real TOTP key so totp.Validate works.
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "OrbitTest",
		AccountName: email,
	})
	if err != nil {
		t.Fatalf("generate totp key: %v", err)
	}
	secret = key.Secret()

	// Inject TOTP settings directly into the mock store.
	u := userStore.byEmail[email]
	if u == nil {
		t.Fatal("admin user not found in store — call bootstrapAndLogin first")
	}
	u.TOTPSecret = &secret
	u.TOTPEnabled = true

	return email, secret
}

func TestLogin_With2FA_MissingCode(t *testing.T) {
	app, _, userStore := setupTestApp(t)

	bootstrapAndLogin(t, app)
	setup2FAUser(t, userStore)

	// Login without a TOTP code should fail with 400 and error code "2fa_required".
	resp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "admin@orbit.test",
		"password": "securepassword123",
	}, nil)

	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, body)
	}

	result := parseResponse(resp)
	if errCode, _ := result["error"].(string); errCode != "2fa_required" {
		t.Errorf("expected error field '2fa_required', got %q (full response: %v)", errCode, result)
	}
}

func TestLogin_With2FA_WrongCode(t *testing.T) {
	app, _, userStore := setupTestApp(t)

	bootstrapAndLogin(t, app)
	setup2FAUser(t, userStore)

	resp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":     "admin@orbit.test",
		"password":  "securepassword123",
		"totp_code": "000000",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401, got %d: %s", resp.StatusCode, body)
	}
}

// --- Sessions tests ---

func TestListSessions_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)

	adminToken := bootstrapAndLogin(t, app)

	resp := doRequest(app, "GET", "/auth/sessions", nil, map[string]string{
		"Authorization": "Bearer " + adminToken,
	})

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := parseResponse(resp)
	if result["sessions"] == nil {
		t.Error("expected 'sessions' key in response")
	}
}

// --- RevokeSession tests ---

func TestRevokeSession_HappyPath(t *testing.T) {
	// Build app components manually so we can inspect the session store directly.
	userStore := newMockUserStore()
	sessionStore := newMockSessionStore()
	inviteStore := newMockInviteStore()

	cfg := &service.Config{
		JWTSecret:     "test-jwt-secret-32-chars-minimum!!",
		AccessTTL:     15 * time.Minute,
		RefreshTTL:    720 * time.Hour,
		TOTPIssuer:    "OrbitTest",
		AdminResetKey: "test-reset-key",
		FrontendURL:   "http://localhost:3000",
	}
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := service.NewAuthService(userStore, sessionStore, inviteStore, rdb, cfg)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	h := NewAuthHandler(svc, logger, "", testBootstrapSecret)
	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	h.Register(app)

	// Bootstrap admin and log in.
	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email": "admin@orbit.test", "password": "securepassword123", "display_name": "Admin",
	}, bootstrapHeaders())
	loginResp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email": "admin@orbit.test", "password": "securepassword123",
	}, nil)
	loginResult := parseResponse(loginResp)
	adminToken, ok := loginResult["access_token"].(string)
	if !ok || adminToken == "" {
		t.Fatal("no access_token in login response")
	}

	// Find the session created for the admin user.
	adminUser := userStore.byEmail["admin@orbit.test"]
	if adminUser == nil {
		t.Fatal("admin user not found in store")
	}
	var sid uuid.UUID
	for id, s := range sessionStore.sessions {
		if s.UserID == adminUser.ID {
			sid = id
			break
		}
	}
	if sid == uuid.Nil {
		t.Fatal("no session found for admin user")
	}

	resp := doRequest(app, "DELETE", fmt.Sprintf("/auth/sessions/%s", sid), nil, map[string]string{
		"Authorization": "Bearer " + adminToken,
	})

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
}

func TestRevokeSession_InvalidatesExistingAccessTokenImmediately(t *testing.T) {
	app, _, _, sessionStore := setupInspectableTestApp(t)

	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, bootstrapHeaders())

	loginResp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "admin@orbit.test",
		"password": "securepassword123",
	}, nil)
	loginResult := parseResponse(loginResp)
	token, ok := loginResult["access_token"].(string)
	if !ok || token == "" {
		t.Fatal("expected access_token in login response")
	}

	getMeResp := doRequest(app, "GET", "/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if getMeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(getMeResp.Body)
		t.Fatalf("expected initial get me 200, got %d: %s", getMeResp.StatusCode, body)
	}

	var sessionID uuid.UUID
	for id := range sessionStore.sessions {
		sessionID = id
		break
	}
	if sessionID == uuid.Nil {
		t.Fatal("expected session to exist after login")
	}

	revokeResp := doRequest(app, "DELETE", fmt.Sprintf("/auth/sessions/%s", sessionID), nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if revokeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(revokeResp.Body)
		t.Fatalf("expected revoke 200, got %d: %s", revokeResp.StatusCode, body)
	}

	getMeAfterRevoke := doRequest(app, "GET", "/auth/me", nil, map[string]string{
		"Authorization": "Bearer " + token,
	})
	if getMeAfterRevoke.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(getMeAfterRevoke.Body)
		t.Fatalf("expected get me after revoke 401, got %d: %s", getMeAfterRevoke.StatusCode, body)
	}

	body := parseResponse(getMeAfterRevoke)
	if body["message"] != "Session revoked" {
		t.Fatalf("expected session revoked message, got %v", body["message"])
	}
}

// --- Invite tests ---

func TestCreateInvite_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)

	adminToken := bootstrapAndLogin(t, app)

	resp := doRequest(app, "POST", "/auth/invites", map[string]interface{}{
		"role":     "member",
		"max_uses": 5,
	}, map[string]string{"Authorization": "Bearer " + adminToken})

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, body)
	}

	result := parseResponse(resp)
	if result["code"] == nil || result["code"] == "" {
		t.Error("expected 'code' field in invite response")
	}
	if result["role"] != "member" {
		t.Errorf("expected role 'member', got %v", result["role"])
	}
}

func TestCreateInvite_NotAdmin(t *testing.T) {
	app, _, _ := setupTestApp(t)

	adminToken := bootstrapAndLogin(t, app)

	// Create an invite so we can register a non-admin user.
	invResp := doRequest(app, "POST", "/auth/invites", map[string]interface{}{
		"role":     "member",
		"max_uses": 1,
	}, map[string]string{"Authorization": "Bearer " + adminToken})
	invResult := parseResponse(invResp)
	code := invResult["code"].(string)

	doRequest(app, "POST", "/auth/register", map[string]string{
		"invite_code":  code,
		"email":        "member@orbit.test",
		"password":     "securepassword123",
		"display_name": "Member",
	}, nil)

	memberLoginResp := doRequest(app, "POST", "/auth/login", map[string]string{
		"email":    "member@orbit.test",
		"password": "securepassword123",
	}, nil)
	memberResult := parseResponse(memberLoginResp)
	memberToken, _ := memberResult["access_token"].(string)

	resp := doRequest(app, "POST", "/auth/invites", map[string]interface{}{
		"role":     "member",
		"max_uses": 1,
	}, map[string]string{"Authorization": "Bearer " + memberToken})

	if resp.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, body)
	}
}

func TestListInvites_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)

	adminToken := bootstrapAndLogin(t, app)

	// Create a couple of invites first.
	doRequest(app, "POST", "/auth/invites", map[string]interface{}{
		"role": "member", "max_uses": 1,
	}, map[string]string{"Authorization": "Bearer " + adminToken})
	doRequest(app, "POST", "/auth/invites", map[string]interface{}{
		"role": "member", "max_uses": 3,
	}, map[string]string{"Authorization": "Bearer " + adminToken})

	resp := doRequest(app, "GET", "/auth/invites", nil, map[string]string{
		"Authorization": "Bearer " + adminToken,
	})

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := parseResponse(resp)
	invites, ok := result["invites"].([]interface{})
	if !ok {
		t.Fatal("expected 'invites' array in response")
	}
	if len(invites) < 2 {
		t.Errorf("expected at least 2 invites, got %d", len(invites))
	}
}

// --- Notification Priority Mode tests ---

func TestGetNotificationPriorityMode_HappyPath(t *testing.T) {
	app, _, userStore := setupTestApp(t)
	token := bootstrapAndLogin(t, app)

	userID := mustUserIDFromToken(t, token)
	userStore.notificationPriorityMode[userID] = "smart"

	resp := doRequest(app, "GET", "/users/me/notification-priority", nil, map[string]string{"Authorization": "Bearer " + token})

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := parseResponse(resp)
	if result["mode"] != "smart" {
		t.Errorf("expected mode=smart, got %v", result["mode"])
	}
}

func TestGetNotificationPriorityMode_NoAuth(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "GET", "/users/me/notification-priority", nil, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUpdateNotificationPriorityMode_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)
	token := bootstrapAndLogin(t, app)

	resp := doRequest(app, "PUT", "/users/me/notification-priority", map[string]string{
		"mode": "off",
	}, map[string]string{"Authorization": "Bearer " + token})

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	result := parseResponse(resp)
	if result["mode"] != "off" {
		t.Errorf("expected mode=off, got %v", result["mode"])
	}
}

func TestUpdateNotificationPriorityMode_InvalidMode(t *testing.T) {
	app, _, _ := setupTestApp(t)
	token := bootstrapAndLogin(t, app)

	resp := doRequest(app, "PUT", "/users/me/notification-priority", map[string]string{
		"mode": "invalid",
	}, map[string]string{"Authorization": "Bearer " + token})

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUpdateNotificationPriorityMode_NoAuth(t *testing.T) {
	app, _, _ := setupTestApp(t)

	resp := doRequest(app, "PUT", "/users/me/notification-priority", map[string]string{
		"mode": "smart",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUpdateNotificationPriorityMode_EmptyBody(t *testing.T) {
	app, _, _ := setupTestApp(t)
	token := bootstrapAndLogin(t, app)

	resp := doRequest(app, "PUT", "/users/me/notification-priority", map[string]string{}, map[string]string{"Authorization": "Bearer " + token})

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}
