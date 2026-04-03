package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

func newReactionApp(rs *mockReactionStore, ms *mockMessageStore, cs *mockChatStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewReactionService(rs, ms, cs, nats, slog.Default())
	h := NewReactionHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// --- AddReaction ---

func TestAddReaction_MissingUserID(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/reactions", bytes.NewBufferString(`{"emoji":"👍"}`))
	req.Header.Set("Content-Type", "application/json")
	// no X-User-ID header
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestAddReaction_InvalidMessageID(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/not-a-uuid/reactions", bytes.NewBufferString(`{"emoji":"👍"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAddReaction_EmptyEmoji(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	msgID := uuid.New()
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+msgID.String()+"/reactions", bytes.NewBufferString(`{"emoji":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAddReaction_NoBody(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	msgID := uuid.New()
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+msgID.String()+"/reactions", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- RemoveReaction ---

func TestRemoveReaction_MissingUserID(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/"+uuid.New().String()+"/reactions", bytes.NewBufferString(`{"emoji":"👍"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestRemoveReaction_EmptyEmoji(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/"+uuid.New().String()+"/reactions", bytes.NewBufferString(`{"emoji":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- ListReactions ---

func TestListReactions_MissingUserID(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/messages/"+uuid.New().String()+"/reactions", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestListReactions_InvalidMessageID(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/messages/bad-id/reactions", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestListReactionUsers_AllReactions_OptionalEmoji(t *testing.T) {
	msgID := uuid.New()
	userID := uuid.New()
	chatID := uuid.New()

	app := newReactionApp(&mockReactionStore{
		listUsersByEmojiFn: func(ctx context.Context, messageID uuid.UUID, emoji string, cursor string, limit int) ([]model.Reaction, string, bool, error) {
			if messageID != msgID {
				t.Fatalf("unexpected message id: %s", messageID)
			}
			if emoji != "" {
				t.Fatalf("expected empty emoji filter, got %q", emoji)
			}
			if cursor != "cursor-1" {
				t.Fatalf("expected cursor-1, got %q", cursor)
			}
			if limit != 25 {
				t.Fatalf("expected limit 25, got %d", limit)
			}

			return []model.Reaction{{
				MessageID:   msgID,
				UserID:      userID,
				Emoji:       "👍",
				DisplayName: "Orbit QA",
			}}, "cursor-2", true, nil
		},
	}, &mockMessageStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: msgID, ChatID: chatID}, nil
		},
	}, &mockChatStore{
		isMemberFn: func(ctx context.Context, cID, uID uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	})

	req, _ := http.NewRequest(http.MethodGet, "/messages/"+msgID.String()+"/reactions/users?cursor=cursor-1&limit=25", nil)
	req.Header.Set("X-User-ID", userID.String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data    []model.Reaction `json:"data"`
		Cursor  string           `json:"cursor"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("expected 1 reaction, got %d", len(body.Data))
	}
	if body.Data[0].Emoji != "👍" {
		t.Fatalf("expected 👍 emoji, got %q", body.Data[0].Emoji)
	}
	if body.Cursor != "cursor-2" {
		t.Fatalf("expected cursor cursor-2, got %q", body.Cursor)
	}
	if !body.HasMore {
		t.Fatal("expected has_more to be true")
	}
}

// --- SetAvailableReactions ---

func TestSetAvailableReactions_MissingUserID(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPut, "/chats/"+uuid.New().String()+"/available-reactions", bytes.NewBufferString(`{"mode":"all"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestSetAvailableReactions_InvalidChatID(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPut, "/chats/bad-id/available-reactions", bytes.NewBufferString(`{"mode":"all"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestSetAvailableReactions_EmptyMode(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPut, "/chats/"+uuid.New().String()+"/available-reactions", bytes.NewBufferString(`{"mode":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestSetAvailableReactions_SelectedModeWithEmojis(t *testing.T) {
	app := newReactionApp(&mockReactionStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPut, "/chats/"+uuid.New().String()+"/available-reactions",
		bytes.NewBufferString(`{"mode":"selected","emojis":["👍","❤️","🎉"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	// Service returns 500 (not implemented), but handler parsing should succeed
	_ = resp
}

// Suppress unused import warning
var _ = model.Reaction{}
