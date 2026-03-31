package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"

	"github.com/redis/go-redis/v9"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newTestMessageService(ms *mockMessageStore, cs *mockChatStore, rec *RecordingPublisher, rdb *redis.Client) *MessageService {
	return NewMessageService(ms, cs, rec, rdb)
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

func TestSendMessage_ChannelNoSignatures_AnonymousSender(t *testing.T) {
	chatID := uuid.New()
	senderID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "News"
			return &model.Chat{ID: chatID, Type: "channel", Name: &name, DefaultPermissions: -1, IsSignatures: false}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
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
			content := "Breaking news"
			return &model.Message{
				ID:       uuid.New(),
				ChatID:   chatID,
				SenderID: &senderID,
				Content:  &content,
				SenderName: "Admin User",
			}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, senderID, "Breaking news", nil, nil, "text")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	events := rec.FindByEvent("new_message")
	if len(events) != 1 {
		t.Fatalf("expected 1 new_message event, got %d", len(events))
	}

	// The data should be a *model.Message with nil SenderID (anonymous)
	msg, ok := events[0].Data.(*model.Message)
	if !ok {
		t.Fatalf("event data should be *model.Message, got %T", events[0].Data)
	}
	if msg.SenderID != nil {
		t.Error("channel without signatures should have nil SenderID in NATS event")
	}
	if msg.SenderName != "" {
		t.Error("channel without signatures should have empty SenderName in NATS event")
	}
}

func TestSendMessage_ChannelWithSignatures_RealSender(t *testing.T) {
	chatID := uuid.New()
	senderID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "News"
			return &model.Chat{ID: chatID, Type: "channel", Name: &name, DefaultPermissions: -1, IsSignatures: true}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "admin", Permissions: permissions.AllPermissions}, nil
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
			content := "Signed post"
			return &model.Message{
				ID:         uuid.New(),
				ChatID:     chatID,
				SenderID:   &senderID,
				Content:    &content,
				SenderName: "Admin",
			}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, senderID, "Signed post", nil, nil, "text")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	events := rec.FindByEvent("new_message")
	if len(events) != 1 {
		t.Fatalf("expected 1 new_message, got %d", len(events))
	}
	msg, ok := events[0].Data.(*model.Message)
	if !ok {
		t.Fatalf("event data should be *model.Message, got %T", events[0].Data)
	}
	if msg.SenderID == nil {
		t.Error("channel with signatures should have real SenderID in NATS event")
	}
}

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
// Channel permission: member cannot send
// ---------------------------------------------------------------------------

func TestSendMessage_ChannelMember_Forbidden(t *testing.T) {
	chatID := uuid.New()
	memberID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "News"
			return &model.Chat{ID: chatID, Type: "channel", Name: &name, DefaultPermissions: -1}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: -1}, nil
		},
	}
	ms := &mockMessageStore{}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, memberID, "test", nil, nil, "text")
	msgAssertAppError(t, err, 403)
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

func TestSendMessage_OwnerCanPostInChannel(t *testing.T) {
	chatID := uuid.New()
	ownerID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "News"
			return &model.Chat{ID: chatID, Type: "channel", Name: &name, DefaultPermissions: -1}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "owner", Permissions: -1}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{ownerID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "owner post"
			return &model.Message{ID: uuid.New(), ChatID: chatID, SenderID: &ownerID, Content: &content}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.SendMessage(context.Background(), chatID, ownerID, "owner post", nil, nil, "text")
	if err != nil {
		t.Fatalf("owner should always be able to post in channel: %v", err)
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

// ---------------------------------------------------------------------------
// EditMessage — only author
// ---------------------------------------------------------------------------

func TestEditMessage_NonAuthor_Forbidden(t *testing.T) {
	authorID := uuid.New()
	otherID := uuid.New()
	msgID := uuid.New()
	rec := &RecordingPublisher{}

	cs := &mockChatStore{}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "original"
			return &model.Message{ID: msgID, SenderID: &authorID, Content: &content}, nil
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	_, err := svc.EditMessage(context.Background(), msgID, otherID, "hacked", nil)
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
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{adminID.String(), authorID.String()}, nil
		},
	}
	ms := &mockMessageStore{
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
	rec := &RecordingPublisher{}

	cs := &mockChatStore{}
	ms := &mockMessageStore{
		softDeleteAuthorizedFn: func(_ context.Context, _, _ uuid.UUID) (uuid.UUID, int, error) {
			return uuid.Nil, 0, fmt.Errorf("forbidden")
		},
	}

	svc := newTestMessageService(ms, cs, rec, nil)
	err := svc.DeleteMessage(context.Background(), msgID, memberID)
	msgAssertAppError(t, err, 403)
}
