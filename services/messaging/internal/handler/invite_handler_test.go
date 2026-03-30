package handler

import (
	"bytes"
	"context"
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

// newInviteApp creates a Fiber app with both authenticated and public invite routes.
func newInviteApp(is *mockInviteStore, cs *mockChatStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewInviteService(is, cs, nats)
	h := NewInviteHandler(svc, slog.Default())
	h.Register(app)
	h.RegisterPublic(app)
	return app
}

// defaultOwnerChatStore returns a store where the caller is always the owner with full permissions.
func defaultOwnerChatStore(chatID uuid.UUID) *mockChatStore {
	chatName := "Test Chat"
	return &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "owner", nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "owner", Permissions: 255}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: chatID, Type: "group", Name: &chatName, DefaultPermissions: 255}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{uuid.New().String()}, nil
		},
		addMemberFn: func(_ context.Context, _, _ uuid.UUID, _ string) error {
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// CreateInviteLink
// ---------------------------------------------------------------------------

func TestCreateInviteLink_NoAuth(t *testing.T) {
	chatID := uuid.New()

	app := newInviteApp(&mockInviteStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/invite-link", bytes.NewBufferString(`{}`))
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
// GetInviteInfo (public endpoint — no auth required)
// ---------------------------------------------------------------------------

func TestGetInviteInfo_Public(t *testing.T) {
	chatID := uuid.New()
	hash := "abc123def456"
	chatName := "Public Group"

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return &model.Chat{ID: chatID, Type: "group", Name: &chatName, DefaultPermissions: 255}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{uuid.New().String(), uuid.New().String()}, nil
		},
	}
	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, h string) (*model.InviteLink, error) {
			if h != hash {
				return nil, nil
			}
			return &model.InviteLink{
				ID:               uuid.New(),
				ChatID:           chatID,
				Hash:             hash,
				IsRevoked:        false,
				UsageLimit:       0,
				UsageCount:       0,
				RequiresApproval: false,
				CreatedAt:        time.Now(),
			}, nil
		},
	}

	app := newInviteApp(is, cs)
	// No X-User-ID — public endpoint
	req, _ := http.NewRequest(http.MethodGet, "/chats/invite/"+hash, nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("GetInviteInfo should work without auth, got %d: %s", resp.StatusCode, raw)
	}

	body := readBody(t, resp)
	if body["chat_name"] == nil {
		t.Fatal("response missing 'chat_name' field")
	}
	if body["member_count"] == nil {
		t.Fatal("response missing 'member_count' field")
	}
}

func TestGetInviteInfo_RevokedLink(t *testing.T) {
	hash := "revoked123"

	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, _ string) (*model.InviteLink, error) {
			return &model.InviteLink{
				Hash:      hash,
				IsRevoked: true, // revoked
			}, nil
		},
	}

	app := newInviteApp(is, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/chats/invite/"+hash, nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("revoked invite should return 404, got %d", resp.StatusCode)
	}
}

func TestGetInviteInfo_NotFound(t *testing.T) {
	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, _ string) (*model.InviteLink, error) {
			return nil, nil // not found
		},
	}

	app := newInviteApp(is, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/chats/invite/doesnotexist", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing invite should return 404, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// JoinByInvite
// ---------------------------------------------------------------------------

func TestJoinByInvite_AlreadyMember(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	hash := "validhash"

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil // already a member
		},
	}
	is := &mockInviteStore{
		getByHashFn: func(_ context.Context, _ string) (*model.InviteLink, error) {
			return &model.InviteLink{
				ChatID:    chatID,
				Hash:      hash,
				IsRevoked: false,
			}, nil
		},
	}

	app := newInviteApp(is, cs)
	req, _ := http.NewRequest(http.MethodPost, "/chats/join/"+hash, nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if body["status"] != "already_member" {
		t.Fatalf("expected status=already_member, got %v", body["status"])
	}
}

func TestJoinByInvite_NoAuth(t *testing.T) {
	app := newInviteApp(&mockInviteStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/chats/join/somehash", nil)
	// no X-User-ID

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("JoinByInvite should require auth, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// RevokeInviteLink
// ---------------------------------------------------------------------------

func TestRevokeInviteLink_CreatorCanRevoke(t *testing.T) {
	creatorID := uuid.New()
	chatID := uuid.New()
	linkID := uuid.New()

	is := &mockInviteStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.InviteLink, error) {
			return &model.InviteLink{
				ID:        linkID,
				ChatID:    chatID,
				CreatorID: creatorID, // caller is the creator
				Hash:      "somehash",
				IsRevoked: false,
			}, nil
		},
		revokeFn: func(_ context.Context, _ uuid.UUID) error {
			return nil
		},
	}

	app := newInviteApp(is, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/invite-links/"+linkID.String(), nil)
	req.Header.Set("X-User-ID", creatorID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("creator should be able to revoke their link, got %d: %s", resp.StatusCode, raw)
	}
}

func TestRevokeInviteLink_AdminCanRevoke(t *testing.T) {
	adminID := uuid.New()
	creatorID := uuid.New() // different from admin
	chatID := uuid.New()
	linkID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
	}
	is := &mockInviteStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.InviteLink, error) {
			return &model.InviteLink{
				ID:        linkID,
				ChatID:    chatID,
				CreatorID: creatorID, // different person created the link
				Hash:      "somehash",
				IsRevoked: false,
			}, nil
		},
		revokeFn: func(_ context.Context, _ uuid.UUID) error {
			return nil
		},
	}

	app := newInviteApp(is, cs)
	req, _ := http.NewRequest(http.MethodDelete, "/invite-links/"+linkID.String(), nil)
	req.Header.Set("X-User-ID", adminID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin should be able to revoke any link, got %d: %s", resp.StatusCode, raw)
	}
}

func TestRevokeInviteLink_MemberCannotRevoke(t *testing.T) {
	memberID := uuid.New()
	creatorID := uuid.New() // different from member
	chatID := uuid.New()
	linkID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil // plain member
		},
	}
	is := &mockInviteStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.InviteLink, error) {
			return &model.InviteLink{
				ID:        linkID,
				ChatID:    chatID,
				CreatorID: creatorID, // memberID != creatorID
				Hash:      "somehash",
				IsRevoked: false,
			}, nil
		},
	}

	app := newInviteApp(is, cs)
	req, _ := http.NewRequest(http.MethodDelete, "/invite-links/"+linkID.String(), nil)
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member who didn't create the link should get 403, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// ListInviteLinks
// ---------------------------------------------------------------------------

func TestListInviteLinks_AdminCanList(t *testing.T) {
	adminID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
	}
	is := &mockInviteStore{
		listByChatIDFn: func(_ context.Context, _ uuid.UUID) ([]model.InviteLink, error) {
			return []model.InviteLink{
				{ID: uuid.New(), Hash: "abc123", IsRevoked: false},
			}, nil
		},
	}

	app := newInviteApp(is, cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/invite-links", nil)
	req.Header.Set("X-User-ID", adminID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin should be able to list invite links, got %d: %s", resp.StatusCode, raw)
	}
}

func TestListInviteLinks_MemberForbidden(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}

	app := newInviteApp(&mockInviteStore{}, cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/invite-links", nil)
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member should NOT be able to list invite links, got %d", resp.StatusCode)
	}
}

func TestApproveJoinRequest_MemberForbidden(t *testing.T) {
	memberID := uuid.New()
	applicantID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}

	app := newInviteApp(&mockInviteStore{}, cs)
	req, _ := http.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/chats/%s/join-requests/%s/approve", chatID, applicantID),
		nil,
	)
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member should NOT be able to approve join requests, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// ListJoinRequests
// ---------------------------------------------------------------------------

func TestListJoinRequests_AdminCanList(t *testing.T) {
	adminID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
	}
	is := &mockInviteStore{
		listJoinRequestsFn: func(_ context.Context, _ uuid.UUID) ([]model.JoinRequest, error) {
			return []model.JoinRequest{
				{ChatID: chatID, UserID: uuid.New(), Status: "pending"},
			}, nil
		},
	}

	app := newInviteApp(is, cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/join-requests", nil)
	req.Header.Set("X-User-ID", adminID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin should be able to list join requests, got %d: %s", resp.StatusCode, raw)
	}
}

func TestListJoinRequests_MemberForbidden(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}

	app := newInviteApp(&mockInviteStore{}, cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/join-requests", nil)
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member should NOT be able to list join requests, got %d", resp.StatusCode)
	}
}
