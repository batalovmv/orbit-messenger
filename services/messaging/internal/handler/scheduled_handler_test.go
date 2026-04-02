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

func newScheduledApp(ss *mockScheduledMessageStore, ms *mockMessageStore, cs *mockChatStore) *fiber.App {
	app := fiber.New()
	nats := service.NewNoopNATSPublisher()
	svc := service.NewScheduledMessageService(ss, ms, nil, cs, nats, slog.Default())
	h := NewScheduledHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// --- ListScheduled ---

func TestListScheduled_MissingUserID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+uuid.New().String()+"/messages/scheduled", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestListScheduled_InvalidChatID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodGet, "/chats/bad-id/messages/scheduled", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- Schedule ---

func TestSchedule_MissingUserID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+uuid.New().String()+"/messages/scheduled",
		bytes.NewBufferString(`{"content":"hello","scheduled_at":"2026-04-03T09:00:00Z"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestSchedule_EmptyContent(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+uuid.New().String()+"/messages/scheduled",
		bytes.NewBufferString(`{"content":"","scheduled_at":"2026-04-03T09:00:00Z"}`))
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

func TestSchedule_MissingScheduledAt(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+uuid.New().String()+"/messages/scheduled",
		bytes.NewBufferString(`{"content":"hello"}`))
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

func TestSchedule_InvalidScheduledAtFormat(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+uuid.New().String()+"/messages/scheduled",
		bytes.NewBufferString(`{"content":"hello","scheduled_at":"not-a-date"}`))
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

func TestSchedule_InvalidChatID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/chats/bad-id/messages/scheduled",
		bytes.NewBufferString(`{"content":"hello","scheduled_at":"2026-04-03T09:00:00Z"}`))
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

// --- Edit ---

func TestEditScheduled_MissingUserID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPatch, "/messages/"+uuid.New().String()+"/scheduled",
		bytes.NewBufferString(`{"content":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestEditScheduled_InvalidMessageID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPatch, "/messages/bad-id/scheduled",
		bytes.NewBufferString(`{"content":"updated"}`))
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

func TestEditScheduled_InvalidScheduledAtFormat(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	sa := "not-a-date"
	req, _ := http.NewRequest(http.MethodPatch, "/messages/"+uuid.New().String()+"/scheduled",
		bytes.NewBufferString(`{"scheduled_at":"`+sa+`"}`))
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

// --- Delete ---

func TestDeleteScheduled_MissingUserID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/"+uuid.New().String()+"/scheduled", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestDeleteScheduled_InvalidMessageID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodDelete, "/messages/bad-id/scheduled", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- SendNow ---

func TestSendNow_MissingUserID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/"+uuid.New().String()+"/scheduled/send-now", nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestSendNow_InvalidMessageID(t *testing.T) {
	app := newScheduledApp(&mockScheduledMessageStore{}, &mockMessageStore{}, &mockChatStore{})
	req, _ := http.NewRequest(http.MethodPost, "/messages/bad-id/scheduled/send-now", nil)
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
