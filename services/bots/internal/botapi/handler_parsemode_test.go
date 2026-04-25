// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

// captureMessagingServer is a fake messaging service. It returns a successful
// MessageResponse on every request and stores the most recent decoded body so
// tests can assert on what the bots service forwarded.
type captureMessagingServer struct {
	server *httptest.Server
	mu     sync.Mutex
	last   map[string]any
}

func newCaptureMessagingServer(t *testing.T) *captureMessagingServer {
	t.Helper()
	c := &captureMessagingServer{}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		c.mu.Lock()
		c.last = decoded
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000000","chat_id":"00000000-0000-0000-0000-000000000000","content":"ok","type":"text","sequence_number":1,"created_at":"2024-01-01T00:00:00Z"}`))
	}))
	t.Cleanup(c.server.Close)
	return c
}

func (c *captureMessagingServer) lastBody() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func newParseModeTestApp(t *testing.T, msgURL string) *fiber.App {
	t.Helper()
	msgClient := client.NewMessagingClient(msgURL, "internal-test-token")
	h := &BotAPIHandler{
		svc: &rateLimitBotService{bots: map[string]*model.Bot{
			"bot-pm": {
				ID:       uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
				UserID:   uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"),
				IsActive: true,
			},
		}},
		msgClient: msgClient,
		logger:    nil,
	}
	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	group := app.Group("/bot/:token", TokenAuthMiddleware(h.svc))
	h.Register(group)
	return app
}

func sendMessageJSON(t *testing.T, app *fiber.App, body map[string]any) *http.Response {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-pm/sendMessage", bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func decodeForwardedEntities(t *testing.T, body map[string]any) []OrbitEntity {
	t.Helper()
	if body == nil {
		return nil
	}
	raw, ok := body["entities"]
	if !ok || raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("re-marshal entities: %v", err)
	}
	var ents []OrbitEntity
	if err := json.Unmarshal(encoded, &ents); err != nil {
		t.Fatalf("decode entities: %v", err)
	}
	return ents
}

func TestSendMessage_ParseModeMarkdownV2_ParsesEntities(t *testing.T) {
	cap := newCaptureMessagingServer(t)
	app := newParseModeTestApp(t, cap.server.URL)

	resp := sendMessageJSON(t, app, map[string]any{
		"chat_id":    "33333333-3333-3333-3333-333333333333",
		"text":       "*bold* and `code`",
		"parse_mode": "MarkdownV2",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	body := cap.lastBody()
	if got, want := body["content"], "bold and code"; got != want {
		t.Fatalf("content=%v, want %v", got, want)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityBold", Offset: 0, Length: 4},
		{Type: "MessageEntityCode", Offset: 9, Length: 4},
	}
	got := decodeForwardedEntities(t, body)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entities=%+v, want %+v", got, want)
	}
}

func TestSendMessage_ParseModeHTML_ParsesBoldItalicCode(t *testing.T) {
	cap := newCaptureMessagingServer(t)
	app := newParseModeTestApp(t, cap.server.URL)

	resp := sendMessageJSON(t, app, map[string]any{
		"chat_id":    "33333333-3333-3333-3333-333333333333",
		"text":       "<b>Alert</b> <i>now</i> <code>500</code>",
		"parse_mode": "HTML",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	body := cap.lastBody()
	if got, want := body["content"], "Alert now 500"; got != want {
		t.Fatalf("content=%v, want %v", got, want)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityBold", Offset: 0, Length: 5},
		{Type: "MessageEntityItalic", Offset: 6, Length: 3},
		{Type: "MessageEntityCode", Offset: 10, Length: 3},
	}
	got := decodeForwardedEntities(t, body)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entities=%+v, want %+v", got, want)
	}
}

func TestSendMessage_NoParseMode_PlainText(t *testing.T) {
	cap := newCaptureMessagingServer(t)
	app := newParseModeTestApp(t, cap.server.URL)

	resp := sendMessageJSON(t, app, map[string]any{
		"chat_id": "33333333-3333-3333-3333-333333333333",
		"text":    "*not parsed* hello",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	body := cap.lastBody()
	if got, want := body["content"], "*not parsed* hello"; got != want {
		t.Fatalf("content=%v, want %v", got, want)
	}
	if _, ok := body["entities"]; ok {
		t.Fatalf("entities should be omitted when parse_mode is empty, got %v", body["entities"])
	}
}

func TestSendMessage_InvalidParseMode_400(t *testing.T) {
	cap := newCaptureMessagingServer(t)
	app := newParseModeTestApp(t, cap.server.URL)

	resp := sendMessageJSON(t, app, map[string]any{
		"chat_id":    "33333333-3333-3333-3333-333333333333",
		"text":       "hi",
		"parse_mode": "Markdown", // legacy v1, not supported
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestSendMessage_ExplicitEntities_OverrideParsedFromText(t *testing.T) {
	cap := newCaptureMessagingServer(t)
	app := newParseModeTestApp(t, cap.server.URL)

	resp := sendMessageJSON(t, app, map[string]any{
		"chat_id":    "33333333-3333-3333-3333-333333333333",
		"text":       "*hello*",
		"parse_mode": "MarkdownV2",
		"entities": []map[string]any{
			{"type": "italic", "offset": 0, "length": 7},
		},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	body := cap.lastBody()
	// Text is unchanged (no marker stripping when explicit entities win).
	if got, want := body["content"], "*hello*"; got != want {
		t.Fatalf("content=%v, want %v", got, want)
	}
	want := []OrbitEntity{
		{Type: "MessageEntityItalic", Offset: 0, Length: 7},
	}
	got := decodeForwardedEntities(t, body)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entities=%+v, want %+v", got, want)
	}
}
