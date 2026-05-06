// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/ai/internal/client"
	"github.com/mst-corp/orbit/services/ai/internal/service"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newMiniredis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr
}

// newTranslateApp wires up a Fiber app with the real handler→service→client
// stack, pointing Anthropic and Messaging clients at the given test servers.
func newTranslateApp(t *testing.T, anthropicURL, messagingURL string) *fiber.App {
	t.Helper()
	mr := newMiniredis(t)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	anthropicClient := client.NewAnthropicClient("test-key", "test-model", slog.Default())
	anthropicClient.SetBaseURL(anthropicURL)

	messagingClient := client.NewMessagingClient(messagingURL, "test-token")

	svc := service.NewAIService(service.AIServiceConfig{
		Anthropic: anthropicClient,
		Messaging: messagingClient,
		Redis:     rdb,
		Logger:    slog.Default(),
	})

	app := fiber.New()
	h := NewAIHandler(svc, slog.Default())
	h.Register(app)
	return app
}

// ---------------------------------------------------------------------------
// Test 1: Cross-chat batch — partial AI failure
//
// Setup: 3 messages from 2 chats. The Anthropic mock returns a JSON map
// that only contains translations for chat2's messages, simulating a partial
// failure where chat1's message was dropped by the LLM.
//
// The translate endpoint uses response_format=json_map for batch (>1 msg).
// The service sends all messages in one Anthropic call and returns whatever
// the LLM produces. If some IDs are missing from the JSON map, the SSE
// stream still succeeds — the frontend handles missing keys as untranslated.
// ---------------------------------------------------------------------------

func TestTranslate_CrossChatBatch_PartialAIFailure(t *testing.T) {
	userID := uuid.New()

	msg1 := uuid.New() // chat1 — AI will NOT return translation for this
	msg2 := uuid.New() // chat2 — AI succeeds
	msg3 := uuid.New() // chat2 — AI succeeds

	now := time.Now().UTC()

	// Mock messaging service: returns all 3 messages
	messagingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"data": []map[string]any{
				{"id": msg1.String(), "sender_id": uuid.New().String(), "content": "Hello", "created_at": now.Format(time.RFC3339)},
				{"id": msg2.String(), "sender_id": uuid.New().String(), "content": "Bonjour", "created_at": now.Format(time.RFC3339)},
				{"id": msg3.String(), "sender_id": uuid.New().String(), "content": "Salut", "created_at": now.Format(time.RFC3339)},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer messagingSrv.Close()

	// Mock Anthropic: returns JSON map with only msg2 and msg3 translated
	// (simulates partial failure — msg1 dropped)
	partialMap := map[string]string{
		msg2.String(): "Привет",
		msg3.String(): "Привет",
	}
	partialJSON, _ := json.Marshal(partialMap)

	anthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Non-streaming response for json_map branch
		resp := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": string(partialJSON)},
			},
			"usage": map[string]any{
				"input_tokens":  100,
				"output_tokens": 50,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer anthropicSrv.Close()

	app := newTranslateApp(t, anthropicSrv.URL, messagingSrv.URL)

	body := fmt.Sprintf(`{
		"message_ids": ["%s", "%s", "%s"],
		"target_language": "ru",
		"response_format": "json_map"
	}`, msg1, msg2, msg3)

	req, _ := http.NewRequest(http.MethodPost, "/ai/translate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	// The response is SSE — read all data frames
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	bodyStr := string(rawBody)

	// Parse the SSE stream: find the delta frame containing the JSON map
	var translationMap map[string]string
	for _, line := range strings.Split(bodyStr, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		if event["type"] == "delta" {
			text, _ := event["text"].(string)
			if err := json.Unmarshal([]byte(text), &translationMap); err == nil {
				break
			}
		}
	}

	if translationMap == nil {
		t.Fatalf("no translation JSON map found in SSE stream: %s", bodyStr)
	}

	// msg2 and msg3 should be translated
	if translationMap[msg2.String()] != "Привет" {
		t.Errorf("expected msg2 translated to 'Привет', got %q", translationMap[msg2.String()])
	}
	if translationMap[msg3.String()] != "Привет" {
		t.Errorf("expected msg3 translated to 'Привет', got %q", translationMap[msg3.String()])
	}

	// msg1 should NOT be in the map (partial failure)
	if _, ok := translationMap[msg1.String()]; ok {
		t.Errorf("msg1 should not be in translation map (simulated partial AI failure)")
	}
}

// ---------------------------------------------------------------------------
// Test 2: Batch truncation at limit 50
//
// The service layer rejects requests with more than 50 message_ids,
// returning 400 Bad Request. This test sends 51 IDs and verifies the error.
// ---------------------------------------------------------------------------

func TestTranslate_BatchTruncationAt50(t *testing.T) {
	userID := uuid.New()

	// Build 51 message IDs
	ids := make([]string, 51)
	for i := range ids {
		ids[i] = `"` + uuid.New().String() + `"`
	}
	idsJSON := "[" + strings.Join(ids, ",") + "]"

	// No need for real servers — validation happens before any external calls
	anthropicSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("anthropic should not be called for over-limit batch")
	}))
	defer anthropicSrv.Close()

	messagingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("messaging should not be called for over-limit batch")
	}))
	defer messagingSrv.Close()

	app := newTranslateApp(t, anthropicSrv.URL, messagingSrv.URL)

	body := fmt.Sprintf(`{
		"message_ids": %s,
		"target_language": "ru"
	}`, idsJSON)

	req, _ := http.NewRequest(http.MethodPost, "/ai/translate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for 51 message_ids, got %d: %s", resp.StatusCode, raw)
	}

	// Verify the error message mentions the limit
	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), "50") {
		t.Errorf("expected error to mention limit 50, got: %s", raw)
	}

	// Verify 50 is accepted (boundary check)
	ids50 := make([]string, 50)
	for i := range ids50 {
		ids50[i] = `"` + uuid.New().String() + `"`
	}
	body50 := fmt.Sprintf(`{
		"message_ids": [%s],
		"target_language": "ru"
	}`, strings.Join(ids50, ","))

	// For the 50-ID request, messaging will be called. Set up a server that
	// returns empty data (no messages found) — triggers "No messages found" 400.
	messagingSrv50 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer messagingSrv50.Close()

	app50 := newTranslateApp(t, anthropicSrv.URL, messagingSrv50.URL)
	req50, _ := http.NewRequest(http.MethodPost, "/ai/translate", strings.NewReader(body50))
	req50.Header.Set("Content-Type", "application/json")
	req50.Header.Set("X-User-ID", userID.String())

	resp50, err := app50.Test(req50, -1)
	if err != nil {
		t.Fatalf("app.Test (50 ids): %v", err)
	}
	// 50 IDs should NOT get the "Too many" error — it should pass validation.
	// It may return 400 for "No messages found" since messaging returns empty,
	// but NOT for the batch limit.
	raw50, _ := io.ReadAll(resp50.Body)
	if strings.Contains(string(raw50), "Too many") {
		t.Fatalf("50 message_ids should be accepted, got: %s", raw50)
	}
}

// ---------------------------------------------------------------------------
// Ask validation tests
// ---------------------------------------------------------------------------

func TestAsk_MissingChatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"prompt":"hello"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestAsk_InvalidChatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"chat_id":"not-a-uuid","prompt":"hello"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestAsk_MissingPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"chat_id":"` + uuid.New().String() + `"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestAsk_PromptTooLong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	longPrompt := strings.Repeat("a", 4097)
	body := `{"chat_id":"` + uuid.New().String() + `","prompt":"` + longPrompt + `"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/ask", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// Summarize validation tests
// ---------------------------------------------------------------------------

func TestSummarize_MissingChatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"time_range":"1h","language":"ru"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/summarize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestSummarize_InvalidTimeRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"chat_id":"` + uuid.New().String() + `","time_range":"2h","language":"ru"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/summarize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestSummarize_MissingLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"chat_id":"` + uuid.New().String() + `","time_range":"1h"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/summarize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// SuggestReply validation tests
// ---------------------------------------------------------------------------

func TestSuggestReply_MissingChatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/reply-suggest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestSuggestReply_InvalidChatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"chat_id":"bad"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/reply-suggest", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// Transcribe validation tests
// ---------------------------------------------------------------------------

func TestTranscribe_MissingMediaID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/transcribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestTranscribe_InvalidMediaID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"media_id":"not-uuid"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/transcribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// Search validation tests
// ---------------------------------------------------------------------------

func TestSearch_MissingQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestSearch_QueryTooLong(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	longQuery := strings.Repeat("a", 513)
	body := `{"query":"` + longQuery + `"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestSearch_InvalidChatID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"query":"hello","chat_id":"bad-uuid"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/search", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// Translate additional validation tests
// ---------------------------------------------------------------------------

func TestTranslate_MissingMessageIDs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"target_language":"ru"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/translate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

func TestTranslate_InvalidResponseFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach service")
	}))
	defer srv.Close()
	app := newTranslateApp(t, srv.URL, srv.URL)

	body := `{"message_ids":["` + uuid.New().String() + `"],"target_language":"ru","response_format":"xml"}`
	req, _ := http.NewRequest(http.MethodPost, "/ai/translate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// Capabilities endpoint — frontend feature-gates UI by these flags so users
// don't see broken buttons (e.g. Transcribe) when the corresponding provider
// key is empty/"placeholder" on Saturn.
// ---------------------------------------------------------------------------

// newCapsApp builds a Fiber app where the AI service has explicit Anthropic
// and Whisper clients with the given keys. Empty/"placeholder" keys mean
// the corresponding Configured() returns false.
func newCapsApp(t *testing.T, anthropicKey, whisperKey string) *fiber.App {
	t.Helper()
	mr := newMiniredis(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	anthropicClient := client.NewAnthropicClient(anthropicKey, "test-model", slog.Default())
	whisperClient := client.NewWhisperClient(whisperKey, "whisper-1", slog.Default())
	messagingClient := client.NewMessagingClient("http://unused", "test-token")

	svc := service.NewAIService(service.AIServiceConfig{
		Anthropic: anthropicClient,
		Whisper:   whisperClient,
		Messaging: messagingClient,
		Redis:     rdb,
		Logger:    slog.Default(),
	})

	app := fiber.New()
	h := NewAIHandler(svc, slog.Default())
	h.Register(app)
	return app
}

func TestCapabilities_BothConfigured(t *testing.T) {
	app := newCapsApp(t, "real-anthropic-key", "real-whisper-key")

	req, _ := http.NewRequest(http.MethodGet, "/ai/capabilities", nil)
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var body struct {
		AnthropicConfigured bool `json:"anthropic_configured"`
		WhisperConfigured   bool `json:"whisper_configured"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.AnthropicConfigured {
		t.Errorf("anthropic_configured = false, want true")
	}
	if !body.WhisperConfigured {
		t.Errorf("whisper_configured = false, want true")
	}
}

func TestCapabilities_WhisperPlaceholder(t *testing.T) {
	app := newCapsApp(t, "real-anthropic-key", "placeholder")

	req, _ := http.NewRequest(http.MethodGet, "/ai/capabilities", nil)
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var body struct {
		AnthropicConfigured bool `json:"anthropic_configured"`
		WhisperConfigured   bool `json:"whisper_configured"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.AnthropicConfigured {
		t.Errorf("anthropic_configured = false, want true")
	}
	if body.WhisperConfigured {
		t.Errorf("whisper_configured = true, want false (placeholder key)")
	}
}

func TestCapabilities_BothEmpty(t *testing.T) {
	app := newCapsApp(t, "", "")

	req, _ := http.NewRequest(http.MethodGet, "/ai/capabilities", nil)
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var body struct {
		AnthropicConfigured bool `json:"anthropic_configured"`
		WhisperConfigured   bool `json:"whisper_configured"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.AnthropicConfigured {
		t.Errorf("anthropic_configured = true, want false")
	}
	if body.WhisperConfigured {
		t.Errorf("whisper_configured = true, want false")
	}
}

func TestCapabilities_RequiresUserID(t *testing.T) {
	app := newCapsApp(t, "", "")

	req, _ := http.NewRequest(http.MethodGet, "/ai/capabilities", nil)
	// no X-User-ID — should be rejected by getUserID

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 401 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401, got %d: %s", resp.StatusCode, raw)
	}
}
