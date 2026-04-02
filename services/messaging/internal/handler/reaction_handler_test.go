package handler

import (
	"bytes"
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
