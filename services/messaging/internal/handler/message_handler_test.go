package handler

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// newMessageApp creates a Fiber app wired with a MessageHandler backed by given mock stores.
func newMessageApp(ms *mockMessageStore, cs *mockChatStore) *fiber.App {
	return newRichMessageApp(ms, cs, nil, nil)
}

func newRichMessageApp(ms *mockMessageStore, cs *mockChatStore, ps *mockPollStore, ss *mockScheduledMessageStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	msgSvc := service.NewMessageService(ms, cs, nil, nats, nil) // nil blockedStore + nil redis in tests

	var pollSvc *service.PollService
	if ps != nil {
		pollSvc = service.NewPollService(ps, ms, cs, nats, slog.Default())
	}

	var scheduledSvc *service.ScheduledMessageService
	if ss != nil {
		scheduledSvc = service.NewScheduledMessageService(ss, ms, nil, cs, nats, slog.Default())
	}

	h := NewMessageHandler(msgSvc, pollSvc, scheduledSvc, nil, slog.Default())
	h.Register(app)
	return app
}

// defaultMemberChatStore returns a mockChatStore where the caller is a group member who can send.
func defaultMemberChatStore() *mockChatStore {
	return &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: -1}, nil
		},
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Chat, error) {
			name := "Test Chat"
			return &model.Chat{ID: id, Type: "group", Name: &name, DefaultPermissions: 15}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{uuid.New().String()}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// SendMessage
// ---------------------------------------------------------------------------

func TestSendMessage_EmptyContent(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := `{"content":""}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content, got %d", resp.StatusCode)
	}
}

func TestSendMessage_PollTypeCreatesPoll(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()
	pollID := uuid.New()

	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = msgID
			msg.CreatedAt = time.Now()
			return nil
		},
	}
	ps := &mockPollStore{
		createFn: func(_ context.Context, poll *model.Poll) error {
			poll.ID = pollID
			poll.CreatedAt = time.Now()
			for i := range poll.Options {
				poll.Options[i].ID = uuid.New()
				poll.Options[i].PollID = pollID
			}
			return nil
		},
	}

	app := newRichMessageApp(ms, defaultMemberChatStore(), ps, nil)
	body := `{"type":"poll","question":"Where?","options":["Office","Cafe"]}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for poll message, got %d", resp.StatusCode)
	}
}

func TestSendMessage_ScheduledAtCreatesScheduledMessage(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	scheduledID := uuid.New()
	scheduledAt := "2026-04-03T09:00:00Z"

	ss := &mockScheduledMessageStore{
		createFn: func(_ context.Context, msg *model.ScheduledMessage) error {
			msg.ID = scheduledID
			msg.CreatedAt = time.Now()
			msg.UpdatedAt = msg.CreatedAt
			return nil
		},
	}

	app := newRichMessageApp(&mockMessageStore{}, defaultMemberChatStore(), nil, ss)
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages?scheduled_at="+scheduledAt,
		bytes.NewBufferString(`{"content":"hello later"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for scheduled message, got %d", resp.StatusCode)
	}
}

func TestSendMessage_ScheduledPollCreatesScheduledPollPayload(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	scheduledID := uuid.New()
	scheduledAt := "2026-04-03T09:00:00Z"

	var created *model.ScheduledMessage
	ss := &mockScheduledMessageStore{
		createFn: func(_ context.Context, msg *model.ScheduledMessage) error {
			created = msg
			msg.ID = scheduledID
			msg.CreatedAt = time.Now()
			msg.UpdatedAt = msg.CreatedAt
			return nil
		},
	}

	app := newRichMessageApp(&mockMessageStore{}, defaultMemberChatStore(), nil, ss)
	req, _ := http.NewRequest(
		http.MethodPost,
		"/chats/"+chatID.String()+"/messages?scheduled_at="+scheduledAt,
		bytes.NewBufferString(`{"type":"poll","question":"Where?","options":["Office","Cafe"]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for scheduled poll, got %d", resp.StatusCode)
	}
	if created == nil || created.PollPayload == nil {
		t.Fatalf("expected scheduled poll payload to be stored")
	}
	if created.Type != "poll" {
		t.Fatalf("expected poll type, got %q", created.Type)
	}
	if created.PollPayload.Question != "Where?" {
		t.Fatalf("expected poll question to be stored, got %q", created.PollPayload.Question)
	}
}

func TestSendMessage_ScheduledMediaSupportsReplyAndAttachments(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	replyToID := uuid.New()
	mediaID := uuid.New()
	scheduledAt := "2026-04-03T09:00:00Z"

	var created *model.ScheduledMessage
	ss := &mockScheduledMessageStore{
		createFn: func(_ context.Context, msg *model.ScheduledMessage) error {
			created = msg
			msg.ID = uuid.New()
			msg.CreatedAt = time.Now()
			msg.UpdatedAt = msg.CreatedAt
			return nil
		},
	}

	app := newRichMessageApp(&mockMessageStore{}, defaultMemberChatStore(), nil, ss)
	body := fmt.Sprintf(
		`{"type":"photo","content":"caption","reply_to_id":"%s","media_ids":["%s"],"is_spoiler":true}`,
		replyToID,
		mediaID,
	)
	req, _ := http.NewRequest(
		http.MethodPost,
		"/chats/"+chatID.String()+"/messages?scheduled_at="+scheduledAt,
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 for scheduled media message, got %d", resp.StatusCode)
	}
	if created == nil {
		t.Fatal("expected scheduled message to be created")
	}
	if created.ReplyToID == nil || *created.ReplyToID != replyToID {
		t.Fatalf("expected reply_to_id %s, got %+v", replyToID, created.ReplyToID)
	}
	if len(created.MediaIDs) != 1 || created.MediaIDs[0] != mediaID {
		t.Fatalf("expected media_ids [%s], got %+v", mediaID, created.MediaIDs)
	}
	if !created.IsSpoiler {
		t.Fatal("expected scheduled media message to preserve spoiler flag")
	}
}

// ---------------------------------------------------------------------------
// ForwardMessages
// ---------------------------------------------------------------------------

func TestForwardMessages_EmptyIDs(t *testing.T) {
	userID := uuid.New()
	toChatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := fmt.Sprintf(`{"message_ids":[],"to_chat_id":"%s"}`, toChatID.String())
	req, _ := http.NewRequest(http.MethodPost, "/messages/forward", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty message_ids, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// MarkRead
// ---------------------------------------------------------------------------

func TestMarkRead_InvalidMsgID(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := `{"last_read_message_id":"not-a-uuid"}`
	req, _ := http.NewRequest(http.MethodPatch, "/chats/"+chatID.String()+"/read", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid message ID, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// ListMessages
// ---------------------------------------------------------------------------

func TestListMessages_NotMember(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}

	app := newMessageApp(&mockMessageStore{}, cs)
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/messages", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// SlowMode — nil redis means slow_mode_seconds>0 would panic; verify 0 is safe
// ---------------------------------------------------------------------------

// TestSendMessage_SlowModeZeroNotEnforced verifies that slow_mode_seconds=0
// bypasses Redis entirely (nil redis client is safe).
func TestSendMessage_SlowModeZeroNotEnforced(t *testing.T) {
	memberID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()

	cs := &mockChatStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Chat, error) {
			name := "Team"
			return &model.Chat{ID: chatID, Type: "group", Name: &name, DefaultPermissions: 15, SlowModeSeconds: 0}, nil
		},
		getMemberFn: func(_ context.Context, _, _ uuid.UUID) (*model.ChatMember, error) {
			return &model.ChatMember{Role: "member", Permissions: -1}, nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{memberID.String()}, nil
		},
	}
	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = msgID
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "no slowmode"
			return &model.Message{ID: msgID, ChatID: chatID, Content: &content, Type: "text"}, nil
		},
	}

	app := newMessageApp(ms, cs)
	body := `{"content":"no slowmode","type":"text"}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", memberID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("slow_mode=0 should not block, got %d", resp.StatusCode)
	}
}
