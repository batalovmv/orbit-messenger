// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestSendMediaGroup_ReturnsArrayOfMessages(t *testing.T) {
	sourceChat := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	destChat := uuid.MustParse("22222222-3333-4444-5555-666666666666")
	media1 := uuid.New()
	media2 := uuid.New()
	media3 := uuid.New()
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{sourceChat: true, destChat: true})

	body, _ := json.Marshal(map[string]any{
		"chat_id": destChat.String(),
		"media": []map[string]any{
			{"type": "photo", "media": setup.codec.Encode(media1, sourceChat, setup.bot.ID), "caption": "album"},
			{"type": "photo", "media": setup.codec.Encode(media2, sourceChat, setup.bot.ID)},
			{"type": "photo", "media": setup.codec.Encode(media3, sourceChat, setup.bot.ID)},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendMediaGroup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := setup.app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	var decoded struct {
		OK     bool             `json:"ok"`
		Result []map[string]any `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	if !decoded.OK {
		t.Fatal("ok=false")
	}
	if len(decoded.Result) != 1 {
		t.Fatalf("result has %d items, expected exactly 1 (single-message album)", len(decoded.Result))
	}

	if len(*setup.msgBodies) != 1 {
		t.Fatalf("expected 1 messaging call, got %d", len(*setup.msgBodies))
	}
	got := (*setup.msgBodies)[0]
	if got["type"] != "photo" {
		t.Fatalf("type=%v, want photo", got["type"])
	}
	if got["content"] != "album" {
		t.Fatalf("content=%v, want first item caption 'album'", got["content"])
	}
	mediaIDs, ok := got["media_ids"].([]any)
	if !ok || len(mediaIDs) != 3 {
		t.Fatalf("media_ids=%v, expected 3 entries", got["media_ids"])
	}
}

func TestSendMediaGroup_ValidatesMin2Max10(t *testing.T) {
	sourceChat := uuid.New()
	destChat := uuid.New()
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{sourceChat: true, destChat: true})

	// 1 item: too few.
	tooFew, _ := json.Marshal(map[string]any{
		"chat_id": destChat.String(),
		"media": []map[string]any{
			{"type": "photo", "media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID)},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendMediaGroup", bytes.NewReader(tooFew))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("1 item: status=%d, want 400", resp.StatusCode)
	}

	// 11 items: too many.
	mediaItems := make([]map[string]any, 11)
	for i := range mediaItems {
		mediaItems[i] = map[string]any{
			"type":  "photo",
			"media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID),
		}
	}
	tooMany, _ := json.Marshal(map[string]any{
		"chat_id": destChat.String(),
		"media":   mediaItems,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendMediaGroup", bytes.NewReader(tooMany))
	req2.Header.Set("Content-Type", "application/json")
	resp2, _ := setup.app.Test(req2, -1)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("11 items: status=%d, want 400", resp2.StatusCode)
	}
}

func TestSendMediaGroup_MixedTypes(t *testing.T) {
	sourceChat := uuid.New()
	destChat := uuid.New()
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{sourceChat: true, destChat: true})

	body, _ := json.Marshal(map[string]any{
		"chat_id": destChat.String(),
		"media": []map[string]any{
			{"type": "video", "media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID)},
			{"type": "document", "media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID)},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendMediaGroup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	got := (*setup.msgBodies)[0]
	// type comes from the first item.
	if got["type"] != "video" {
		t.Fatalf("type=%v, want video (from first item)", got["type"])
	}
}

func TestSendMediaGroup_InvalidType_400(t *testing.T) {
	sourceChat := uuid.New()
	destChat := uuid.New()
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{sourceChat: true, destChat: true})

	body, _ := json.Marshal(map[string]any{
		"chat_id": destChat.String(),
		"media": []map[string]any{
			{"type": "sticker", "media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID)},
			{"type": "photo", "media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID)},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendMediaGroup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestSendMediaGroup_BotNotInstalledInSourceChat_403(t *testing.T) {
	sourceChat := uuid.New()
	destChat := uuid.New()
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{
		sourceChat: false, // bot left/never joined the source chat
		destChat:   true,
	})

	body, _ := json.Marshal(map[string]any{
		"chat_id": destChat.String(),
		"media": []map[string]any{
			{"type": "photo", "media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID)},
			{"type": "photo", "media": setup.codec.Encode(uuid.New(), sourceChat, setup.bot.ID)},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendMediaGroup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
}
