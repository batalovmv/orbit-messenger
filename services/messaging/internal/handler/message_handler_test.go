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
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewMessageService(ms, cs, nats)
	h := NewMessageHandler(svc, nil, slog.Default())
	h.Register(app)
	return app
}

// defaultMemberChatStore returns a mockChatStore where the caller is always a member.
func defaultMemberChatStore() *mockChatStore {
	return &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
		getMemberIDsFn: func(_ context.Context, _ uuid.UUID) ([]string, error) {
			return []string{uuid.New().String()}, nil
		},
	}
}

// ---------------------------------------------------------------------------
// SendMessage
// ---------------------------------------------------------------------------

func TestSendMessage_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()

	ms := &mockMessageStore{
		createFn: func(_ context.Context, msg *model.Message) error {
			msg.ID = msgID
			msg.CreatedAt = time.Now()
			return nil
		},
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			content := "Hello world"
			return &model.Message{
				ID:      msgID,
				ChatID:  chatID,
				Content: &content,
				Type:    "text",
			}, nil
		},
	}

	app := newMessageApp(ms, defaultMemberChatStore())
	body := `{"content":"Hello world","type":"text"}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
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

func TestSendMessage_NoAuth(t *testing.T) {
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := `{"content":"Hello"}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

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

// ---------------------------------------------------------------------------
// EditMessage
// ---------------------------------------------------------------------------

func TestEditMessage_HappyPath(t *testing.T) {
	userID := uuid.New()
	msgID := uuid.New()
	chatID := uuid.New()
	original := "original"

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{
				ID:       msgID,
				ChatID:   chatID,
				SenderID: &userID,
				Content:  &original,
				Type:     "text",
			}, nil
		},
		updateFn: func(_ context.Context, _ *model.Message) error {
			return nil
		},
	}
	cs := defaultMemberChatStore()
	cs.getMemberIDsFn = func(_ context.Context, _ uuid.UUID) ([]string, error) {
		return []string{userID.String()}, nil
	}

	app := newMessageApp(ms, cs)
	body := `{"content":"edited content"}`
	req, _ := http.NewRequest(http.MethodPatch, "/messages/"+msgID.String(), bytes.NewBufferString(body))
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

func TestEditMessage_NotAuthor(t *testing.T) {
	callerID := uuid.New()
	authorID := uuid.New()
	msgID := uuid.New()
	chatID := uuid.New()
	content := "message"

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{
				ID:       msgID,
				ChatID:   chatID,
				SenderID: &authorID, // different from callerID
				Content:  &content,
				Type:     "text",
			}, nil
		},
	}

	app := newMessageApp(ms, defaultMemberChatStore())
	body := `{"content":"hacked"}`
	req, _ := http.NewRequest(http.MethodPatch, "/messages/"+msgID.String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", callerID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-author edit, got %d", resp.StatusCode)
	}
}

func TestEditMessage_NoAuth(t *testing.T) {
	msgID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := `{"content":"edited"}`
	req, _ := http.NewRequest(http.MethodPatch, "/messages/"+msgID.String(), bytes.NewBufferString(body))
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
// DeleteMessage
// ---------------------------------------------------------------------------

func TestDeleteMessage_HappyPath(t *testing.T) {
	userID := uuid.New()
	msgID := uuid.New()
	chatID := uuid.New()
	content := "to be deleted"

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{
				ID:       msgID,
				ChatID:   chatID,
				SenderID: &userID,
				Content:  &content,
				Type:     "text",
			}, nil
		},
		softDeleteFn: func(_ context.Context, _ uuid.UUID) error {
			return nil
		},
	}
	cs := defaultMemberChatStore()
	cs.getMemberIDsFn = func(_ context.Context, _ uuid.UUID) ([]string, error) {
		return []string{userID.String()}, nil
	}

	app := newMessageApp(ms, cs)
	req, _ := http.NewRequest(http.MethodDelete, "/messages/"+msgID.String(), nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDeleteMessage_NoAuth(t *testing.T) {
	msgID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodDelete, "/messages/"+msgID.String(), nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// ForwardMessages
// ---------------------------------------------------------------------------

func TestForwardMessages_HappyPath(t *testing.T) {
	userID := uuid.New()
	msgID := uuid.New()
	toChatID := uuid.New()
	content := "forward me"

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return &model.Message{
				ID:       msgID,
				ChatID:   uuid.New(),
				SenderID: &userID,
				Content:  &content,
				Type:     "text",
			}, nil
		},
		createForwardedFn: func(_ context.Context, msgs []model.Message) ([]model.Message, error) {
			result := make([]model.Message, len(msgs))
			for i, m := range msgs {
				m.ID = uuid.New()
				m.CreatedAt = time.Now()
				m.IsForwarded = true
				result[i] = m
			}
			return result, nil
		},
	}
	cs := defaultMemberChatStore()
	cs.getMemberIDsFn = func(_ context.Context, _ uuid.UUID) ([]string, error) {
		return []string{userID.String()}, nil
	}

	app := newMessageApp(ms, cs)
	body := fmt.Sprintf(`{"message_ids":["%s"],"to_chat_id":"%s"}`, msgID.String(), toChatID.String())
	req, _ := http.NewRequest(http.MethodPost, "/messages/forward", bytes.NewBufferString(body))
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

func TestForwardMessages_NoAuth(t *testing.T) {
	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := fmt.Sprintf(`{"message_ids":["%s"],"to_chat_id":"%s"}`, uuid.New(), uuid.New())
	req, _ := http.NewRequest(http.MethodPost, "/messages/forward", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

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

func TestMarkRead_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()

	ms := &mockMessageStore{
		updateReadPointerFn: func(_ context.Context, _, _, _ uuid.UUID) error {
			return nil
		},
	}
	cs := defaultMemberChatStore()
	cs.getMemberIDsFn = func(_ context.Context, _ uuid.UUID) ([]string, error) {
		return []string{userID.String()}, nil
	}

	app := newMessageApp(ms, cs)
	body := fmt.Sprintf(`{"last_read_message_id":"%s"}`, msgID.String())
	req, _ := http.NewRequest(http.MethodPatch, "/chats/"+chatID.String()+"/read", bytes.NewBufferString(body))
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

func TestMarkRead_NoAuth(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := fmt.Sprintf(`{"last_read_message_id":"%s"}`, msgID.String())
	req, _ := http.NewRequest(http.MethodPatch, "/chats/"+chatID.String()+"/read", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

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

func TestListMessages_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	content := "hello"

	ms := &mockMessageStore{
		listByChatFn: func(_ context.Context, _ uuid.UUID, _ string, _ int) ([]model.Message, string, bool, error) {
			return []model.Message{
				{ID: uuid.New(), ChatID: chatID, Content: &content, Type: "text"},
			}, "", false, nil
		},
	}

	app := newMessageApp(ms, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/messages", nil)
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

func TestListMessages_NoAuth(t *testing.T) {
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/messages", nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

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
// PinMessage
// ---------------------------------------------------------------------------

func TestPinMessage_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()

	ms := &mockMessageStore{
		pinFn: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}

	app := newMessageApp(ms, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/chats/%s/pin/%s", chatID, msgID), nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestPinMessage_NoAuth(t *testing.T) {
	chatID := uuid.New()
	msgID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("/chats/%s/pin/%s", chatID, msgID), nil)

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// UnpinMessage
// ---------------------------------------------------------------------------

func TestUnpinMessage_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	msgID := uuid.New()

	ms := &mockMessageStore{
		unpinFn: func(_ context.Context, _, _ uuid.UUID) error { return nil },
	}
	// Use admin role so unpin doesn't need to call GetByID
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
	}

	app := newMessageApp(ms, cs)
	req, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("/chats/%s/pin/%s", chatID, msgID), nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// UnpinAll
// ---------------------------------------------------------------------------

func TestUnpinAll_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()

	ms := &mockMessageStore{
		unpinAllFn: func(_ context.Context, _ uuid.UUID) error { return nil },
	}
	// Use admin role so UnpinAll doesn't need to call GetByID
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _, _ uuid.UUID) (bool, string, error) {
			return true, "admin", nil
		},
	}

	app := newMessageApp(ms, cs)
	req, _ := http.NewRequest(http.MethodDelete, "/chats/"+chatID.String()+"/pin", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// ListPinned
// ---------------------------------------------------------------------------

func TestListPinned_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	content := "pinned msg"

	ms := &mockMessageStore{
		listPinnedFn: func(_ context.Context, _ uuid.UUID) ([]model.Message, error) {
			return []model.Message{
				{ID: uuid.New(), ChatID: chatID, Content: &content, Type: "text"},
			}, nil
		},
	}

	app := newMessageApp(ms, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/pinned", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body := readBody(t, resp)
	if body["messages"] == nil {
		t.Fatal("response missing 'messages' field")
	}
}

// ---------------------------------------------------------------------------
// FindByDate
// ---------------------------------------------------------------------------

func TestFindByDate_HappyPath(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	content := "dated message"

	ms := &mockMessageStore{
		findByChatAndDateFn: func(_ context.Context, _ uuid.UUID, _ time.Time, _ int) ([]model.Message, string, bool, error) {
			return []model.Message{
				{ID: uuid.New(), ChatID: chatID, Content: &content, Type: "text"},
			}, "", false, nil
		},
	}

	app := newMessageApp(ms, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/history?date=2026-01-01T00:00:00Z", nil)
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
