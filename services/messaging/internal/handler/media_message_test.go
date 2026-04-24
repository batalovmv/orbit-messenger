// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// ---------------------------------------------------------------------------
// BUG-FINDING: SendMessage with media_ids
// ---------------------------------------------------------------------------

func TestSendMessage_MediaWithoutContent(t *testing.T) {
	// Should succeed: media_ids provided, content empty
	userID := uuid.New()
	chatID := uuid.New()
	mediaID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := fmt.Sprintf(`{"media_ids":["%s"],"type":"photo"}`, mediaID)
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("BUG: SendMessage with media_ids but no content should succeed, got 400: %s", body)
	}
	// Should be 201 (created)
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 for media message without content, got %d: %s", resp.StatusCode, body)
	}
}

func TestSendMessage_NoContentNoMedia(t *testing.T) {
	// Should fail: neither content nor media_ids
	userID := uuid.New()
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := `{}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content + empty media_ids, got %d", resp.StatusCode)
	}
}

func TestSendMessage_EmptyContentEmptyMediaArray(t *testing.T) {
	// Explicit empty: content="" and media_ids=[]
	userID := uuid.New()
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := `{"content":"","media_ids":[]}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty content + empty media array, got %d", resp.StatusCode)
	}
}

func TestSendMessage_TooManyMediaIDs(t *testing.T) {
	// Should fail: 11 media_ids (max is 10)
	userID := uuid.New()
	chatID := uuid.New()

	ids := make([]string, 11)
	for i := range ids {
		ids[i] = fmt.Sprintf(`"%s"`, uuid.New())
	}
	body := fmt.Sprintf(`{"content":"test","media_ids":[%s]}`, joinStrings(ids))

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for 11 media_ids, got %d: %s", resp.StatusCode, body)
	}
}

func TestSendMessage_ExactlyTenMediaIDs(t *testing.T) {
	// Should succeed: exactly 10 (boundary)
	userID := uuid.New()
	chatID := uuid.New()

	ids := make([]string, 10)
	for i := range ids {
		ids[i] = fmt.Sprintf(`"%s"`, uuid.New())
	}
	body := fmt.Sprintf(`{"content":"album","media_ids":[%s],"type":"photo"}`, joinStrings(ids))

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, _ := app.Test(req, -1)
	if resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("BUG: 10 media_ids should be allowed, got 400: %s", body)
	}
}

func TestSendMessage_InvalidMediaID(t *testing.T) {
	// Should fail: invalid UUID in media_ids
	userID := uuid.New()
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	body := `{"media_ids":["not-a-uuid"],"type":"photo"}`
	req, _ := http.NewRequest(http.MethodPost, "/chats/"+chatID.String()+"/messages", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID.String())

	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid media UUID, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: MediaAttachment JSON shape
// ---------------------------------------------------------------------------

func TestMediaAttachment_JSONOmitEmpty(t *testing.T) {
	// File type: width/height/duration should be omitted (nil)
	att := model.MediaAttachment{
		MediaID:          uuid.New().String(),
		Type:             "file",
		MimeType:         "application/pdf",
		SizeBytes:        1024,
		ProcessingStatus: "ready",
		Position:         0,
	}

	data, _ := json.Marshal(att)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if _, ok := raw["width"]; ok {
		val := raw["width"]
		if val != nil {
			t.Error("BUG: width should be nil/omitted for file type, got:", val)
		}
	}
	if _, ok := raw["duration_seconds"]; ok {
		val := raw["duration_seconds"]
		if val != nil {
			t.Error("BUG: duration should be nil/omitted for file type, got:", val)
		}
	}
}

func TestMediaAttachment_PhotoHasDimensions(t *testing.T) {
	w := 1920
	h := 1080
	att := model.MediaAttachment{
		MediaID:          uuid.New().String(),
		Type:             "photo",
		MimeType:         "image/jpeg",
		URL:              "/media/test-id",
		ThumbnailURL:     "/media/test-id/thumbnail",
		SizeBytes:        245000,
		Width:            &w,
		Height:           &h,
		ProcessingStatus: "ready",
	}

	data, _ := json.Marshal(att)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if raw["width"] != float64(1920) {
		t.Errorf("BUG: width should be 1920, got %v", raw["width"])
	}
	if raw["url"] != "/media/test-id" {
		t.Errorf("BUG: url should be /media/test-id, got %v", raw["url"])
	}
	if raw["thumbnail_url"] != "/media/test-id/thumbnail" {
		t.Errorf("BUG: thumbnail_url should be present, got %v", raw["thumbnail_url"])
	}
}

// ---------------------------------------------------------------------------
// BUG-FINDING: SharedMedia endpoint
// ---------------------------------------------------------------------------

func TestListSharedMedia_NoAuth(t *testing.T) {
	chatID := uuid.New()

	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodGet, "/chats/"+chatID.String()+"/media", nil)
	// No X-User-ID header

	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for no auth on shared media, got %d", resp.StatusCode)
	}
}

func TestListSharedMedia_InvalidChatID(t *testing.T) {
	app := newMessageApp(&mockMessageStore{}, defaultMemberChatStore())
	req, _ := http.NewRequest(http.MethodGet, "/chats/not-a-uuid/media", nil)
	req.Header.Set("X-User-ID", uuid.New().String())

	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid chat UUID, got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ","
		}
		result += s
	}
	return result
}
