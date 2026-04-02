package handler

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

func newPollApp(ps *mockPollStore, ms *mockMessageStore, cs *mockChatStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewPollService(ps, ms, cs, nats, slog.Default())
	h := NewPollHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// --- Vote ---

func TestVote_MissingUserID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/vote",
		bytes.NewBufferString(`{"option_ids":["`+uuid.New().String()+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestVote_InvalidMessageID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/bad-id/poll/vote",
		bytes.NewBufferString(`{"option_ids":["`+uuid.New().String()+`"]}`))
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

func TestVote_EmptyOptionIDs(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/vote",
		bytes.NewBufferString(`{"option_ids":[]}`))
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

func TestVote_InvalidOptionID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/vote",
		bytes.NewBufferString(`{"option_ids":["not-a-uuid"]}`))
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

// --- Unvote ---

func TestUnvote_MissingUserID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/"+uuid.New().String()+"/poll/vote", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestUnvote_InvalidMessageID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/bad-id/poll/vote", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- ClosePoll ---

func TestClosePoll_MissingUserID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/poll/close", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestClosePoll_InvalidMessageID(t *testing.T) {
	app := newPollApp(&mockPollStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/bad-id/poll/close", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
