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
	nats := service.NewNoopNATSPublisher()
	svc := service.NewChatService(cs, &mockMessageStore{}, nats)
	h := NewChatHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// ---------------------------------------------------------------------------
// CreateChat
// ---------------------------------------------------------------------------

func TestCreateChat_WithMembers(t *testing.T) {
	ownerID := uuid.New()
	member1 := uuid.New()
	member2 := uuid.New()

	addedRoles := map[uuid.UUID]string{}
	batchAdded := []uuid.UUID{}

	cs := &mockChatStore{
		createFn: func(_ context.Context, chat *model.Chat) error {
			chat.ID = uuid.New()
			chat.CreatedAt = time.Now()
			chat.UpdatedAt = time.Now()
			return nil
		},
		addMemberFn: func(_ context.Context, _, userID uuid.UUID, role string) error {
			addedRoles[userID] = role
			return nil
		},
		addMembersFn: func(_ context.Context, _ uuid.UUID, userIDs []uuid.UUID, role string) error {
			batchAdded = append(batchAdded, userIDs...)
			return nil
		},
	}

	app := newChatApp(cs)
	body := fmt.Sprintf(`{"type":"group","name":"Team","member_ids":["%s","%s"]}`, member1, member2)
	req, _ := http.NewRequest(http.MethodPost, "/chats", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", ownerID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}

	// Owner must be added with role "owner"
	if addedRoles[ownerID] != "owner" {
		t.Errorf("owner should have role=owner, got %q", addedRoles[ownerID])
	}
	// Initial members must be batch-added
	if len(batchAdded) != 2 {
		t.Errorf("expected 2 batch-added members, got %d", len(batchAdded))
	}
}

// ---------------------------------------------------------------------------
// AddMembers — permission tests
// ---------------------------------------------------------------------------

func TestAddMembers_WithoutPermission(t *testing.T) {
	memberID := uuid.New()
	newMemberID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			// member role, permissions=1 (CanSendMessages only, no CanAddMembers)
			return &model.ChatMember{Role: "member", Permissions: 1}, nil
		},
	}

	app := newChatApp(cs)
	body := fmt.Sprintf(`{"user_ids":["%s"]}`, newMemberID)
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member without CanAddMembers should get 403, got %d", resp.StatusCode)
	}
}

func TestAddMembers_MemberWithCanAddMembersPermission(t *testing.T) {
	memberID := uuid.New()
	newMemberID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			// member role but explicitly granted CanAddMembers (bit 2 = 4)
			return &model.ChatMember{Role: "member", Permissions: 4}, nil
		},
		addMembersFn: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID, _ string) error {
			return nil
		},
	}

	app := newChatApp(cs)
	body := fmt.Sprintf(`{"user_ids":["%s"]}`, newMemberID)
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/members", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("member with explicit CanAddMembers should succeed, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GetMembers
// ---------------------------------------------------------------------------

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

func TestGetMemberIDs_RequiresMembership(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/member-ids", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member should get 403, got %d", resp.StatusCode)
	}
}


// ---------------------------------------------------------------------------
// UpdateDefaultPermissions
// ---------------------------------------------------------------------------

func TestUpdateDefaultPermissions_OwnerAllowed(t *testing.T) {
	ownerID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "owner", Permissions: 255}, nil
		},
		updateDefaultPermsFn: func(_ context.Context, _ uuid.UUID, _ int64) error {
			return nil
		},
	}

	app := newChatApp(cs)
	body := `{"permissions":239}` // 255 & ^16 — disable CanChangeInfo for members
	req, _ := http.NewRequest(http.MethodPut, "/chats/"+chatID.String()+"/permissions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", ownerID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("owner should be able to set default permissions, got %d: %s", resp.StatusCode, raw)
	}
}

func TestUpdateDefaultPermissions_MemberForbidden(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: 1}, nil
		},
	}

	app := newChatApp(cs)
	body := `{"permissions":0}`
	req, _ := http.NewRequest(http.MethodPut, "/chats/"+chatID.String()+"/permissions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member should NOT be able to change default permissions, got %d", resp.StatusCode)
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
