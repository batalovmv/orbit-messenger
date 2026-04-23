// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// ---------------------------------------------------------------------------
// Mock FolderStore
// ---------------------------------------------------------------------------

type mockFolderStore struct {
	listFn        func(ctx context.Context, userID uuid.UUID) ([]*store.ChatFolder, error)
	getFn         func(ctx context.Context, id int, userID uuid.UUID) (*store.ChatFolder, error)
	createFn      func(ctx context.Context, f *store.ChatFolder) error
	updateFn      func(ctx context.Context, f *store.ChatFolder) error
	deleteFn      func(ctx context.Context, id int, userID uuid.UUID) error
	updateOrderFn func(ctx context.Context, userID uuid.UUID, folderIDs []int) error
	setChatsFn    func(ctx context.Context, folderID int, userID uuid.UUID, included, excluded, pinned []string) error
}

func (m *mockFolderStore) List(ctx context.Context, userID uuid.UUID) ([]*store.ChatFolder, error) {
	if m.listFn != nil {
		return m.listFn(ctx, userID)
	}
	return []*store.ChatFolder{}, nil
}

func (m *mockFolderStore) Get(ctx context.Context, id int, userID uuid.UUID) (*store.ChatFolder, error) {
	if m.getFn != nil {
		return m.getFn(ctx, id, userID)
	}
	return nil, apperror.NotFound("folder not found")
}

func (m *mockFolderStore) Create(ctx context.Context, f *store.ChatFolder) error {
	if m.createFn != nil {
		return m.createFn(ctx, f)
	}
	f.ID = 1
	f.Position = 0
	f.CreatedAt = time.Now()
	f.UpdatedAt = time.Now()
	return nil
}

func (m *mockFolderStore) Update(ctx context.Context, f *store.ChatFolder) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, f)
	}
	return nil
}

func (m *mockFolderStore) Delete(ctx context.Context, id int, userID uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id, userID)
	}
	return nil
}

func (m *mockFolderStore) UpdateOrder(ctx context.Context, userID uuid.UUID, folderIDs []int) error {
	if m.updateOrderFn != nil {
		return m.updateOrderFn(ctx, userID, folderIDs)
	}
	return nil
}

func (m *mockFolderStore) SetChats(ctx context.Context, folderID int, userID uuid.UUID, included, excluded, pinned []string) error {
	if m.setChatsFn != nil {
		return m.setChatsFn(ctx, folderID, userID, included, excluded, pinned)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func newFolderApp(fs *mockFolderStore) *fiber.App {
	app := fiber.New()
	svc := service.NewFolderService(fs)
	h := NewFolderHandler(svc, slog.Default())
	h.Register(app)
	return app
}

func doFolderReq(t *testing.T, app *fiber.App, method, path string, body interface{}, userID string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, path, reader)
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

func readFolderBody(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal body: %v (raw=%s)", err, raw)
	}
	return m
}

// ---------------------------------------------------------------------------
// GET /folders
// ---------------------------------------------------------------------------

func TestFolderList_Happy(t *testing.T) {
	uid := uuid.New()
	fs := &mockFolderStore{
		listFn: func(_ context.Context, userID uuid.UUID) ([]*store.ChatFolder, error) {
			if userID != uid {
				t.Errorf("unexpected userID %s", userID)
			}
			return []*store.ChatFolder{
				{ID: 1, UserID: uid, Title: "Work", Position: 0},
				{ID: 2, UserID: uid, Title: "Personal", Position: 1},
			}, nil
		},
	}
	app := newFolderApp(fs)
	resp := doFolderReq(t, app, http.MethodGet, "/folders", nil, uid.String())
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var list []map[string]interface{}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 folders, got %d", len(list))
	}
	if list[0]["title"] != "Work" {
		t.Errorf("expected title Work, got %v", list[0]["title"])
	}
	if list[1]["title"] != "Personal" {
		t.Errorf("expected title Personal, got %v", list[1]["title"])
	}
}

func TestFolderList_MissingUserID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodGet, "/folders", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestFolderList_InvalidUserID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodGet, "/folders", nil, "not-a-uuid")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// POST /folders
// ---------------------------------------------------------------------------

func TestFolderCreate_Happy(t *testing.T) {
	uid := uuid.New()
	chatID := uuid.New().String()
	var createdFolder *store.ChatFolder

	fs := &mockFolderStore{
		createFn: func(_ context.Context, f *store.ChatFolder) error {
			f.ID = 5
			f.Position = 0
			f.CreatedAt = time.Now()
			f.UpdatedAt = time.Now()
			createdFolder = f
			return nil
		},
	}
	app := newFolderApp(fs)
	bodyMap := map[string]interface{}{"title": "Work", "emoticon": "briefcase", "included_chat_ids": []string{chatID}}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", bodyMap, uid.String())
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201, got %d: %s", resp.StatusCode, raw)
	}
	result := readFolderBody(t, resp)
	if result["title"] != "Work" {
		t.Errorf("expected title Work, got %v", result["title"])
	}
	if int(result["id"].(float64)) != 5 {
		t.Errorf("expected id 5, got %v", result["id"])
	}
	if createdFolder == nil {
		t.Fatal("createFn not called")
	}
	if createdFolder.UserID != uid {
		t.Errorf("expected userID %s, got %s", uid, createdFolder.UserID)
	}
}

func TestFolderCreate_MissingTitle(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{"emoticon": "x"}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderCreate_InvalidUUIDInChatIDs(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{
		"title":             "Work",
		"included_chat_ids": []string{"not-a-uuid"},
	}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	result := readFolderBody(t, resp)
	msg, _ := result["message"].(string)
	if msg == "" {
		t.Error("expected error message about invalid UUID")
	}
}

func TestFolderCreate_MissingUserID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{"title": "Work"}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestFolderCreate_TooManyFolders(t *testing.T) {
	fs := &mockFolderStore{
		createFn: func(_ context.Context, f *store.ChatFolder) error {
			return apperror.BadRequest("too many folders")
		},
	}
	app := newFolderApp(fs)
	body := map[string]interface{}{"title": "Another"}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	result := readFolderBody(t, resp)
	if result["message"] != "too many folders" {
		t.Errorf("expected 'too many folders', got %v", result["message"])
	}
}

func TestFolderCreate_TitleTooLong(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	longTitle := ""
	for i := 0; i < 65; i++ {
		longTitle += "x"
	}
	body := map[string]interface{}{"title": longTitle}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400 for title > 64 chars, got %d", resp.StatusCode)
	}
}

func TestFolderCreate_InvalidBody(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	req, _ := http.NewRequest(http.MethodPost, "/folders", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// GET /folders/:id
// ---------------------------------------------------------------------------

func TestFolderGet_Happy(t *testing.T) {
	uid := uuid.New()
	fs := &mockFolderStore{
		getFn: func(_ context.Context, id int, userID uuid.UUID) (*store.ChatFolder, error) {
			if id != 3 || userID != uid {
				return nil, apperror.NotFound("folder not found")
			}
			return &store.ChatFolder{ID: 3, UserID: uid, Title: "Work", Position: 0}, nil
		},
	}
	app := newFolderApp(fs)
	resp := doFolderReq(t, app, http.MethodGet, "/folders/3", nil, uid.String())
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	result := readFolderBody(t, resp)
	if result["title"] != "Work" {
		t.Errorf("expected title Work, got %v", result["title"])
	}
	if int(result["id"].(float64)) != 3 {
		t.Errorf("expected id 3, got %v", result["id"])
	}
}

func TestFolderGet_NotFound(t *testing.T) {
	fs := &mockFolderStore{
		getFn: func(_ context.Context, id int, userID uuid.UUID) (*store.ChatFolder, error) {
			return nil, apperror.NotFound("folder not found")
		},
	}
	app := newFolderApp(fs)
	resp := doFolderReq(t, app, http.MethodGet, "/folders/999", nil, uuid.New().String())
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestFolderGet_InvalidID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodGet, "/folders/abc", nil, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderGet_ZeroID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodGet, "/folders/0", nil, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderGet_NegativeID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodGet, "/folders/-1", nil, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderGet_MissingUserID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodGet, "/folders/1", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PUT /folders/:id
// ---------------------------------------------------------------------------

func TestFolderUpdate_Happy(t *testing.T) {
	uid := uuid.New()
	fs := &mockFolderStore{
		updateFn: func(_ context.Context, f *store.ChatFolder) error {
			if f.ID != 3 || f.UserID != uid {
				return apperror.NotFound("folder not found")
			}
			return nil
		},
		getFn: func(_ context.Context, id int, userID uuid.UUID) (*store.ChatFolder, error) {
			return &store.ChatFolder{ID: 3, UserID: uid, Title: "Updated", Position: 0}, nil
		},
	}
	app := newFolderApp(fs)
	body := map[string]interface{}{"title": "Updated"}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/3", body, uid.String())
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	result := readFolderBody(t, resp)
	if result["title"] != "Updated" {
		t.Errorf("expected title Updated, got %v", result["title"])
	}
}

func TestFolderUpdate_NotFound(t *testing.T) {
	fs := &mockFolderStore{
		updateFn: func(_ context.Context, f *store.ChatFolder) error {
			return apperror.NotFound("folder not found")
		},
	}
	app := newFolderApp(fs)
	body := map[string]interface{}{"title": "Updated"}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/999", body, uuid.New().String())
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestFolderUpdate_InvalidBody(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	req, _ := http.NewRequest(http.MethodPut, "/folders/1", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderUpdate_MissingTitle(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{"emoticon": "x"}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/1", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderUpdate_MissingUserID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{"title": "Work"}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/1", body, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// DELETE /folders/:id
// ---------------------------------------------------------------------------

func TestFolderDelete_Happy(t *testing.T) {
	uid := uuid.New()
	deleted := false
	fs := &mockFolderStore{
		deleteFn: func(_ context.Context, id int, userID uuid.UUID) error {
			if id != 3 || userID != uid {
				return apperror.NotFound("folder not found")
			}
			deleted = true
			return nil
		},
	}
	app := newFolderApp(fs)
	resp := doFolderReq(t, app, http.MethodDelete, "/folders/3", nil, uid.String())
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !deleted {
		t.Error("delete was not called")
	}
	result := readFolderBody(t, resp)
	if result["ok"] != true {
		t.Errorf("expected ok=true, got %v", result["ok"])
	}
}

func TestFolderDelete_NotFound(t *testing.T) {
	fs := &mockFolderStore{
		deleteFn: func(_ context.Context, id int, userID uuid.UUID) error {
			return apperror.NotFound("folder not found")
		},
	}
	app := newFolderApp(fs)
	resp := doFolderReq(t, app, http.MethodDelete, "/folders/999", nil, uuid.New().String())
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestFolderDelete_InvalidID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodDelete, "/folders/abc", nil, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderDelete_MissingUserID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	resp := doFolderReq(t, app, http.MethodDelete, "/folders/1", nil, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PUT /folders/order
// ---------------------------------------------------------------------------

func TestFolderOrder_Happy(t *testing.T) {
	uid := uuid.New()
	var receivedIDs []int
	fs := &mockFolderStore{
		updateOrderFn: func(_ context.Context, userID uuid.UUID, folderIDs []int) error {
			if userID != uid {
				return apperror.NotFound("folder not found")
			}
			receivedIDs = folderIDs
			return nil
		},
	}
	app := newFolderApp(fs)
	body := map[string]interface{}{"folder_ids": []int{3, 1, 2}}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/order", body, uid.String())
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if len(receivedIDs) != 3 || receivedIDs[0] != 3 || receivedIDs[1] != 1 || receivedIDs[2] != 2 {
		t.Errorf("expected [3,1,2], got %v", receivedIDs)
	}
}

func TestFolderOrder_EmptyList(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{"folder_ids": []int{}}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/order", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestFolderOrder_DuplicateIDs(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{"folder_ids": []int{1, 1, 2}}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/order", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	result := readFolderBody(t, resp)
	msg, _ := result["message"].(string)
	if msg != "duplicate folder ID in folder_ids" {
		t.Errorf("expected duplicate error message, got %q", msg)
	}
}

func TestFolderOrder_MissingUserID(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	body := map[string]interface{}{"folder_ids": []int{1, 2}}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/order", body, "")
	if resp.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestFolderOrder_InvalidBody(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	req, _ := http.NewRequest(http.MethodPut, "/folders/order", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uuid.New().String())
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Boundary: chat IDs lists > 200 entries
// ---------------------------------------------------------------------------

func TestFolderCreate_TooManyIncludedChatIDs(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	ids := make([]string, 201)
	for i := range ids {
		ids[i] = uuid.New().String()
	}
	body := map[string]interface{}{
		"title":             "Work",
		"included_chat_ids": ids,
	}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Response shape: nil slices become empty arrays
// ---------------------------------------------------------------------------

func TestFolderGet_EmptyArraysNotNull(t *testing.T) {
	uid := uuid.New()
	fs := &mockFolderStore{
		getFn: func(_ context.Context, id int, userID uuid.UUID) (*store.ChatFolder, error) {
			return &store.ChatFolder{
				ID: 1, UserID: uid, Title: "Test", Position: 0,
			}, nil
		},
	}
	app := newFolderApp(fs)
	resp := doFolderReq(t, app, http.MethodGet, "/folders/1", nil, uid.String())
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	result := make(map[string]interface{})
	json.Unmarshal(raw, &result)

	for _, field := range []string{"included_chat_ids", "excluded_chat_ids", "pinned_chat_ids"} {
		v := result[field]
		arr, ok := v.([]interface{})
		if !ok {
			t.Errorf("%s should be array, got %T (%v)", field, v, v)
		} else if len(arr) != 0 {
			t.Errorf("%s should be empty array, got %v", field, arr)
		}
	}
}

// ---------------------------------------------------------------------------
// Emoticon handling: empty string -> nil, non-empty -> stored
// ---------------------------------------------------------------------------

func TestFolderCreate_EmptyEmoticonNotStored(t *testing.T) {
	uid := uuid.New()
	var capturedEmoticon *string
	fs := &mockFolderStore{
		createFn: func(_ context.Context, f *store.ChatFolder) error {
			capturedEmoticon = f.Emoticon
			f.ID = 1
			f.Position = 0
			f.CreatedAt = time.Now()
			f.UpdatedAt = time.Now()
			return nil
		},
	}
	app := newFolderApp(fs)
	body := map[string]interface{}{"title": "Work", "emoticon": ""}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, uid.String())
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if capturedEmoticon != nil {
		t.Errorf("empty emoticon should be nil, got %q", *capturedEmoticon)
	}
}

func TestFolderCreate_NonEmptyEmoticonStored(t *testing.T) {
	uid := uuid.New()
	var capturedEmoticon *string
	fs := &mockFolderStore{
		createFn: func(_ context.Context, f *store.ChatFolder) error {
			capturedEmoticon = f.Emoticon
			f.ID = 1
			f.Position = 0
			f.CreatedAt = time.Now()
			f.UpdatedAt = time.Now()
			return nil
		},
	}
	app := newFolderApp(fs)
	body := map[string]interface{}{"title": "Work", "emoticon": "fire"}
	resp := doFolderReq(t, app, http.MethodPost, "/folders", body, uid.String())
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if capturedEmoticon == nil || *capturedEmoticon != "fire" {
		t.Errorf("expected emoticon fire, got %v", capturedEmoticon)
	}
}

// ---------------------------------------------------------------------------
// Over 100 folder_ids
// ---------------------------------------------------------------------------

func TestFolderOrder_TooManyIDs(t *testing.T) {
	app := newFolderApp(&mockFolderStore{})
	ids := make([]int, 101)
	for i := range ids {
		ids[i] = i + 1
	}
	body := map[string]interface{}{"folder_ids": ids}
	resp := doFolderReq(t, app, http.MethodPut, "/folders/order", body, uuid.New().String())
	if resp.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Service error propagation (internal error -> 500)
// ---------------------------------------------------------------------------

func TestFolderList_InternalError(t *testing.T) {
	fs := &mockFolderStore{
		listFn: func(_ context.Context, _ uuid.UUID) ([]*store.ChatFolder, error) {
			return nil, fmt.Errorf("db connection failed")
		},
	}
	app := newFolderApp(fs)
	resp := doFolderReq(t, app, http.MethodGet, "/folders", nil, uuid.New().String())
	if resp.StatusCode != 500 {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

