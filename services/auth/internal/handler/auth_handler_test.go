package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/service"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

// --- Mock Stores ---

type mockUserStore struct {
	users      map[uuid.UUID]*model.User
	byEmail    map[string]*model.User
	adminCount int
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{
		users:   make(map[uuid.UUID]*model.User),
		byEmail: make(map[string]*model.User),
	}
}

func (m *mockUserStore) Create(_ context.Context, u *model.User) error {
	u.ID = uuid.New()
	u.Status = "offline"
	u.TOTPEnabled = false
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	m.users[u.ID] = u
	m.byEmail[u.Email] = u
	if u.Role == "admin" {
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

func (m *mockInviteStore) UseInvite(_ context.Context, code string, userID uuid.UUID) error {
	inv, ok := m.invites[code]
	if !ok || !inv.IsActive || inv.UseCount >= inv.MaxUses {
		return store.ErrNotFound
	}
	inv.UseCount++
	inv.UsedBy = &userID
	now := time.Now()
	inv.UsedAt = &now
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

// --- Mock Redis (minimal) ---

type miniredis struct{}

// We'll use a nil redis client and handle panics in tests that don't need Redis.

// --- Test Setup ---

func setupTestApp(t *testing.T) (*fiber.App, *service.AuthService, *mockUserStore) {
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

	// Create a mini Redis mock - use real client pointing to nothing, service handles errors
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:0"})

	svc := service.NewAuthService(userStore, sessionStore, inviteStore, rdb, cfg)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	handler := NewAuthHandler(svc, logger)

	app := fiber.New(fiber.Config{
		ErrorHandler: response.FiberErrorHandler,
	})
	handler.Register(app)

	return app, svc, userStore
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
	}, nil)

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	result := parseResponse(resp)
	if result["email"] != "admin@orbit.test" {
		t.Errorf("expected email admin@orbit.test, got %v", result["email"])
	}
	if result["role"] != "admin" {
		t.Errorf("expected role admin, got %v", result["role"])
	}
}

func TestBootstrap_AlreadyExists(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// First bootstrap
	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, nil)

	// Second bootstrap should fail
	resp := doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin2@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin2",
	}, nil)

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
			resp := doRequest(app, "POST", "/auth/bootstrap", tt.body, nil)
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("expected 400, got %d", resp.StatusCode)
			}
		})
	}
}

func TestLogin_HappyPath(t *testing.T) {
	app, _, _ := setupTestApp(t)

	// Bootstrap first
	doRequest(app, "POST", "/auth/bootstrap", map[string]string{
		"email":        "admin@orbit.test",
		"password":     "securepassword123",
		"display_name": "Admin",
	}, nil)

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
	}, nil)

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
	}, nil)

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
