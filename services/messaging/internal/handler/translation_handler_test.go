// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)



// ---------------------------------------------------------------------------
// Mock TranslationStore
// ---------------------------------------------------------------------------

type mockTranslationStore struct {
	getFn      func(ctx context.Context, messageID uuid.UUID, lang string) (*model.MessageTranslation, error)
	getBatchFn func(ctx context.Context, messageIDs []uuid.UUID, lang string) (map[uuid.UUID]*model.MessageTranslation, error)
	upsertFn   func(ctx context.Context, t *model.MessageTranslation) error
}

func (m *mockTranslationStore) Get(ctx context.Context, messageID uuid.UUID, lang string) (*model.MessageTranslation, error) {
	if m.getFn != nil {
		return m.getFn(ctx, messageID, lang)
	}
	return nil, nil
}

func (m *mockTranslationStore) GetBatch(ctx context.Context, messageIDs []uuid.UUID, lang string) (map[uuid.UUID]*model.MessageTranslation, error) {
	if m.getBatchFn != nil {
		return m.getBatchFn(ctx, messageIDs, lang)
	}
	return make(map[uuid.UUID]*model.MessageTranslation), nil
}

func (m *mockTranslationStore) Upsert(ctx context.Context, t *model.MessageTranslation) error {
	if m.upsertFn != nil {
		return m.upsertFn(ctx, t)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newMockAIServer(t *testing.T, responseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		if responseBody != "" {
			fmt.Fprintf(w, "data: %s\n", responseBody)
		}
		fmt.Fprint(w, "data: [DONE]\n")
	}))
}

func newTranslationApp(ts store.TranslationStore, ms store.MessageStore, cs store.ChatStore, aiURL string) *fiber.App {
	app := fiber.New()
	h := NewTranslationHandler(ts, ms, cs, aiURL, "test-secret", slog.Default())
	h.Register(app)
	return app
}

// ---------------------------------------------------------------------------
// GetTranslation — single message
// ---------------------------------------------------------------------------

func TestGetTranslation_CacheHit(t *testing.T) {
	msgID := uuid.New()
	chatID := uuid.New()
	userID := uuid.New()

	ts := &mockTranslationStore{
		getFn: func(_ context.Context, id uuid.UUID, lang string) (*model.MessageTranslation, error) {
			return &model.MessageTranslation{MessageID: id, Lang: lang, Text: "Привет"}, nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("Hello")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}

	app := newTranslationApp(ts, ms, cs, "http://unused")
	req, _ := http.NewRequest(http.MethodGet, "/messages/"+msgID.String()+"/translation/ru", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body model.MessageTranslation
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Text != "Привет" {
		t.Fatalf("expected cached text 'Привет', got %q", body.Text)
	}
}

func TestGetTranslation_CacheMiss_AICall(t *testing.T) {
	msgID := uuid.New()
	chatID := uuid.New()
	userID := uuid.New()

	aiServer := newMockAIServer(t, "Привет мир")
	defer aiServer.Close()

	var upserted bool
	ts := &mockTranslationStore{
		getFn: func(_ context.Context, _ uuid.UUID, _ string) (*model.MessageTranslation, error) {
			return nil, nil // cache miss
		},
		upsertFn: func(_ context.Context, tr *model.MessageTranslation) error {
			upserted = true
			if tr.Text != "Привет мир" {
				t.Errorf("upserted wrong text: %q", tr.Text)
			}
			return nil
		},
	}
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("Hello world")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}

	app := newTranslationApp(ts, ms, cs, aiServer.URL)
	req, _ := http.NewRequest(http.MethodGet, "/messages/"+msgID.String()+"/translation/ru", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !upserted {
		t.Fatal("expected upsert to be called")
	}
}

func TestGetTranslation_AuthFail(t *testing.T) {
	app := newTranslationApp(&mockTranslationStore{}, &mockMessageStore{}, &mockChatStore{}, "http://unused")
	req, _ := http.NewRequest(http.MethodGet, "/messages/"+uuid.New().String()+"/translation/en", nil)
	// no X-User-ID

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGetTranslation_MessageNotFound(t *testing.T) {
	userID := uuid.New()
	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.Message, error) {
			return nil, nil
		},
	}

	app := newTranslationApp(&mockTranslationStore{}, ms, &mockChatStore{}, "http://unused")
	req, _ := http.NewRequest(http.MethodGet, "/messages/"+uuid.New().String()+"/translation/en", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestGetTranslation_NotMember(t *testing.T) {
	msgID := uuid.New()
	chatID := uuid.New()
	userID := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("Hello")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return false, "", nil
		},
	}

	app := newTranslationApp(&mockTranslationStore{}, ms, cs, "http://unused")
	req, _ := http.NewRequest(http.MethodGet, "/messages/"+msgID.String()+"/translation/en", nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GetTranslationsBatch
// ---------------------------------------------------------------------------

func TestGetTranslationsBatch_FullCacheHit(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	msg1 := uuid.New()
	msg2 := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("text")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	ts := &mockTranslationStore{
		getBatchFn: func(_ context.Context, ids []uuid.UUID, lang string) (map[uuid.UUID]*model.MessageTranslation, error) {
			result := make(map[uuid.UUID]*model.MessageTranslation)
			for _, id := range ids {
				result[id] = &model.MessageTranslation{MessageID: id, Lang: lang, Text: "cached"}
			}
			return result, nil
		},
	}

	app := newTranslationApp(ts, ms, cs, "http://unused-should-not-be-called")
	url := fmt.Sprintf("/messages/translations?ids=%s,%s&lang=ru", msg1, msg2)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Translations map[string]*model.MessageTranslation `json:"translations"`
		Uncached     []string                              `json:"uncached"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Translations) != 2 {
		t.Fatalf("expected 2 translations, got %d", len(body.Translations))
	}
	if len(body.Uncached) != 0 {
		t.Fatalf("expected 0 uncached, got %d", len(body.Uncached))
	}
}

func TestGetTranslationsBatch_PartialMiss_TwoChats(t *testing.T) {
	userID := uuid.New()
	chat1 := uuid.New()
	chat2 := uuid.New()
	msg1 := uuid.New() // chat1, cached
	msg2 := uuid.New() // chat1, uncached
	msg3 := uuid.New() // chat2, uncached

	msgMap := map[uuid.UUID]uuid.UUID{msg1: chat1, msg2: chat1, msg3: chat2}

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			chatID, ok := msgMap[id]
			if !ok {
				return nil, nil
			}
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("text")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}
	ts := &mockTranslationStore{
		getBatchFn: func(_ context.Context, ids []uuid.UUID, _ string) (map[uuid.UUID]*model.MessageTranslation, error) {
			result := make(map[uuid.UUID]*model.MessageTranslation)
			for _, id := range ids {
				if id == msg1 {
					result[id] = &model.MessageTranslation{MessageID: id, Lang: "ru", Text: "cached"}
				}
			}
			return result, nil
		},
	}

	// AI returns translations for uncached msgs
	responseMap := map[string]string{
		msg2.String(): "перевод2",
		msg3.String(): "перевод3",
	}
	jsonBytes, _ := json.Marshal(responseMap)

	aiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Return the full map for any request (both chat groups get same server)
		fmt.Fprintf(w, "data: %s\n", string(jsonBytes))
		fmt.Fprint(w, "data: [DONE]\n")
	}))
	defer aiServer.Close()

	app := newTranslationApp(ts, ms, cs, aiServer.URL)
	url := fmt.Sprintf("/messages/translations?ids=%s,%s,%s&lang=ru", msg1, msg2, msg3)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Translations map[string]*model.MessageTranslation `json:"translations"`
		Uncached     []string                              `json:"uncached"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Translations) != 3 {
		t.Fatalf("expected 3 translations, got %d", len(body.Translations))
	}
}

func TestGetTranslationsBatch_AIMalformedJSON(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	msg1 := uuid.New()

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("text")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}

	var upsertCalled bool
	ts := &mockTranslationStore{
		upsertFn: func(_ context.Context, _ *model.MessageTranslation) error {
			upsertCalled = true
			return nil
		},
	}

	// AI returns garbage — for batch (>1 msg) it tries JSON unmarshal which fails.
	// But single msg path just uses raw text. So we need 2 msgs to hit batch path.
	msg2 := uuid.New()
	aiServer := newMockAIServer(t, "not valid json {{{")
	defer aiServer.Close()

	app := newTranslationApp(ts, ms, cs, aiServer.URL)
	url := fmt.Sprintf("/messages/translations?ids=%s,%s&lang=ru", msg1, msg2)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 (with failedIDs), got %d", resp.StatusCode)
	}

	var body struct {
		Translations map[string]*model.MessageTranslation `json:"translations"`
		Uncached     []string                              `json:"uncached"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body.Uncached) != 2 {
		t.Fatalf("expected 2 uncached (failed), got %d: %v", len(body.Uncached), body.Uncached)
	}
	if upsertCalled {
		t.Fatal("upsert should NOT be called for malformed AI response")
	}
}

func TestGetTranslationsBatch_AIPartialMap(t *testing.T) {
	userID := uuid.New()
	chatID := uuid.New()
	ids := make([]uuid.UUID, 5)
	for i := range ids {
		ids[i] = uuid.New()
	}

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("text")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, _ uuid.UUID, _ uuid.UUID) (bool, string, error) {
			return true, "member", nil
		},
	}

	var upsertCount int
	ts := &mockTranslationStore{
		upsertFn: func(_ context.Context, _ *model.MessageTranslation) error {
			upsertCount++
			return nil
		},
	}

	// AI returns only 3 of 5
	partialMap := map[string]string{
		ids[0].String(): "t1",
		ids[1].String(): "t2",
		ids[2].String(): "t3",
	}
	jsonBytes, _ := json.Marshal(partialMap)
	aiServer := newMockAIServer(t, string(jsonBytes))
	defer aiServer.Close()

	app := newTranslationApp(ts, ms, cs, aiServer.URL)
	idStrs := make([]string, len(ids))
	for i, id := range ids {
		idStrs[i] = id.String()
	}
	url := fmt.Sprintf("/messages/translations?ids=%s,%s,%s,%s,%s&lang=ru", idStrs[0], idStrs[1], idStrs[2], idStrs[3], idStrs[4])
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Translations map[string]*model.MessageTranslation `json:"translations"`
		Uncached     []string                              `json:"uncached"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	if len(body.Translations) != 3 {
		t.Fatalf("expected 3 translations, got %d", len(body.Translations))
	}
	if len(body.Uncached) != 2 {
		t.Fatalf("expected 2 uncached, got %d", len(body.Uncached))
	}
	if upsertCount != 3 {
		t.Fatalf("expected 3 upserts, got %d", upsertCount)
	}
}

func TestGetTranslationsBatch_CrossChat_PartialMembership(t *testing.T) {
	userID := uuid.New()
	chat1 := uuid.New() // member
	chat2 := uuid.New() // NOT member
	msg1 := uuid.New()  // chat1
	msg2 := uuid.New()  // chat2

	msgMap := map[uuid.UUID]uuid.UUID{msg1: chat1, msg2: chat2}

	ms := &mockMessageStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.Message, error) {
			chatID, ok := msgMap[id]
			if !ok {
				return nil, nil
			}
			return &model.Message{ID: id, ChatID: chatID, Content: strPtr("text")}, nil
		},
	}
	cs := &mockChatStore{
		isMemberFn: func(_ context.Context, cid uuid.UUID, _ uuid.UUID) (bool, string, error) {
			if cid == chat1 {
				return true, "member", nil
			}
			return false, "", nil
		},
	}
	ts := &mockTranslationStore{
		getBatchFn: func(_ context.Context, ids []uuid.UUID, _ string) (map[uuid.UUID]*model.MessageTranslation, error) {
			result := make(map[uuid.UUID]*model.MessageTranslation)
			for _, id := range ids {
				result[id] = &model.MessageTranslation{MessageID: id, Lang: "ru", Text: "ok"}
			}
			return result, nil
		},
	}

	app := newTranslationApp(ts, ms, cs, "http://unused")
	url := fmt.Sprintf("/messages/translations?ids=%s,%s&lang=ru", msg1, msg2)
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Translations map[string]*model.MessageTranslation `json:"translations"`
	}
	json.NewDecoder(resp.Body).Decode(&body)

	// Only msg1 should be in translations (member of chat1 only)
	if len(body.Translations) != 1 {
		t.Fatalf("expected 1 translation, got %d", len(body.Translations))
	}
	if _, ok := body.Translations[msg1.String()]; !ok {
		t.Fatal("expected msg1 in translations")
	}
}

func TestGetTranslationsBatch_EmptyIDs(t *testing.T) {
	app := newTranslationApp(&mockTranslationStore{}, &mockMessageStore{}, &mockChatStore{}, "http://unused")
	req, _ := http.NewRequest(http.MethodGet, "/messages/translations?ids=&lang=en", nil)
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetTranslationsBatch_InvalidLang(t *testing.T) {
	app := newTranslationApp(&mockTranslationStore{}, &mockMessageStore{}, &mockChatStore{}, "http://unused")
	req, _ := http.NewRequest(http.MethodGet, "/messages/translations?ids="+uuid.New().String()+"&lang=!!!", nil)
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// extractJSONFromResponse
// ---------------------------------------------------------------------------

func TestExtractJSONFromResponse_PlainJSON(t *testing.T) {
	input := `{"a":"b","c":"d"}`
	result := extractJSONFromResponse(input)
	var m map[string]string
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	if m["a"] != "b" {
		t.Fatalf("expected a=b, got %q", m["a"])
	}
}

func TestExtractJSONFromResponse_FencedJSON(t *testing.T) {
	input := "```json\n{\"a\":\"b\"}\n```"
	result := extractJSONFromResponse(input)
	var m map[string]string
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("failed to parse fenced JSON: %v", err)
	}
	if m["a"] != "b" {
		t.Fatalf("expected a=b, got %q", m["a"])
	}
}

func TestExtractJSONFromResponse_NoJSON(t *testing.T) {
	input := "hello world"
	result := extractJSONFromResponse(input)
	// No braces → returns raw string
	if result != "hello world" {
		t.Fatalf("expected raw string back, got %q", result)
	}
}
