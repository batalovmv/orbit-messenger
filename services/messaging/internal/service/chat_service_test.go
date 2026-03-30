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
	return NewChatService(cs, rec)
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

func defaultChannelChat(chatID uuid.UUID) *model.Chat {
	name := "Test Channel"
	return &model.Chat{
		ID:                 chatID,
		Type:               "channel",
		Name:               &name,
		DefaultPermissions: 0,
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
		addMemberFn: func(_ context.Context, _, _ uuid.UUID, _ string) error { return nil },
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

func TestCreateDirectChat_SelfDM_Rejected(t *testing.T) {
	userID := uuid.New()
	rec := &RecordingPublisher{}
	cs := &mockChatStore{}

	svc := newTestChatService(cs, rec)
	_, err := svc.CreateDirectChat(context.Background(), userID, userID)
	assertAppError(t, err, 400)
}

func TestCreateChat_ChannelDefaultPerms0(t *testing.T) {
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
		addMemberFn:  func(_ context.Context, _, _ uuid.UUID, _ string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	_, err := svc.CreateChat(context.Background(), ownerID, "channel", "News", "", nil)
	if err != nil {
		t.Fatalf("CreateChat channel: %v", err)
	}
	if createdChat.DefaultPermissions != 0 {
		t.Fatalf("channel default_permissions should be 0, got %d", createdChat.DefaultPermissions)
	}
}

func TestCreateChat_GroupDefaultPerms255(t *testing.T) {
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
		addMemberFn:  func(_ context.Context, _, _ uuid.UUID, _ string) error { return nil },
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String()}, nil
		},
	}

	svc := newTestChatService(cs, rec)
	_, err := svc.CreateChat(context.Background(), ownerID, "group", "My Group", "", nil)
	if err != nil {
		t.Fatalf("CreateChat group: %v", err)
	}
	if createdChat.DefaultPermissions != 255 {
		t.Fatalf("group default_permissions should be 255, got %d", createdChat.DefaultPermissions)
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

// suppress unused import
var _ = fmt.Sprintf
