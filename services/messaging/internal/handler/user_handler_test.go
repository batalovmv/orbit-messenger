package handler

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// newUserApp creates a Fiber app wired with a UserHandler backed by the given mock store.
func newUserApp(us *mockUserStore) *fiber.App {
	app := fiber.New()
	svc := service.NewUserService(us, &mockChatStore{}, &mockPrivacySettingsStore{})
	h := NewUserHandler(svc, slog.Default())
	h.Register(app)
	return app
}

func sampleUser(id uuid.UUID) *model.User {
	return &model.User{
		ID:          id,
		Email:       "user@example.com",
		DisplayName: "Test User",
		Status:      "online",
		Role:        "member",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func TestGetMe_UserNotFound(t *testing.T) {
	userID := uuid.New()

	us := &mockUserStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.User, error) {
			return nil, nil // store returns nil → service returns 404
		},
	}

	app := newUserApp(us)
	req, _ := http.NewRequest(http.MethodGet, "/users/me", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// UpdateProfile
// ---------------------------------------------------------------------------

func TestUpdateProfile_UserNotFound(t *testing.T) {
	userID := uuid.New()

	us := &mockUserStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.User, error) {
			return nil, nil
		},
	}

	app := newUserApp(us)
	body := `{"display_name":"Ghost"}`
	req, _ := http.NewRequest(http.MethodPut, "/users/me", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// SearchUsers
// ---------------------------------------------------------------------------

func TestSearchUsers_EmptyQuery_ReturnsAllUsers(t *testing.T) {
	us := &mockUserStore{
		listAllFn: func(_ context.Context, _ int) ([]model.User, error) {
			return []model.User{*sampleUser(uuid.New())}, nil
		},
	}

	app := newUserApp(us)
	req, _ := http.NewRequest(http.MethodGet, "/users", nil) // no ?q= → returns all users
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for empty query (list all), got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if body["users"] == nil {
		t.Fatal("response missing 'users' field")
	}
}

// ---------------------------------------------------------------------------
// GetUser
// ---------------------------------------------------------------------------

func TestGetUser_NoAuth(t *testing.T) {
	targetID := uuid.New()

	us := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return sampleUser(id), nil
		},
	}

	// GetUser does NOT require auth — no X-User-ID still returns 200 with PII stripped.
	// The handler calls getUserID with _ (ignores error) so it falls through to response.
	app := newUserApp(us)
	req, _ := http.NewRequest(http.MethodGet, "/users/"+targetID.String(), nil)
	// No X-User-ID header → callerID will be uuid.Nil

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	// GetUser returns 200 even without auth; PII is stripped for non-self callers.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (GetUser has no auth gate), got %d", resp.StatusCode)
	}
}

func TestGetUser_StripsPII(t *testing.T) {
	callerID := uuid.New()
	targetID := uuid.New() // different user → PII stripped

	phone := "+79001234567"
	us := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			u := sampleUser(id)
			u.Phone = &phone
			return u, nil
		},
	}

	app := newUserApp(us)
	req, _ := http.NewRequest(http.MethodGet, "/users/"+targetID.String(), nil)
	req.Header.Set("X-User-ID", callerID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if email, ok := body["email"]; ok && email != "" {
		t.Fatalf("expected email stripped for non-self lookup, got %q", email)
	}
	// phone is omitempty so it won't appear in the JSON at all when nil
	if _, ok := body["phone"]; ok {
		t.Fatal("expected phone field absent for non-self lookup")
	}
}

// strPtr is a helper to get a pointer to a string literal.
func strPtr(s string) *string { return &s }
