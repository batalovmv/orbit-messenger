package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// newChatApp creates a Fiber app wired with a ChatHandler backed by the given mock store.
func newChatApp(cs *mockChatStore) *fiber.App {
	app := fiber.New()
	svc := service.NewChatService(cs)
	h := NewChatHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// ---------------------------------------------------------------------------
// ListChats
// ---------------------------------------------------------------------------

func TestListChats_HappyPath(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	chatName := "General"

	cs := &mockChatStore{
		listByUserFn: func(_ context.Context, _ uuid.UUID, _ string, _ int) ([]model.ChatListItem, string, bool, error) {
			return []model.ChatListItem{
				{
					Chat:        model.Chat{ID: chatID, Type: "group", Name: &chatName, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					MemberCount: 3,
				},
			}, "", false, nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if _, ok := body["data"]; !ok {
		t.Fatal("response missing 'data' key")
	}
}

func TestListChats_NoAuth(t *testing.T) {
	app := newChatApp(&mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/chats", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CreateDirectChat
// ---------------------------------------------------------------------------

func TestCreateDirectChat_HappyPath(t *testing.T) {
	userID := uuid.New()
	otherID := uuid.New()

	cs := &mockChatStore{
		getDirectChatFn: func(_ context.Context, _, _ uuid.UUID) (*uuid.UUID, error) {
			return nil, nil
		},
		createDirectFn: func(_ context.Context, _, _ uuid.UUID) (*model.Chat, error) {
			id := uuid.New()
			return &model.Chat{ID: id, Type: "direct", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
	}

	app := newChatApp(cs)
	body := fmt.Sprintf(`{"user_id":"%s"}`, otherID.String())
	req, _ := http.NewRequest(http.MethodPost, "/chats/direct", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateDirectChat_SelfDM(t *testing.T) {
	userID := uuid.New()

	app := newChatApp(&mockChatStore{})
	body := fmt.Sprintf(`{"user_id":"%s"}`, userID.String())
	req, _ := http.NewRequest(http.MethodPost, "/chats/direct", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-DM, got %d", resp.StatusCode)
	}
}

func TestCreateDirectChat_NoAuth(t *testing.T) {
	otherID := uuid.New()

	app := newChatApp(&mockChatStore{})
	body := fmt.Sprintf(`{"user_id":"%s"}`, otherID.String())
	req, _ := http.NewRequest(http.MethodPost, "/chats/direct", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// CreateGroup
// ---------------------------------------------------------------------------

func TestCreateGroup_HappyPath(t *testing.T) {
	userID := uuid.New()

	cs := &mockChatStore{
		createFn: func(_ context.Context, chat *model.Chat) error {
			chat.ID = uuid.New()
			chat.CreatedAt = time.Now()
			chat.UpdatedAt = time.Now()
			return nil
		},
		addMemberFn: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			return nil
		},
	}

	app := newChatApp(cs)
	body := `{"name":"Engineering","description":"Dev team chat"}`
	req, _ := http.NewRequest(http.MethodPost, "/chats", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateGroup_MissingName(t *testing.T) {
	userID := uuid.New()

	app := newChatApp(&mockChatStore{})
	body := `{"name":"","description":"No name group"}`
	req, _ := http.NewRequest(http.MethodPost, "/chats", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing name, got %d", resp.StatusCode)
	}
}

func TestCreateGroup_NoAuth(t *testing.T) {
	app := newChatApp(&mockChatStore{})
	body := `{"name":"Team"}`
	req, _ := http.NewRequest(http.MethodPost, "/chats", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GetChat
// ---------------------------------------------------------------------------

func TestGetChat_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	chatName := "General"

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: chatID, Type: "group", Name: &chatName, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String(), nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetChat_NotMember(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String(), nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d", resp.StatusCode)
	}
}

func TestGetChat_InvalidID(t *testing.T) {
	userID := uuid.New()

	app := newChatApp(&mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/chats/not-a-uuid", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid ID, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// GetMembers
// ---------------------------------------------------------------------------

func TestGetMembers_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMembersFn: func(_ context.Context, _ uuid.UUID, _ string, _ int) ([]model.ChatMember, string, bool, error) {
			return []model.ChatMember{
				{ChatID: chatID, UserID: userID, Role: "member", JoinedAt: time.Now()},
			}, "", false, nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/members", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if body["data"] == nil {
		t.Fatal("response missing 'data' field")
	}
}

func TestGetMembers_NotMember(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/members", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestGetMembers_NoAuth(t *testing.T) {
	chatID := uuid.New()

	app := newChatApp(&mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/members", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GetMemberIDs
// ---------------------------------------------------------------------------

func TestGetMemberIDs_HappyPath(t *testing.T) {
	chatID := uuid.New()
	memberID := uuid.New()

	cs := &mockChatStore{
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{memberID.String()}, nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/member-ids", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if body["member_ids"] == nil {
		t.Fatal("response missing 'member_ids' field")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func readBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal body: %v (raw: %s)", err, raw)
	}
	return out
}
