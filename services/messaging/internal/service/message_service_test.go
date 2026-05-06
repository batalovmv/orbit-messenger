// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"

	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestMessageService(ms *mockMessageStore, cs *mockChatStore, rec *RecordingPublisher, rdb *redis.Client) *MessageService {
	return NewMessageService(ms, cs, nil, rec, rdb)
}

func msgAssertAppError(t *testing.T, err error, wantStatus int) {
	t.Helper()
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError with status %d, got %T: %v", wantStatus, err, err)
	}
	if appErr.Status != wantStatus {
		t.Fatalf("expected status %d, got %d: %s", wantStatus, appErr.Status, appErr.Message)
	}
}

func groupChatStore(chatID uuid.UUID) *mockChatStore {
	return &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "Group"
			return &model.Chat{ID: chatID, Type: "group", Name: &name, DefaultPermissions: 15}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: -1}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{uuid.New().String()}, nil
		},
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
}

// ---------------------------------------------------------------------------
// Channel anonymous posting
// ---------------------------------------------------------------------------
// @Mention events
// ---------------------------------------------------------------------------

func TestSendMessage_MentionEntity_PublishesMentionEvent(t *testing.T) {
	chatID := uuid.New()
	senderID := uuid.New()
	mentionedUserID := uuid.New()
	rec := &RecordingPublisher{}

	cs := groupChatStore(chatID)
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "Hey @someone"
			return &model.Message{ID: uuid.New(), ChatID: chatID, SenderID: &senderID, Content: &content}, nil
		},
	}

	entities, _ := json.Marshal([]map[string]string{
		{"type": "mention", "user_id": mentionedUserID.String()},
	})

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, senderID, "Hey @someone", entities, nil, "text")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	mentionEvents := rec.FindByEvent("mention")
	if len(mentionEvents) != 1 {
		t.Fatalf("expected 1 mention event, got %d", len(mentionEvents))
	}
	if mentionEvents[0].Subject != "orbit.user."+mentionedUserID.String()+".mention" {
		t.Errorf("mention subject should target user, got %q", mentionEvents[0].Subject)
	}
}

// Smart Notifications Chunk 3: verifies the publisher carries SenderRole
// classifier hint. Admin senders get "admin" → gateway rule classifier
// elevates DM pushes to "urgent".
func TestSendMessage_AdminSender_PublishesAdminHint(t *testing.T) {
	chatID := uuid.New()
	senderID := uuid.New()
	rec := &RecordingPublisher{}

	cs := groupChatStore(chatID)
	cs.getUserClassifierHintFn = func(_ context.Context, uid uuid.UUID) (string, error) {
		if uid == senderID {
			return "admin", nil
		}
		return "member", nil
	}
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "hi"
			return &model.Message{ID: uuid.New(), ChatID: chatID, SenderID: &senderID, Content: &content}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	if _, err := svc.SendMessage(context.Background(), chatID, senderID, "hi", nil, nil, "text"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	newMsgEvents := rec.FindByEvent("new_message")
	if len(newMsgEvents) != 1 {
		t.Fatalf("expected 1 new_message event, got %d", len(newMsgEvents))
	}
	if got := newMsgEvents[0].Hints.SenderRole; got != "admin" {
		t.Errorf("expected SenderRole=admin, got %q", got)
	}
}

func TestSendMessage_NoEntities_NoMentionEvents(t *testing.T) {
	chatID := uuid.New()
	senderID := uuid.New()
	rec := &RecordingPublisher{}

	cs := groupChatStore(chatID)
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "Hello world"
			return &model.Message{ID: uuid.New(), ChatID: chatID, SenderID: &senderID, Content: &content}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, senderID, "Hello world", nil, nil, "text")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	mentionEvents := rec.FindByEvent("mention")
	if len(mentionEvents) != 0 {
		t.Fatalf("expected 0 mention events, got %d", len(mentionEvents))
	}
}

// ---------------------------------------------------------------------------
// Slow mode (with miniredis)
// ---------------------------------------------------------------------------

func TestSendMessage_SlowMode_SecondMessageBlocked(t *testing.T) {
	chatID := uuid.New()
	senderID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "Slow"
			return &model.Chat{ID: chatID, Type: "group", Name: &name, DefaultPermissions: 15, SlowModeSeconds: 30}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: -1}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{senderID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "msg"
			return &model.Message{ID: uuid.New(), ChatID: chatID, SenderID: &senderID, Content: &content}, nil
		},
	}

	// Use a real miniredis for slow mode testing
	mr := newMiniredis(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	svc := newTestMessageService(ms, cs, rec, rdb)

	// First message should succeed
	_, err := svc.SendMessage(context.Background(), chatID, senderID, "first", nil, nil, "text")
	if err != nil {
		t.Fatalf("first message should pass: %v", err)
	}

	// Second message should be rate-limited
	_, err = svc.SendMessage(context.Background(), chatID, senderID, "second", nil, nil, "text")
	msgAssertAppError(t, err, 429)
}

func TestSendMessage_SlowMode_AdminBypass(t *testing.T) {
	chatID := uuid.New()
	adminID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "Slow"
			return &model.Chat{ID: chatID, Type: "group", Name: &name, DefaultPermissions: 15, SlowModeSeconds: 30}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{adminID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "msg"
			return &model.Message{ID: uuid.New(), ChatID: chatID, SenderID: &adminID, Content: &content}, nil
		},
	}

	mr := newMiniredis(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	svc := newTestMessageService(ms, cs, rec, rdb)

	// Both messages should succeed for admin
	_, err := svc.SendMessage(context.Background(), chatID, adminID, "first", nil, nil, "text")
	if err != nil {
		t.Fatalf("admin first message: %v", err)
	}
	_, err = svc.SendMessage(context.Background(), chatID, adminID, "second", nil, nil, "text")
	if err != nil {
		t.Fatalf("admin should bypass slow mode: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Member with restricted permissions
// ---------------------------------------------------------------------------

func TestSendMessage_MemberWithoutSendPerm_Forbidden(t *testing.T) {
	chatID := uuid.New()
	memberID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "Group"
			return &model.Chat{ID: chatID, Type: "group", Name: &name, DefaultPermissions: 15}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			// Custom perms with CanSendMessages removed (254)
			return &model.ChatMember{Role: "member", Permissions: permissions.AllPermissions &^ permissions.CanSendMessages}, nil
		},
	}
	ms := &mockMessageStore{}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, memberID, "test", nil, nil, "text")
	msgAssertAppError(t, err, 403)
}

func TestSendMessage_DirectChatMember_AllowedWithDirectDefaults(t *testing.T) {
	chatID := uuid.New()
	memberID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return &model.Chat{
				ID:                 chatID,
				Type:               "direct",
				DefaultPermissions: permissions.DefaultDirectPermissions,
			}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: permissions.PermissionsUnset}, nil
		},
		getMembersFn: func(_ context.Context, _ uuid.UUID, _ string, _ int) ([]model.ChatMember, string, bool, error) {
			return []model.ChatMember{
				{UserID: memberID},
				{UserID: uuid.New()},
			}, "", false, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{memberID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "dm text"
			return &model.Message{ID: uuid.New(), ChatID: chatID, SenderID: &memberID, Content: &content}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, memberID, "dm text", nil, nil, "text")
	if err != nil {
		t.Fatalf("direct chat member should be able to send text with direct defaults: %v", err)
	}
}

func TestSendMediaMessage_DirectChatMember_AllowedWithDirectDefaults(t *testing.T) {
	chatID := uuid.New()
	memberID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return &model.Chat{
				ID:                 chatID,
				Type:               "direct",
				DefaultPermissions: permissions.DefaultDirectPermissions,
			}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: permissions.PermissionsUnset}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{memberID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, messageID uuid.UUID) (*model.Message, error) {
			content := "dm media"
			return &model.Message{ID: messageID, ChatID: chatID, SenderID: &memberID, Content: &content, Type: "photo"}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMediaMessage(
		context.Background(),
		chatID,
		memberID,
		"dm media",
		nil,
		nil,
		"photo",
		[]uuid.UUID{uuid.New()},
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("direct chat member should be able to send media with direct defaults: %v", err)
	}
}

func TestViewOneTimeMessage_NotFound(t *testing.T) {
	rec := &RecordingPublisher{}
	svc := newTestMessageService(&mockMessageStore{
		markOneTimeViewedFn: func(_ context.Context, _, _ uuid.UUID) (*model.Message, error) {
			return nil, pgx.ErrNoRows
		},
	}, &mockChatStore{}, rec, nil)

	_, err := svc.ViewOneTimeMessage(context.Background(), uuid.New(), uuid.New())
	msgAssertAppError(t, err, 404)
}

func TestViewOneTimeMessage_Forbidden(t *testing.T) {
	rec := &RecordingPublisher{}
	svc := newTestMessageService(&mockMessageStore{
		markOneTimeViewedFn: func(_ context.Context, _, _ uuid.UUID) (*model.Message, error) {
			return nil, store.ErrMessageForbidden
		},
	}, &mockChatStore{}, rec, nil)

	_, err := svc.ViewOneTimeMessage(context.Background(), uuid.New(), uuid.New())
	msgAssertAppError(t, err, 403)
}

func TestViewOneTimeMessage_NotOneTime(t *testing.T) {
	rec := &RecordingPublisher{}
	svc := newTestMessageService(&mockMessageStore{
		markOneTimeViewedFn: func(_ context.Context, _, _ uuid.UUID) (*model.Message, error) {
			return nil, store.ErrMessageNotOneTime
		},
	}, &mockChatStore{}, rec, nil)

	_, err := svc.ViewOneTimeMessage(context.Background(), uuid.New(), uuid.New())
	msgAssertAppError(t, err, 400)
}

func TestViewOneTimeMessage_SuccessPublishesUpdate(t *testing.T) {
	chatID := uuid.New()
	messageID := uuid.New()
	viewerID := uuid.New()
	viewedAt := time.Now()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberIDsFn: func(_ context.Context, id uuid.UUID) ([]string, error) {
			if id != chatID {
				t.Fatalf("unexpected chat id: %s", id)
			}
			return []string{viewerID.String()}, nil
		},
	}

	ms := &mockMessageStore{
		markOneTimeViewedFn: func(_ context.Context, msgID, userID uuid.UUID) (*model.Message, error) {
			if msgID != messageID {
				t.Fatalf("unexpected message id: %s", msgID)
			}
			if userID != viewerID {
				t.Fatalf("unexpected viewer id: %s", userID)
			}
			return &model.Message{
				ID:         messageID,
				ChatID:     chatID,
				SenderID:   &viewerID,
				Type:       "photo",
				IsOneTime:  true,
				ViewedAt:   &viewedAt,
				ViewedBy:   &viewerID,
				CreatedAt:  viewedAt,
				SenderName: "Viewer",
			}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	msg, err := svc.ViewOneTimeMessage(context.Background(), messageID, viewerID)
	if err != nil {
		t.Fatalf("ViewOneTimeMessage: %v", err)
	}
	if msg == nil || msg.ViewedAt == nil {
		t.Fatalf("expected viewed message with viewed_at, got %+v", msg)
	}

	events := rec.FindByEvent("message_updated")
	if len(events) != 1 {
		t.Fatalf("expected 1 message_updated event, got %d", len(events))
	}
	if events[0].Subject != "orbit.chat."+chatID.String()+".message.updated" {
		t.Fatalf("unexpected subject: %s", events[0].Subject)
	}
	updated, ok := events[0].Data.(*model.Message)
	if !ok {
		t.Fatalf("event data should be *model.Message, got %T", events[0].Data)
	}
	if updated.ViewedAt == nil || updated.ViewedBy == nil || *updated.ViewedBy != viewerID {
		t.Fatalf("expected viewed metadata in event, got %+v", updated)
	}
}

// ---------------------------------------------------------------------------
// PinMessage — permission-based
// ---------------------------------------------------------------------------

func TestPinMessage_MemberWithPermission_Allowed(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	msgID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: permissions.CanPinMessages}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			return defaultGroupChat(chatID), nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{userID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		pinFn: func(_ context.Context, _, _ uuid.UUID) error { return nil },
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	err := svc.PinMessage(context.Background(), chatID, msgID, userID)
	if err != nil {
		t.Fatalf("member with CanPinMessages should be able to pin: %v", err)
	}
}

func TestPinMessage_MemberWithoutPermission_Forbidden(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	msgID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			// Member has all permissions EXCEPT CanPinMessages
			return &model.ChatMember{Role: "member", Permissions: permissions.AllPermissions &^ permissions.CanPinMessages}, nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			chat := defaultGroupChat(chatID)
			// Set default perms without pin too, so no fallback
			chat.DefaultPermissions = permissions.AllPermissions &^ permissions.CanPinMessages
			return chat, nil
		},
	}
	ms := &mockMessageStore{}

	svc := newTestMessageService(ms, cs, rec, nil)
	err := svc.PinMessage(context.Background(), chatID, msgID, userID)
	msgAssertAppError(t, err, 403)
}

func TestUnpinAll_PublishesNATSEvent(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	memberIDs := []string{userID.String(), uuid.New().String()}
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return memberIDs, nil
		},
	}
	ms := &mockMessageStore{
		unpinAllFn: func(_ context.Context, gotChatID uuid.UUID) error {
			if gotChatID != chatID {
				t.Fatalf("unexpected chat id: %s", gotChatID)
			}
			return nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	if err := svc.UnpinAll(context.Background(), chatID, userID); err != nil {
		t.Fatalf("UnpinAll: %v", err)
	}

	events := rec.FindByEvent("unpin_all")
	if len(events) != 1 {
		t.Fatalf("expected 1 unpin_all event, got %d", len(events))
	}
	if events[0].Subject != "orbit.chat."+chatID.String()+".message.updated" {
		t.Fatalf("unexpected subject: %s", events[0].Subject)
	}
	if events[0].SenderID != userID.String() {
		t.Fatalf("unexpected sender id: %s", events[0].SenderID)
	}
	if len(events[0].MemberIDs) != len(memberIDs) {
		t.Fatalf("unexpected member ids count: %d", len(events[0].MemberIDs))
	}

	payload, ok := events[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected payload type: %T", events[0].Data)
	}
	if payload["chat_id"] != chatID.String() {
		t.Fatalf("unexpected chat_id payload: %v", payload["chat_id"])
	}
}

// ---------------------------------------------------------------------------
// EditMessage — only author
// ---------------------------------------------------------------------------

func TestEditMessage_NonAuthor_Forbidden(t *testing.T) {
	authorID := uuid.New()
	otherID := uuid.New()
	msgID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (bool, string, error) {
			if gotChatID != chatID || gotUserID != otherID {
				t.Fatalf("unexpected membership check: chat=%s user=%s", gotChatID, gotUserID)
			}
			return true, "member", nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "original"
			return &model.Message{ID: msgID, ChatID: chatID, SenderID: &authorID, Content: &content}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.EditMessage(context.Background(), msgID, otherID, "hacked", nil)
	msgAssertAppError(t, err, 403)
}

func TestEditMessage_ExMemberCannotEditOwnMessage(t *testing.T) {
	authorID := uuid.New()
	msgID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (bool, string, error) {
			if gotChatID != chatID || gotUserID != authorID {
				t.Fatalf("unexpected membership check: chat=%s user=%s", gotChatID, gotUserID)
			}
			return false, "", nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "original"
			return &model.Message{ID: msgID, ChatID: chatID, SenderID: &authorID, Content: &content}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.EditMessage(context.Background(), msgID, authorID, "edited", nil)
	msgAssertAppError(t, err, 403)
}

// ---------------------------------------------------------------------------
// DeleteMessage — author or admin
// ---------------------------------------------------------------------------

func TestDeleteMessage_AdminCanDelete(t *testing.T) {
	authorID := uuid.New()
	adminID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (bool, string, error) {
			if gotChatID != chatID || gotUserID != adminID {
				t.Fatalf("unexpected membership check: chat=%s user=%s", gotChatID, gotUserID)
			}
			return true, "admin", nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{adminID.String(), authorID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
		softDeleteAuthorizedFn: func(_ context.Context, mid, uid uuid.UUID) (uuid.UUID, int, error) {
			return chatID, 1, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	err := svc.DeleteMessage(context.Background(), msgID, adminID)
	if err != nil {
		t.Fatalf("admin should be able to delete any message: %v", err)
	}
}

func TestDeleteMessage_MemberCannotDeleteOthers(t *testing.T) {
	memberID := uuid.New()
	msgID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (bool, string, error) {
			if gotChatID != chatID || gotUserID != memberID {
				t.Fatalf("unexpected membership check: chat=%s user=%s", gotChatID, gotUserID)
			}
			return true, "member", nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
		softDeleteAuthorizedFn: func(_ context.Context, _, _ uuid.UUID) (uuid.UUID, int, error) {
			return uuid.Nil, 0, fmt.Errorf("forbidden")
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	err := svc.DeleteMessage(context.Background(), msgID, memberID)
	msgAssertAppError(t, err, 403)
}

func TestDeleteMessage_ExMemberCannotDeleteOwnMessage(t *testing.T) {
	userID := uuid.New()
	msgID := uuid.New()
	chatID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, gotChatID, gotUserID uuid.UUID) (bool, string, error) {
			if gotChatID != chatID || gotUserID != userID {
				t.Fatalf("unexpected membership check: chat=%s user=%s", gotChatID, gotUserID)
			}
			return false, "", nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID, SenderID: &userID}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	err := svc.DeleteMessage(context.Background(), msgID, userID)
	msgAssertAppError(t, err, 403)
}

// ---------------------------------------------------------------------------
// MarkRead — read-sync NATS publish
// ---------------------------------------------------------------------------

// TestMarkRead_PublishesReadSyncToOriginatingUser locks the wire format of the
// new orbit.user.<userID>.read_sync event so future refactors can't silently
// drop a field the gateway/frontend depend on (chat_id, last_read_seq_num,
// unread_count, origin_session_id).
func TestMarkRead_PublishesReadSyncToOriginatingUser(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	msgID := uuid.New()
	const sessionID = "tab-abc-123"

	rec := &RecordingPublisher{}
	cs := groupChatStore(chatID)
	ms := &mockMessageStore{
		updateReadPointerFn: func(_ context.Context, _, _, _ uuid.UUID) error { return nil },
		getReadStateFn: func(_ context.Context, gotChat, gotUser uuid.UUID) (int64, int64, error) {
			if gotChat != chatID || gotUser != userID {
				t.Fatalf("GetReadState called with wrong ids: chat=%s user=%s", gotChat, gotUser)
			}
			return 42, 0, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	if err := svc.MarkRead(context.Background(), chatID, userID, msgID, sessionID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	syncEvents := rec.FindByEvent("read_sync")
	if len(syncEvents) != 1 {
		t.Fatalf("expected 1 read_sync event, got %d", len(syncEvents))
	}
	ev := syncEvents[0]

	wantSubject := "orbit.user." + userID.String() + ".read_sync"
	if ev.Subject != wantSubject {
		t.Errorf("subject: got %q, want %q", ev.Subject, wantSubject)
	}
	if len(ev.MemberIDs) != 1 || ev.MemberIDs[0] != userID.String() {
		t.Errorf("MemberIDs should contain only the originating user, got %v", ev.MemberIDs)
	}
	if ev.SenderID != userID.String() {
		t.Errorf("SenderID: got %q, want %q", ev.SenderID, userID.String())
	}

	payload, ok := ev.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("payload is not map: %T", ev.Data)
	}
	if got := payload["chat_id"]; got != chatID.String() {
		t.Errorf("payload.chat_id: got %v, want %s", got, chatID)
	}
	if got := payload["last_read_message_id"]; got != msgID.String() {
		t.Errorf("payload.last_read_message_id: got %v, want %s", got, msgID)
	}
	if got, want := payload["last_read_seq_num"], int64(42); got != want {
		t.Errorf("payload.last_read_seq_num: got %v, want %d", got, want)
	}
	if got, want := payload["unread_count"], int64(0); got != want {
		t.Errorf("payload.unread_count: got %v, want %d", got, want)
	}
	if got := payload["origin_session_id"]; got != sessionID {
		t.Errorf("payload.origin_session_id: got %v, want %s", got, sessionID)
	}
	if _, has := payload["read_at"]; !has {
		t.Error("payload missing read_at timestamp")
	}

	// Cross-user receipt event must still fire alongside, on the chat-scoped
	// subject. If we ever break this, other members' "Alice read X" indicators
	// will silently stop updating.
	receiptEvents := rec.FindByEvent("messages_read")
	if len(receiptEvents) != 1 {
		t.Fatalf("expected 1 messages_read event, got %d", len(receiptEvents))
	}
	if receiptEvents[0].Subject != "orbit.chat."+chatID.String()+".messages.read" {
		t.Errorf("receipt subject: got %q, want orbit.chat.%s.messages.read",
			receiptEvents[0].Subject, chatID)
	}
}

// TestMarkRead_NotMemberRejected guards the IsMember gate so the new GetReadState
// call cannot leak unread_count to a non-member who slipped past auth.
func TestMarkRead_NotMemberRejected(t *testing.T) {
	chatID := uuid.New()
	userID := uuid.New()
	msgID := uuid.New()

	rec := &RecordingPublisher{}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}
	ms := &mockMessageStore{}

	svc := newTestMessageService(ms, cs, rec, nil)
	err := svc.MarkRead(context.Background(), chatID, userID, msgID, "")
	msgAssertAppError(t, err, 403)

	if len(rec.Events) != 0 {
		t.Errorf("non-member must not trigger any publish, got %d events", len(rec.Events))
	}
}
