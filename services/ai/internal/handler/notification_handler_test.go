// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/ai/internal/client"
	"github.com/mst-corp/orbit/services/ai/internal/model"
	"github.com/mst-corp/orbit/services/ai/internal/service"
	"github.com/mst-corp/orbit/services/ai/internal/store"
)

// ---------------------------------------------------------------------------
// Mock notification store
// ---------------------------------------------------------------------------

type mockNotificationStore struct {
	saveFeedbackFn func(ctx context.Context, userID, messageID, classified, override string) error
	getStatsFn     func(ctx context.Context, userID string, days int) (*model.NotificationStatsResponse, error)
}

func (m *mockNotificationStore) SaveFeedback(ctx context.Context, userID, messageID, classified, override string) error {
	if m.saveFeedbackFn != nil {
		return m.saveFeedbackFn(ctx, userID, messageID, classified, override)
	}
	return nil
}

func (m *mockNotificationStore) GetStats(ctx context.Context, userID string, days int) (*model.NotificationStatsResponse, error) {
	if m.getStatsFn != nil {
		return m.getStatsFn(ctx, userID, days)
	}
	return &model.NotificationStatsResponse{PerPriority: make(map[string]model.PriorityStats)}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newNotifApp(t *testing.T, ns store.NotificationStore) *fiber.App {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	anthropicClient := client.NewAnthropicClient("test-key", "test-model", slog.Default())

	svc := service.NewAIService(service.AIServiceConfig{
		Anthropic:      anthropicClient,
		ClassifyClient: anthropicClient,
		Redis:          rdb,
		Logger:         slog.Default(),
	})

	app := fiber.New()
	h := NewNotificationHandler(svc, ns, slog.Default())
	h.Register(app)
	return app
}

func doNotifReq(t *testing.T, app *fiber.App, method, path, body string, userID string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Tests: classify-notification
// ---------------------------------------------------------------------------

func TestClassifyNotification_HappyPath_RuleBased(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)
	userID := uuid.New().String()

	body := `{
		"sender_id": "` + uuid.New().String() + `",
		"sender_role": "admin",
		"chat_type": "direct",
		"message_text": "Hey, need you now"
	}`

	resp := doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result model.ClassifyNotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Priority != "urgent" {
		t.Errorf("expected priority urgent, got %s", result.Priority)
	}
	if result.Source != "rule" {
		t.Errorf("expected source rule, got %s", result.Source)
	}
}

func TestClassifyNotification_BotGroupLow(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)
	userID := uuid.New().String()

	body := `{
		"sender_id": "` + uuid.New().String() + `",
		"sender_role": "bot",
		"chat_type": "group",
		"message_text": "Build #42 succeeded",
		"has_mention": false
	}`

	resp := doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result model.ClassifyNotificationResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Priority != "low" {
		t.Errorf("expected low, got %s", result.Priority)
	}
}

func TestClassifyNotification_MentionImportant(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)
	userID := uuid.New().String()

	body := `{
		"sender_id": "` + uuid.New().String() + `",
		"sender_role": "member",
		"chat_type": "group",
		"message_text": "Hey @you check this",
		"has_mention": true
	}`

	resp := doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result model.ClassifyNotificationResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Priority != "important" {
		t.Errorf("expected important, got %s", result.Priority)
	}
}

func TestClassifyNotification_NoUserID(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)

	body := `{
		"sender_id": "` + uuid.New().String() + `",
		"sender_role": "member",
		"chat_type": "direct",
		"message_text": "hello"
	}`

	resp := doNotifReq(t, app, "POST", "/ai/classify-notification", body, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestClassifyNotification_ValidationFail(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)
	userID := uuid.New().String()

	// Missing message_text
	body := `{"sender_id": "` + uuid.New().String() + `", "sender_role": "member", "chat_type": "direct"}`
	resp := doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for missing message_text, got %d", resp.StatusCode)
	}

	// Invalid sender_role
	body = `{"sender_id": "` + uuid.New().String() + `", "sender_role": "hacker", "chat_type": "direct", "message_text": "hi"}`
	resp = doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid role, got %d", resp.StatusCode)
	}

	// Invalid sender_id (not UUID)
	body = `{"sender_id": "not-a-uuid", "sender_role": "member", "chat_type": "direct", "message_text": "hi"}`
	resp = doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for bad sender_id, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Tests: feedback
// ---------------------------------------------------------------------------

func TestNotificationFeedback_HappyPath(t *testing.T) {
	var saved bool
	ns := &mockNotificationStore{
		saveFeedbackFn: func(ctx context.Context, userID, messageID, classified, override string) error {
			saved = true
			return nil
		},
	}
	app := newNotifApp(t, ns)
	userID := uuid.New().String()

	body := `{
		"message_id": "` + uuid.New().String() + `",
		"classified_priority": "normal",
		"user_override_priority": "important"
	}`

	resp := doNotifReq(t, app, "POST", "/ai/notification-priority/feedback", body, userID)
	if resp.StatusCode != 204 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 204, got %d: %s", resp.StatusCode, string(b))
	}
	if !saved {
		t.Error("expected SaveFeedback to be called")
	}
}

func TestNotificationFeedback_NoAuth(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)

	body := `{"message_id": "` + uuid.New().String() + `", "classified_priority": "normal", "user_override_priority": "low"}`
	resp := doNotifReq(t, app, "POST", "/ai/notification-priority/feedback", body, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestNotificationFeedback_ValidationFail(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)
	userID := uuid.New().String()

	// Invalid priority
	body := `{"message_id": "` + uuid.New().String() + `", "classified_priority": "normal", "user_override_priority": "invalid"}`
	resp := doNotifReq(t, app, "POST", "/ai/notification-priority/feedback", body, userID)
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for invalid priority, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Tests: stats
// ---------------------------------------------------------------------------

func TestNotificationStats_HappyPath(t *testing.T) {
	ns := &mockNotificationStore{
		getStatsFn: func(ctx context.Context, userID string, days int) (*model.NotificationStatsResponse, error) {
			return &model.NotificationStatsResponse{
				TotalClassified: 100,
				TotalOverridden: 5,
				MatchRate:       0.95,
				PerPriority:     map[string]model.PriorityStats{"normal": {Classified: 80, Overridden: 3}},
			}, nil
		},
	}
	app := newNotifApp(t, ns)
	userID := uuid.New().String()

	resp := doNotifReq(t, app, "GET", "/ai/notification-priority/stats", "", userID)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var stats model.NotificationStatsResponse
	json.NewDecoder(resp.Body).Decode(&stats)
	if stats.TotalClassified != 100 {
		t.Errorf("expected 100, got %d", stats.TotalClassified)
	}
}

func TestNotificationStats_NoAuth(t *testing.T) {
	ns := &mockNotificationStore{}
	app := newNotifApp(t, ns)

	resp := doNotifReq(t, app, "GET", "/ai/notification-priority/stats", "", "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Tests: AI fallback (Claude returns classification)
// ---------------------------------------------------------------------------

func TestClassifyNotification_AIFallback(t *testing.T) {
	// When no rule matches, service calls Claude. We mock Claude with httptest.
	anthropicMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
			"content": [{"type":"text","text":"{\"priority\":\"important\",\"reasoning\":\"Budget discussion needs attention\"}"}],
			"usage": {"input_tokens": 50, "output_tokens": 20}
		}`))
	}))
	defer anthropicMock.Close()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	classifyClient := client.NewAnthropicClient("test-key", "claude-3-haiku-20240307", slog.Default())
	classifyClient.SetBaseURL(anthropicMock.URL)

	svc := service.NewAIService(service.AIServiceConfig{
		Anthropic:      client.NewAnthropicClient("test-key", "test-model", slog.Default()),
		ClassifyClient: classifyClient,
		Redis:          rdb,
		Logger:         slog.Default(),
	})

	ns := &mockNotificationStore{}
	app := fiber.New()
	h := NewNotificationHandler(svc, ns, slog.Default())
	h.Register(app)

	userID := uuid.New().String()
	// member + group + no mention/reply → no rule match → AI fallback
	body := `{
		"sender_id": "` + uuid.New().String() + `",
		"sender_role": "member",
		"chat_type": "group",
		"message_text": "We need to discuss the Q4 budget allocation"
	}`

	resp := doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result model.ClassifyNotificationResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Priority != "important" {
		t.Errorf("expected important from AI, got %s", result.Priority)
	}
	if result.Source != "ai" {
		t.Errorf("expected source ai, got %s", result.Source)
	}
}

// Test AI failure → fail open to normal
func TestClassifyNotification_AIFailure_FailOpen(t *testing.T) {
	anthropicMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer anthropicMock.Close()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	classifyClient := client.NewAnthropicClient("test-key", "claude-3-haiku-20240307", slog.Default())
	classifyClient.SetBaseURL(anthropicMock.URL)

	svc := service.NewAIService(service.AIServiceConfig{
		Anthropic:      client.NewAnthropicClient("test-key", "test-model", slog.Default()),
		ClassifyClient: classifyClient,
		Redis:          rdb,
		Logger:         slog.Default(),
	})

	ns := &mockNotificationStore{}
	app := fiber.New()
	h := NewNotificationHandler(svc, ns, slog.Default())
	h.Register(app)

	userID := uuid.New().String()
	body := `{
		"sender_id": "` + uuid.New().String() + `",
		"sender_role": "member",
		"chat_type": "group",
		"message_text": "Some random message"
	}`

	resp := doNotifReq(t, app, "POST", "/ai/classify-notification", body, userID)
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
	}

	var result model.ClassifyNotificationResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Priority != "normal" {
		t.Errorf("expected normal on AI failure, got %s", result.Priority)
	}
}
