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
	svc := service.NewUserService(us)
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

// ---------------------------------------------------------------------------
// GetMe
// ---------------------------------------------------------------------------

func TestGetMe_HappyPath(t *testing.T) {
	userID := uuid.New()

	us := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return sampleUser(id), nil
		},
	}

	app := newUserApp(us)
	req, _ := http.NewRequest(http.MethodGet, "/users/me", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if body["id"] == nil {
		t.Fatal("response missing 'id' field")
	}
}

func TestGetMe_NoAuth(t *testing.T) {
	app := newUserApp(&mockUserStore{})
	req, _ := http.NewRequest(http.MethodGet, "/users/me", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
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

func TestUpdateProfile_HappyPath(t *testing.T) {
	userID := uuid.New()

	us := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return sampleUser(id), nil
		},
		updateFn: func(_ context.Context, _ *model.User) error {
			return nil
		},
	}

	app := newUserApp(us)
	body := `{"display_name":"New Name"}`
	req, _ := http.NewRequest(http.MethodPut, "/users/me", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUpdateProfile_NoAuth(t *testing.T) {
	app := newUserApp(&mockUserStore{})
	body := `{"display_name":"New Name"}`
	req, _ := http.NewRequest(http.MethodPut, "/users/me", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

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

func TestSearchUsers_HappyPath(t *testing.T) {
	us := &mockUserStore{
		searchFn: func(_ context.Context, _ string, _ int) ([]model.User, error) {
			return []model.User{*sampleUser(uuid.New())}, nil
		},
	}

	app := newUserApp(us)
	req, _ := http.NewRequest(http.MethodGet, "/users?q=alice", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if body["users"] == nil {
		t.Fatal("response missing 'users' field")
	}
}

func TestSearchUsers_EmptyQuery(t *testing.T) {
	app := newUserApp(&mockUserStore{})
	req, _ := http.NewRequest(http.MethodGet, "/users", nil) // no ?q=

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", resp.StatusCode)
	}
}

func TestSearchUsers_NoResults(t *testing.T) {
	us := &mockUserStore{
		searchFn: func(_ context.Context, _ string, _ int) ([]model.User, error) {
			return []model.User{}, nil
		},
	}

	app := newUserApp(us)
	req, _ := http.NewRequest(http.MethodGet, "/users?q=zzznobody", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with empty results, got %d", resp.StatusCode)
	}
}
