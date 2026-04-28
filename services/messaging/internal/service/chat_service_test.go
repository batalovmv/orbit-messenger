// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestChatService(cs *mockChatStore, rec *RecordingPublisher) *ChatService {
	return NewChatService(cs, &mockMessageStore{}, rec)
}

type stubMessagePollHydrator struct {
	hydrateFn func(ctx context.Context, userID uuid.UUID, msgs []model.Message) error
}

func (s *stubMessagePollHydrator) HydrateMessagePolls(ctx context.Context, userID uuid.UUID, msgs []model.Message) error {
	if s.hydrateFn != nil {
		return s.hydrateFn(ctx, userID, msgs)
	}

	return nil
}

func defaultGroupChat(chatID uuid.UUID) *model.Chat {
	name := "Test Group"
	return &model.Chat{
		ID:                 chatID,
		Type:               "group",
		Name:               &name,
		DefaultPermissions: permissions.AllPermissions,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
}

func assertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

// ---------------------------------------------------------------------------
// CreateChat — NATS verification
// ---------------------------------------------------------------------------

func TestCreateChat_NATS_ChatCreated(t *testing.T) {
	ownerID := uuid.New()
	member1 := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		createFn: func(_ context.Context, chat *model.Chat) error {
			chat.ID = uuid.New()
			chat.CreatedAt = time.Now()
			chat.UpdatedAt = time.Now()
			return nil
		},
		addMemberFn:  func(_ context.Context, _, _ uuid.UUID, _ string) error { return nil },
		addMembersFn: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID, _ string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String(), member1.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	chat, err := svc.CreateChat(context.Background(), ownerID, "group", "My Group", "desc", []uuid.UUID{member1})
	if err != nil {
		t.Fatalf("CreateChat: %v", err)
	}

	events := rec.FindByEvent("chat_created")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_created event, got %d", len(events))
	}
	ev := events[0]
	if !strings.Contains(ev.Subject, chat.ID.String()) {
		t.Errorf("subject should contain chat ID, got %q", ev.Subject)
	}
	if len(ev.MemberIDs) != 2 {
		t.Errorf("expected 2 memberIDs, got %d", len(ev.MemberIDs))
	}
	if ev.SenderID != ownerID.String() {
		t.Errorf("senderID should be owner %s, got %s", ownerID, ev.SenderID)
	}
}

// ---------------------------------------------------------------------------
// AddMembers — NATS verification
// ---------------------------------------------------------------------------

func TestAddMembers_NATS_PerMemberEvent(t *testing.T) {
	ownerID := uuid.New()
	m1, m2 := uuid.New(), uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == ownerID {
				return &model.ChatMember{Role: "owner"}, nil
			}
			return nil, nil
		},
		addMembersFn: func(_ context.Context, _ uuid.UUID, _ []uuid.UUID, _ string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String(), m1.String(), m2.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.AddMembers(context.Background(), chatID, ownerID, []uuid.UUID{m1, m2})
	if err != nil {
		t.Fatalf("AddMembers: %v", err)
	}

	events := rec.FindByEvent("chat_member_added")
	if len(events) != 2 {
		t.Fatalf("expected 2 chat_member_added events (one per member), got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// RemoveMember — NATS: includes removed user
// ---------------------------------------------------------------------------

func TestRemoveMember_NATS_IncludesRemovedUser(t *testing.T) {
	ownerID := uuid.New()
	targetID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == ownerID {
				return &model.ChatMember{Role: "owner"}, nil
			}
			return &model.ChatMember{Role: "member"}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			// memberIDs collected BEFORE deletion includes the target
			return []string{ownerID.String(), targetID.String()}, nil
		},
		removeMemberFn: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}

	svc := newTestChatService(cs, rec)
	err := svc.RemoveMember(context.Background(), chatID, ownerID, targetID)
	if err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	events := rec.FindByEvent("chat_member_removed")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_member_removed, got %d", len(events))
	}
	// Verify removed user is in the memberIDs (so they receive the event)
	found := false
	for _, id := range events[0].MemberIDs {
		if id == targetID.String() {
			found = true
		}
	}
	if !found {
		t.Error("removed user should be in memberIDs so they receive the removal event")
	}
}

// ---------------------------------------------------------------------------
// UpdateChat — NATS
// ---------------------------------------------------------------------------

func TestUpdateChat_NATS_ChatUpdated(t *testing.T) {
	ownerID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}
	newName := "Renamed"

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "owner"}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return defaultGroupChat(chatID), nil
		},
		updateChatFn: func(_ context.Context, _ uuid.UUID, _, _, _ *string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	_, err := svc.UpdateChat(context.Background(), chatID, ownerID, &newName, nil, nil)
	if err != nil {
		t.Fatalf("UpdateChat: %v", err)
	}

	events := rec.FindByEvent("chat_updated")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_updated, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// UpdateMemberRole — NATS
// ---------------------------------------------------------------------------

func TestUpdateMemberRole_NATS_MemberUpdated(t *testing.T) {
	ownerID := uuid.New()
	targetID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == ownerID {
				return &model.ChatMember{Role: "owner"}, nil
			}
			return &model.ChatMember{Role: "member"}, nil
		},
		updateMemberRoleFn: func(_ context.Context, _, _ uuid.UUID, _ string, _ int64, _ *string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String(), targetID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.UpdateMemberRole(context.Background(), chatID, ownerID, targetID, "admin", permissions.AllPermissions, nil)
	if err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}

	events := rec.FindByEvent("chat_member_updated")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_member_updated, got %d", len(events))
	}
}

func TestUpdateMemberRole_DemoteResetsPermissions(t *testing.T) {
	ownerID := uuid.New()
	targetID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	var savedPerms int64 = -1
	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == ownerID {
				return &model.ChatMember{Role: "owner"}, nil
			}
			return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
		},
		updateMemberRoleFn: func(_ context.Context, _, _ uuid.UUID, _ string, perms int64, _ *string) error {
			savedPerms = perms
			return nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String(), targetID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	// Demote admin to member — even if caller passes permissions=255, they should be reset to 0
	err := svc.UpdateMemberRole(context.Background(), chatID, ownerID, targetID, "member", permissions.AllPermissions, nil)
	if err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}
	if savedPerms != 0 {
		t.Fatalf("demoted member should have permissions=0, got %d", savedPerms)
	}
}

func TestUpdateMemberPreferences_Success(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

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
			if prefs.IsArchived == nil || !*prefs.IsArchived {
				t.Fatal("expected is_archived=true")
			}
			return &model.ChatMember{
				ChatID:      chatID,
				UserID:      userID,
				Role:        "member",
				IsArchived:  true,
				IsPinned:    false,
				IsMuted:     false,
				DisplayName: "Orbit",
			}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	value := true
	member, err := svc.UpdateMemberPreferences(context.Background(), chatID, userID, model.ChatMemberPreferences{
		IsArchived: &value,
	})
	if err != nil {
		t.Fatalf("UpdateMemberPreferences: %v", err)
	}
	if !member.IsArchived {
		t.Fatal("expected archived member response")
	}
}

func TestUpdateMemberPreferences_EmptyPayloadRejected(t *testing.T) {
	rec := &RecordingPublisher{}
	cs := &mockChatStore{}

	svc := newTestChatService(cs, rec)
	_, err := svc.UpdateMemberPreferences(context.Background(), uuid.New(), uuid.New(), model.ChatMemberPreferences{})
	assertAppError(t, err, 400)
}

func TestUpdateMemberPreferences_NonMemberForbidden(t *testing.T) {
	rec := &RecordingPublisher{}
	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return nil, nil
		},
	}

	svc := newTestChatService(cs, rec)
	value := true
	_, err := svc.UpdateMemberPreferences(context.Background(), uuid.New(), uuid.New(), model.ChatMemberPreferences{
		IsMuted: &value,
	})
	assertAppError(t, err, 403)
}

// ---------------------------------------------------------------------------
// DeleteChat — NATS: sent BEFORE deletion
// ---------------------------------------------------------------------------

func TestDeleteChat_NATS_SentBeforeDeletion(t *testing.T) {
	ownerID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}
	deleteCallOrder := 0
	natsCallOrder := 0
	callCounter := 0

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "owner", nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			callCounter++
			natsCallOrder = callCounter
			return []string{ownerID.String()}, nil
		},
		deleteChatFn: func(_ context.Context, _ uuid.UUID) error {
			callCounter++
			deleteCallOrder = callCounter
			return nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.DeleteChat(context.Background(), chatID, ownerID)
	if err != nil {
		t.Fatalf("DeleteChat: %v", err)
	}

	if natsCallOrder >= deleteCallOrder {
		t.Error("GetMemberIDs (for NATS) should be called BEFORE DeleteChat")
	}

	events := rec.FindByEvent("chat_deleted")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_deleted, got %d", len(events))
	}
}

// ---------------------------------------------------------------------------
// Permission edge cases
// ---------------------------------------------------------------------------

func TestRemoveMember_AdminCannotKickAdmin(t *testing.T) {
	adminID := uuid.New()
	otherAdminID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == adminID {
				return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
			}
			return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{adminID.String(), otherAdminID.String()}, nil
		},
		removeMemberFn: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}

	svc := newTestChatService(cs, rec)
	err := svc.RemoveMember(context.Background(), chatID, adminID, otherAdminID)
	assertAppError(t, err, 403)
}

func TestRemoveMember_AdminWithBanPerms_CanKickMember(t *testing.T) {
	adminID := uuid.New()
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == adminID {
				return &model.ChatMember{Role: "admin", Permissions: permissions.CanBanUsers}, nil
			}
			return &model.ChatMember{Role: "member"}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{adminID.String(), memberID.String()}, nil
		},
		removeMemberFn: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}

	svc := newTestChatService(cs, rec)
	err := svc.RemoveMember(context.Background(), chatID, adminID, memberID)
	if err != nil {
		t.Fatalf("admin with CanBanUsers should be able to kick member: %v", err)
	}
}

func TestUpdateChat_MemberWithoutChangeInfoPerm_Forbidden(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}
	newName := "Hacked"

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			// member with all permissions EXCEPT CanChangeInfo
			return &model.ChatMember{
				Role:        "member",
				Permissions: permissions.AllPermissions &^ permissions.CanChangeInfo,
			}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return defaultGroupChat(chatID), nil
		},
	}

	svc := newTestChatService(cs, rec)
	_, err := svc.UpdateChat(context.Background(), chatID, memberID, &newName, nil, nil)
	assertAppError(t, err, 403)
}

func TestUpdateChat_MemberWithChangeInfoPerm_Allowed(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}
	newName := "Allowed"

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{
				Role:        "member",
				Permissions: permissions.CanChangeInfo,
			}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return defaultGroupChat(chatID), nil
		},
		updateChatFn: func(_ context.Context, _ uuid.UUID, _, _, _ *string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{memberID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	_, err := svc.UpdateChat(context.Background(), chatID, memberID, &newName, nil, nil)
	if err != nil {
		t.Fatalf("member with CanChangeInfo should be allowed: %v", err)
	}
}

func TestDeleteChat_OnlyOwnerCanDelete(t *testing.T) {
	adminID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.DeleteChat(context.Background(), chatID, adminID)
	assertAppError(t, err, 403)
}

func TestCreateDirectChat_SelfDM_RedirectsToSavedMessages(t *testing.T) {
	userID := uuid.New()
	savedChatID := uuid.New()
	rec := &RecordingPublisher{}
	cs := &mockChatStore{
		getOrCreateSavedChatFn: func(_ context.Context, u uuid.UUID) (*model.Chat, error) {
			if u != userID {
				t.Fatalf("expected saved chat lookup for %s, got %s", userID, u)
			}
			return &model.Chat{ID: savedChatID, Type: "direct"}, nil
		},
		createDirectFn: func(_ context.Context, _, _ uuid.UUID) (*model.Chat, error) {
			t.Fatal("self-DM must not call CreateDirectChat — expected Saved Messages redirect")
			return nil, nil
		},
	}

	svc := newTestChatService(cs, rec)
	chat, err := svc.CreateDirectChat(context.Background(), userID, userID)
	if err != nil {
		t.Fatalf("expected self-DM to succeed, got: %v", err)
	}
	if chat == nil || chat.ID != savedChatID {
		t.Fatalf("expected Saved Messages chat %s, got %#v", savedChatID, chat)
	}
}

func TestCreateDirectChat_NATS_ChatCreated(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

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
			return &model.Chat{
				ID:   chatID,
				Type: "direct",
			}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	chat, err := svc.CreateDirectChat(context.Background(), userID, otherUserID)
	if err != nil {
		t.Fatalf("CreateDirectChat: %v", err)
	}

	if chat.ID != chatID {
		t.Fatalf("expected chat ID %s, got %s", chatID, chat.ID)
	}

	events := rec.FindByEvent("chat_created")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_created event, got %d", len(events))
	}

	ev := events[0]
	if !strings.Contains(ev.Subject, chatID.String()) {
		t.Fatalf("subject should contain chat ID, got %q", ev.Subject)
	}
	if len(ev.MemberIDs) != 2 {
		t.Fatalf("expected 2 member IDs, got %d", len(ev.MemberIDs))
	}
	if ev.MemberIDs[0] != userID.String() || ev.MemberIDs[1] != otherUserID.String() {
		t.Fatalf("unexpected member IDs: %#v", ev.MemberIDs)
	}
	if ev.SenderID != userID.String() {
		t.Fatalf("expected sender ID %s, got %s", userID, ev.SenderID)
	}
}

func TestCreateDirectChat_ExistingChat_ReturnsExisting(t *testing.T) {
	userID := uuid.New()
	otherUserID := uuid.New()
	existingChatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getDirectChatFn: func(_ context.Context, gotUserID, gotOtherUserID uuid.UUID) (*uuid.UUID, error) {
			if gotUserID != userID || gotOtherUserID != otherUserID {
				t.Fatalf("unexpected direct chat lookup args: %s %s", gotUserID, gotOtherUserID)
			}
			return &existingChatID, nil
		},
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Chat, error) {
			if id != existingChatID {
				t.Fatalf("expected lookup for existing chat %s, got %s", existingChatID, id)
			}
			return &model.Chat{ID: existingChatID, Type: "direct"}, nil
		},
		createDirectFn: func(_ context.Context, _, _ uuid.UUID) (*model.Chat, error) {
			t.Fatal("existing direct chat must not trigger CreateDirectChat")
			return nil, nil
		},
	}

	svc := newTestChatService(cs, rec)
	chat, err := svc.CreateDirectChat(context.Background(), userID, otherUserID)
	if err != nil {
		t.Fatalf("CreateDirectChat (existing): %v", err)
	}
	if chat == nil || chat.ID != existingChatID {
		t.Fatalf("expected existing chat %s, got %#v", existingChatID, chat)
	}
}

func TestCreateChat_GroupDefaultPerms(t *testing.T) {
	ownerID := uuid.New()
	rec := &RecordingPublisher{}

	var createdChat *model.Chat
	cs := &mockChatStore{
		createFn: func(_ context.Context, chat *model.Chat) error {
			chat.ID = uuid.New()
			chat.CreatedAt = time.Now()
			chat.UpdatedAt = time.Now()
			createdChat = chat
			return nil
		},
		addMemberFn: func(_ context.Context, _, _ uuid.UUID, _ string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	_, err := svc.CreateChat(context.Background(), ownerID, "group", "My Group", "", nil)
	if err != nil {
		t.Fatalf("CreateChat group: %v", err)
	}
	expected := permissions.DefaultGroupPermissions
	if createdChat.DefaultPermissions != expected {
		t.Fatalf("group default_permissions should be %d, got %d", expected, createdChat.DefaultPermissions)
	}
}

func TestCreateChat_InvalidType_Rejected(t *testing.T) {
	rec := &RecordingPublisher{}
	cs := &mockChatStore{}

	svc := newTestChatService(cs, rec)
	_, err := svc.CreateChat(context.Background(), uuid.New(), "supergroup", "Oops", "", nil)
	assertAppError(t, err, 400)
}

func TestCreateChat_EmptyName_Rejected(t *testing.T) {
	rec := &RecordingPublisher{}
	cs := &mockChatStore{}

	svc := newTestChatService(cs, rec)
	_, err := svc.CreateChat(context.Background(), uuid.New(), "group", "", "", nil)
	assertAppError(t, err, 400)
}

func TestUpdateMemberRole_AdminCannotPromote(t *testing.T) {
	adminID := uuid.New()
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == adminID {
				return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
			}
			return &model.ChatMember{Role: "member"}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.UpdateMemberRole(context.Background(), chatID, adminID, memberID, "admin", 0, nil)
	assertAppError(t, err, 403)
}

func TestUpdateMemberRole_AdminCannotPromoteToOwner(t *testing.T) {
	adminID := uuid.New()
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == adminID {
				return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
			}
			return &model.ChatMember{Role: "member"}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.UpdateMemberRole(context.Background(), chatID, adminID, memberID, "owner", 0, nil)
	assertAppError(t, err, 403)
}

func TestUpdateMemberRole_OwnerCanTransferOwnership(t *testing.T) {
	ownerID := uuid.New()
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}
	updated := false

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == ownerID {
				return &model.ChatMember{Role: "owner"}, nil
			}
			return &model.ChatMember{Role: "member"}, nil
		},
		updateMemberRoleFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID, role string, perms int64, _ *string) error {
			if gotChatID != chatID {
				t.Fatalf("unexpected chatID: got %s want %s", gotChatID, chatID)
			}
			if gotUserID != memberID {
				t.Fatalf("unexpected targetID: got %s want %s", gotUserID, memberID)
			}
			if role != "owner" {
				t.Fatalf("unexpected role: got %s want owner", role)
			}
			updated = true
			return nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String(), memberID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	if err := svc.UpdateMemberRole(context.Background(), chatID, ownerID, memberID, "owner", 0, nil); err != nil {
		t.Fatalf("owner should be able to assign owner role: %v", err)
	}
	if !updated {
		t.Fatal("expected UpdateMemberRole store call")
	}
}

func TestUpdateMemberRole_CannotChangeOwnerRole(t *testing.T) {
	ownerID := uuid.New()
	otherOwnerAttempt := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == otherOwnerAttempt {
				return &model.ChatMember{Role: "owner"}, nil
			}
			return &model.ChatMember{Role: "owner"}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.UpdateMemberRole(context.Background(), chatID, ownerID, otherOwnerAttempt, "admin", 0, nil)
	assertAppError(t, err, 403)
}

func TestRemoveMember_SelfLeave_AlwaysAllowed(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}
	removed := false

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{memberID.String()}, nil
		},
		removeMemberFn: func(_ context.Context, _, _ uuid.UUID) error {
			removed = true
			return nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.RemoveMember(context.Background(), chatID, memberID, memberID)
	if err != nil {
		t.Fatalf("self-leave should always work: %v", err)
	}
	if !removed {
		t.Fatal("RemoveMember should have been called")
	}
}

func TestRemoveMember_SelfLeave_FanoutIncludesLeaver(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{"another-device-user"}, nil
		},
		removeMemberFn: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}

	svc := newTestChatService(cs, rec)
	if err := svc.RemoveMember(context.Background(), chatID, memberID, memberID); err != nil {
		t.Fatalf("self-leave should succeed: %v", err)
	}

	events := rec.FindByEvent("chat_member_removed")
	if len(events) != 1 {
		t.Fatalf("expected 1 chat_member_removed event, got %d", len(events))
	}

	foundLeaver := false
	for _, id := range events[0].MemberIDs {
		if id == memberID.String() {
			foundLeaver = true
			break
		}
	}
	if !foundLeaver {
		t.Fatal("expected self-leave fanout to include the leaver")
	}
}

func TestRemoveMember_CannotKickOwner(t *testing.T) {
	adminID := uuid.New()
	ownerID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, userID uuid.UUID) (*model.ChatMember, error) {
			if userID == adminID {
				return &model.ChatMember{Role: "admin"}, nil
			}
			return &model.ChatMember{Role: "owner"}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.RemoveMember(context.Background(), chatID, adminID, ownerID)
	assertAppError(t, err, 403)
}

func TestSetSlowMode_MemberForbidden(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member"}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.SetSlowMode(context.Background(), chatID, memberID, 30)
	assertAppError(t, err, 403)
}

func TestSetSlowMode_AdminAllowed(t *testing.T) {
	adminID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "admin"}, nil
		},
		setSlowModeFn: func(_ context.Context, _ uuid.UUID, _ int) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{adminID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	err := svc.SetSlowMode(context.Background(), chatID, adminID, 30)
	if err != nil {
		t.Fatalf("admin should be able to set slow mode: %v", err)
	}
}

func TestGetChat_NonMember_Forbidden(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}

	svc := newTestChatService(cs, rec)
	_, err := svc.GetChat(context.Background(), chatID, userID)
	assertAppError(t, err, 403)
}

func TestListChats_HydratesPollLastMessage(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	messageID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		listByUserFn: func(_ context.Context, gotUserID uuid.UUID, cursor string, limit int) ([]model.ChatListItem, string, bool, error) {
			if gotUserID != userID {
				t.Fatalf("expected userID %s, got %s", userID, gotUserID)
			}
			if cursor != "" {
				t.Fatalf("expected empty cursor, got %q", cursor)
			}
			if limit != 50 {
				t.Fatalf("expected limit 50, got %d", limit)
			}

			question := "Where?"
			return []model.ChatListItem{{
				Chat: *defaultGroupChat(chatID),
				LastMessage: &model.Message{
					ID:      messageID,
					ChatID:  chatID,
					Type:    "poll",
					Content: &question,
				},
			}}, "", false, nil
		},
	}

	polls := &stubMessagePollHydrator{
		hydrateFn: func(_ context.Context, gotUserID uuid.UUID, msgs []model.Message) error {
			if gotUserID != userID {
				t.Fatalf("expected hydrate userID %s, got %s", userID, gotUserID)
			}
			if len(msgs) != 1 {
				t.Fatalf("expected 1 poll message, got %d", len(msgs))
			}

			msgs[0].Poll = &model.Poll{
				ID:        uuid.New(),
				MessageID: messageID,
				Question:  "Where?",
			}
			return nil
		},
	}

	svc := NewChatService(cs, &mockMessageStore{}, rec, polls)
	items, nextCursor, hasMore, err := svc.ListChats(context.Background(), userID, "", 50)
	if err != nil {
		t.Fatalf("ListChats: %v", err)
	}

	if nextCursor != "" {
		t.Fatalf("expected empty next cursor, got %q", nextCursor)
	}
	if hasMore {
		t.Fatal("expected hasMore=false")
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 chat list item, got %d", len(items))
	}
	if items[0].LastMessage == nil || items[0].LastMessage.Poll == nil {
		t.Fatal("expected hydrated poll on last message")
	}
	if items[0].LastMessage.Poll.Question != "Where?" {
		t.Fatalf("expected hydrated question, got %q", items[0].LastMessage.Poll.Question)
	}
}

// ---------------------------------------------------------------------------
// JoinUserToDefaults (mig 069)
// ---------------------------------------------------------------------------

// TestJoinUserToDefaults_NATS_PerChatEvent asserts the welcome-flow service
// publishes one `chat_member_added` event per chat the user was newly added
// to. The audience for each event is the full member list of that chat —
// already-online members must be reconciled.
func TestJoinUserToDefaults_NATS_PerChatEvent(t *testing.T) {
	userID := uuid.New()
	chatA, chatB := uuid.New(), uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		joinUserToDefaultsFn: func(_ context.Context, uid uuid.UUID) ([]uuid.UUID, error) {
			if uid != userID {
				t.Fatalf("JoinUserToDefaults: unexpected user id %s", uid)
			}
			return []uuid.UUID{chatA, chatB}, nil
		},
		getMemberIDsFn: func(_ context.Context, chatID uuid.UUID) ([]string, error) {
			return []string{userID.String(), uuid.NewString()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	added, err := svc.JoinUserToDefaults(context.Background(), userID)
	if err != nil {
		t.Fatalf("JoinUserToDefaults: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("expected 2 chats added, got %d", len(added))
	}

	events := rec.FindByEvent("chat_member_added")
	if len(events) != 2 {
		t.Fatalf("expected 2 chat_member_added events, got %d", len(events))
	}
	for _, ev := range events {
		if ev.SenderID != userID.String() {
			t.Fatalf("welcome-flow event sender must be the user themselves, got %s", ev.SenderID)
		}
	}
}

// TestJoinUserToDefaults_NoDefaults_NoNATS guards the empty path: if the
// store has no defaults to insert, the service must not publish anything.
func TestJoinUserToDefaults_NoDefaults_NoNATS(t *testing.T) {
	rec := &RecordingPublisher{}
	cs := &mockChatStore{
		joinUserToDefaultsFn: func(_ context.Context, _ uuid.UUID) ([]uuid.UUID, error) {
			return nil, nil
		},
	}
	svc := newTestChatService(cs, rec)
	added, err := svc.JoinUserToDefaults(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("JoinUserToDefaults: %v", err)
	}
	if len(added) != 0 {
		t.Fatalf("expected 0 chats added, got %d", len(added))
	}
	if got := rec.FindByEvent("chat_member_added"); len(got) != 0 {
		t.Fatalf("expected no NATS events, got %d", len(got))
	}
}

// suppress unused import
var _ = fmt.Sprintf
