// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/redis/go-redis/v9"
)

type rateLimitBotService struct {
	bots map[string]*model.Bot
}

func (s *rateLimitBotService) ValidateToken(ctx context.Context, rawToken string) (*model.Bot, error) {
	if bot, ok := s.bots[rawToken]; ok {
		return bot, nil
	}
	return nil, model.ErrInvalidToken
}

func (s *rateLimitBotService) IsBotInstalled(ctx context.Context, botID, chatID uuid.UUID) (bool, error) {
	return true, nil
}

func (s *rateLimitBotService) CheckBotScope(ctx context.Context, botID, chatID uuid.UUID, requiredScope int64) error {
	return nil
}

func (s *rateLimitBotService) SetWebhook(ctx context.Context, botID uuid.UUID, webhookURL, secretHash *string) (*model.Bot, error) {
	return nil, nil
}

func newRateLimitTestApp(t *testing.T, redisClient *redis.Client, bots map[string]*model.Bot, messageServerURL string) (*fiber.App, *int32) {
	t.Helper()

	var msgClient *client.MessagingClient
	if messageServerURL != "" {
		msgClient = client.NewMessagingClient(messageServerURL, "internal-test-token")
	}

	h := &BotAPIHandler{
		svc:         &rateLimitBotService{bots: bots},
		msgClient:   msgClient,
		redis:       redisClient,
		fileIDCodec: NewFileIDCodec([]byte("rate-limit-test-secret")),
		logger:      nil,
		updateQueue: nil,
	}

	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	group := app.Group("/bot/:token", TokenAuthMiddleware(h.svc))
	h.Register(group)

	return app, nil
}

func newBotAPITestServer(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()

	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000000","chat_id":"00000000-0000-0000-0000-000000000000","content":"ok","type":"text","sequence_number":1,"created_at":"2024-01-01T00:00:00Z"}`))
	}))

	t.Cleanup(server.Close)
	return server, &callCount
}

func doRateLimitRequest(t *testing.T, app *fiber.App, method, path string, body any) *http.Response {
	t.Helper()

	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		payload = encoded
	}

	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func newRedisTestClient(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

func TestRateLimit_SendMessage_AllowsFirst30AndRejects31st(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, callCount := newBotAPITestServer(t)
	botID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	botUserID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-token-a": {ID: botID, UserID: botUserID, IsActive: true},
	}, server.URL)

	body := map[string]any{
		"chat_id": "33333333-3333-3333-3333-333333333333",
		"text":    "hello",
	}

	for i := 0; i < botAPIRateLimitPerSec; i++ {
		resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-token-a/sendMessage", body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-token-a/sendMessage", body)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("31st request: expected 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got == "" || got == "0" {
		t.Fatalf("expected Retry-After header on rate-limited response, got %q", got)
	}
	if got := atomic.LoadInt32(callCount); got != botAPIRateLimitPerSec {
		t.Fatalf("expected %d downstream message calls, got %d", botAPIRateLimitPerSec, got)
	}
}

func TestRateLimit_AnswerCallbackQuery_AllowsFirst30AndRejects31st(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	_, _ = newBotAPITestServer(t)
	botID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-token-b": {ID: botID, UserID: uuid.MustParse("55555555-5555-5555-5555-555555555555"), IsActive: true},
	}, "")

	body := map[string]any{
		"callback_query_id": "cbq-1",
		"text":              "ack",
		"show_alert":        false,
	}

	for i := 0; i < botAPIRateLimitPerSec; i++ {
		resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-token-b/answerCallbackQuery", body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-token-b/answerCallbackQuery", body)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("31st request: expected 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got == "" || got == "0" {
		t.Fatalf("expected Retry-After header on rate-limited response, got %q", got)
	}
}

func TestRateLimit_IsPerBot(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, callCount := newBotAPITestServer(t)
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-token-a": {ID: uuid.MustParse("66666666-6666-6666-6666-666666666666"), UserID: uuid.MustParse("77777777-7777-7777-7777-777777777777"), IsActive: true},
		"bot-token-b": {ID: uuid.MustParse("88888888-8888-8888-8888-888888888888"), UserID: uuid.MustParse("99999999-9999-9999-9999-999999999999"), IsActive: true},
	}, server.URL)

	body := map[string]any{
		"chat_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		"text":    "hello",
	}

	for i := 0; i < botAPIRateLimitPerSec; i++ {
		resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-token-a/sendMessage", body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("bot A request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-token-b/sendMessage", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bot B should have its own limit bucket, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(callCount); got != botAPIRateLimitPerSec+1 {
		t.Fatalf("expected %d downstream calls, got %d", botAPIRateLimitPerSec+1, got)
	}
}

func TestSendVideo_AuthPass(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, _ := newBotAPITestServer(t)
	botID := uuid.MustParse("aaaa0001-0000-0000-0000-000000000000")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-video-ok": {ID: botID, UserID: uuid.MustParse("aaaa0002-0000-0000-0000-000000000000"), IsActive: true},
	}, server.URL)

	body := map[string]any{
		"chat_id": "aaaa0003-0000-0000-0000-000000000000",
	}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-video-ok/sendVideo", body)
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusOK {
		t.Fatalf("sendVideo auth pass: unexpected status (want 200 or 400, got %d)", resp.StatusCode)
	}
}

func TestSendVideo_AuthFail(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{}, "")

	body := map[string]any{"chat_id": "aaaa0003-0000-0000-0000-000000000000"}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/invalid-token/sendVideo", body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sendVideo auth fail: expected 401, got %d", resp.StatusCode)
	}
}

func TestSendVideo_RateLimit(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, _ := newBotAPITestServer(t)
	botID := uuid.MustParse("aaaa0004-0000-0000-0000-000000000000")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-video-rl": {ID: botID, UserID: uuid.MustParse("aaaa0005-0000-0000-0000-000000000000"), IsActive: true},
	}, server.URL)

	body := map[string]any{"chat_id": "aaaa0006-0000-0000-0000-000000000000"}
	for i := 0; i < botAPIRateLimitPerSec; i++ {
		doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-video-rl/sendVideo", body)
	}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-video-rl/sendVideo", body)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("sendVideo rate limit: expected 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got == "" || got == "0" {
		t.Fatalf("sendVideo rate limit: expected Retry-After header, got %q", got)
	}
}

func TestSendAudio_AuthPass(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, _ := newBotAPITestServer(t)
	botID := uuid.MustParse("bbbb0001-0000-0000-0000-000000000000")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-audio-ok": {ID: botID, UserID: uuid.MustParse("bbbb0002-0000-0000-0000-000000000000"), IsActive: true},
	}, server.URL)

	body := map[string]any{"chat_id": "bbbb0003-0000-0000-0000-000000000000"}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-audio-ok/sendAudio", body)
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusOK {
		t.Fatalf("sendAudio auth pass: unexpected status (want 200 or 400, got %d)", resp.StatusCode)
	}
}

func TestSendAudio_AuthFail(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{}, "")

	body := map[string]any{"chat_id": "bbbb0003-0000-0000-0000-000000000000"}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/invalid-token/sendAudio", body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sendAudio auth fail: expected 401, got %d", resp.StatusCode)
	}
}

func TestSendAudio_RateLimit(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, _ := newBotAPITestServer(t)
	botID := uuid.MustParse("bbbb0004-0000-0000-0000-000000000000")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-audio-rl": {ID: botID, UserID: uuid.MustParse("bbbb0005-0000-0000-0000-000000000000"), IsActive: true},
	}, server.URL)

	body := map[string]any{"chat_id": "bbbb0006-0000-0000-0000-000000000000"}
	for i := 0; i < botAPIRateLimitPerSec; i++ {
		doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-audio-rl/sendAudio", body)
	}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-audio-rl/sendAudio", body)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("sendAudio rate limit: expected 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got == "" || got == "0" {
		t.Fatalf("sendAudio rate limit: expected Retry-After header, got %q", got)
	}
}

func TestSendVoice_AuthPass(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, _ := newBotAPITestServer(t)
	botID := uuid.MustParse("cccc0001-0000-0000-0000-000000000000")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-voice-ok": {ID: botID, UserID: uuid.MustParse("cccc0002-0000-0000-0000-000000000000"), IsActive: true},
	}, server.URL)

	body := map[string]any{"chat_id": "cccc0003-0000-0000-0000-000000000000"}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-voice-ok/sendVoice", body)
	if resp.StatusCode != http.StatusBadRequest && resp.StatusCode != http.StatusOK {
		t.Fatalf("sendVoice auth pass: unexpected status (want 200 or 400, got %d)", resp.StatusCode)
	}
}

func TestSendVoice_AuthFail(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{}, "")

	body := map[string]any{"chat_id": "cccc0003-0000-0000-0000-000000000000"}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/invalid-token/sendVoice", body)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sendVoice auth fail: expected 401, got %d", resp.StatusCode)
	}
}

func TestSendVoice_RateLimit(t *testing.T) {
	rdb, _ := newRedisTestClient(t)
	server, _ := newBotAPITestServer(t)
	botID := uuid.MustParse("cccc0004-0000-0000-0000-000000000000")
	app, _ := newRateLimitTestApp(t, rdb, map[string]*model.Bot{
		"bot-voice-rl": {ID: botID, UserID: uuid.MustParse("cccc0005-0000-0000-0000-000000000000"), IsActive: true},
	}, server.URL)

	body := map[string]any{"chat_id": "cccc0006-0000-0000-0000-000000000000"}
	for i := 0; i < botAPIRateLimitPerSec; i++ {
		doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-voice-rl/sendVoice", body)
	}
	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-voice-rl/sendVoice", body)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("sendVoice rate limit: expected 429, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Retry-After"); got == "" || got == "0" {
		t.Fatalf("sendVoice rate limit: expected Retry-After header, got %q", got)
	}
}

func TestRateLimit_DisabledWithoutRedisPassesThrough(t *testing.T) {
	server, callCount := newBotAPITestServer(t)
	app, _ := newRateLimitTestApp(t, nil, map[string]*model.Bot{
		"bot-token-a": {ID: uuid.MustParse("10101010-1010-1010-1010-101010101010"), UserID: uuid.MustParse("11111111-2222-3333-4444-555555555555"), IsActive: true},
	}, server.URL)

	body := map[string]any{
		"chat_id": "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
		"text":    "hello",
	}

	resp := doRateLimitRequest(t, app, http.MethodPost, "/bot/bot-token-a/sendMessage", body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 when Redis is not configured, got %d", resp.StatusCode)
	}
	if got := atomic.LoadInt32(callCount); got != 1 {
		t.Fatalf("expected request to pass through to downstream client, got %d calls", got)
	}
}
