// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

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

func TestCreateDirectChat_Success(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	chatID := uuid.New()
	created := false

	cs := &mockChatStore{
		getDirectChatFn: func(_ context.Context, gotUserID, gotOtherUserID uuid.UUID) (*uuid.UUID, error) {
			if gotUserID != userID || gotOtherUserID != otherUserID {
				t.Fatalf("unexpected direct chat lookup args: %s %s", gotUserID, gotOtherUserID)
			}
			return nil, nil
		},
		createDirectFn: func(_ context.Context, gotUserID, gotOtherUserID uuid.UUID) (*model.Chat, error) {
			if gotUserID != userID || gotOtherUserID != otherUserID {
				t.Fatalf("unexpected create direct args: %s %s", gotUserID, gotOtherUserID)
			}
			created = true
			return &model.Chat{ID: chatID, Type: "direct"}, nil
		},
	}

	app := newChatApp(cs)
	body := fmt.Sprintf(`{"user_id":"%s"}`, otherUserID)
	req, _ := http.NewRequest(http.MethodPost, "/chats/direct", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}
	if !created {
		t.Fatal("expected direct chat to be created")
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
	req, _ := http.NewRequest(http.MethodGet, "/internal/chats/"+chatID.String()+"/member-ids", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	// Route requires X-Internal-Token — without it the handler returns 403.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-member should get 403, got %d", resp.StatusCode)
	}
}

func TestUpdateOwnMemberPreferences_Success(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (*model.ChatMember, error) {
			if gotChatID != chatID || gotUserID != userID {
				t.Fatalf("unexpected get member args: %s %s", gotChatID, gotUserID)
			}
			return &model.ChatMember{ChatID: chatID, UserID: userID, Role: "member"}, nil
		},
		updateMemberPrefsFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID, prefs model.ChatMemberPreferences) (*model.ChatMember, error) {
			if gotChatID != chatID || gotUserID != userID {
				t.Fatalf("unexpected update args: %s %s", gotChatID, gotUserID)
			}
			if prefs.IsPinned == nil || !*prefs.IsPinned {
				t.Fatal("expected is_pinned=true")
			}
			return &model.ChatMember{
				ChatID:            chatID,
				UserID:            userID,
				Role:              "member",
				NotificationLevel: "all",
				IsPinned:          true,
			}, nil
		},
	}

	app := newChatApp(cs)
	req, _ := http.NewRequest(http.MethodPatch, "/chats/"+chatID.String()+"/members/me", bytes.NewBufferString(`{"is_pinned":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	body := readBody(t, resp)
	if body["is_pinned"] != true {
		t.Fatalf("expected is_pinned=true, got %#v", body["is_pinned"])
	}
}

func TestUpdateOwnMemberPreferences_ValidationFail(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	app := newChatApp(&mockChatStore{})
	req, _ := http.NewRequest(http.MethodPatch, "/chats/"+chatID.String()+"/members/me", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestUpdateOwnMemberPreferences_AuthFail(t *testing.T) {
	chatID := uuid.New()

	app := newChatApp(&mockChatStore{})
	req, _ := http.NewRequest(http.MethodPatch, "/chats/"+chatID.String()+"/members/me", bytes.NewBufferString(`{"is_pinned":true}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401, got %d: %s", resp.StatusCode, raw)
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
