// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

// fileBotService extends rateLimitBotService with a configurable
// IsBotInstalled answer per chat.
type fileBotService struct {
	rateLimitBotService
	installedChats map[uuid.UUID]bool
}

func (s *fileBotService) IsBotInstalled(ctx context.Context, botID, chatID uuid.UUID) (bool, error) {
	if s.installedChats == nil {
		return true, nil
	}
	return s.installedChats[chatID], nil
}

type fileTestSetup struct {
	app         *fiber.App
	codec       *FileIDCodec
	bot         *model.Bot
	mediaServer *httptest.Server
	msgServer   *httptest.Server
	msgBodies   *[]map[string]any
	svc         *fileBotService
}

func newFileTestSetup(t *testing.T, mediaSizeBytes int64, installedChats map[uuid.UUID]bool) *fileTestSetup {
	t.Helper()

	mediaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "00000000-0000-0000-0000-000000000000",
			"type": "file",
			"mime_type": "application/pdf",
			"original_filename": "report.pdf",
			"size_bytes": ` + jsonInt(mediaSizeBytes) + `
		}`))
	}))
	t.Cleanup(mediaServer.Close)

	var mu sync.Mutex
	msgBodies := make([]map[string]any, 0)
	msgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		msgBodies = append(msgBodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"00000000-0000-0000-0000-000000000000","chat_id":"00000000-0000-0000-0000-000000000000","content":"ok","type":"file","sequence_number":1,"created_at":"2024-01-01T00:00:00Z"}`))
	}))
	t.Cleanup(msgServer.Close)

	bot := &model.Bot{
		ID:       uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		UserID:   uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"),
		IsActive: true,
	}
	svc := &fileBotService{
		rateLimitBotService: rateLimitBotService{bots: map[string]*model.Bot{
			"bot-file": bot,
		}},
		installedChats: installedChats,
	}
	codec := NewFileIDCodec([]byte("file-handler-test-secret"))
	h := &BotAPIHandler{
		svc:         svc,
		msgClient:   client.NewMessagingClient(msgServer.URL, "internal-test-token"),
		mediaClient: client.NewMediaClient(mediaServer.URL, "internal-test-token"),
		fileIDCodec: codec,
		logger:      nil,
	}
	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	group := app.Group("/bot/:token", TokenAuthMiddleware(h.svc))
	h.Register(group)
	return &fileTestSetup{
		app:         app,
		codec:       codec,
		bot:         bot,
		mediaServer: mediaServer,
		msgServer:   msgServer,
		msgBodies:   &msgBodies,
		svc:         svc,
	}
}

func jsonInt(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestGetFile_ReturnsPresignedFileMetadata(t *testing.T) {
	chatID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	media := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	setup := newFileTestSetup(t, 12345, map[uuid.UUID]bool{chatID: true})
	fileID := setup.codec.Encode(media, chatID, setup.bot.ID)

	req := httptest.NewRequest(http.MethodGet, "/bot/bot-file/getFile?file_id="+fileID, nil)
	resp, err := setup.app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var decoded struct {
		OK     bool   `json:"ok"`
		Result APIFile `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&decoded)
	if !decoded.OK {
		t.Fatalf("ok=false")
	}
	if decoded.Result.FileID != fileID {
		t.Fatalf("file_id mismatch: %q vs %q", decoded.Result.FileID, fileID)
	}
	if decoded.Result.FileSize != 12345 {
		t.Fatalf("file_size=%d", decoded.Result.FileSize)
	}
	if decoded.Result.FilePath != fileID {
		t.Fatalf("file_path=%q (expected file_id echoed)", decoded.Result.FilePath)
	}
	if decoded.Result.FileUniqueID == "" {
		t.Fatal("empty file_unique_id")
	}
}

func TestGetFile_BotWithoutChatAccess_403(t *testing.T) {
	chatID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	media := uuid.New()
	// Bot is NOT installed in chatID.
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{chatID: false})
	fileID := setup.codec.Encode(media, chatID, setup.bot.ID)

	req := httptest.NewRequest(http.MethodGet, "/bot/bot-file/getFile?file_id="+fileID, nil)
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
}

func TestGetFile_TamperedFileID_400(t *testing.T) {
	setup := newFileTestSetup(t, 100, nil)
	req := httptest.NewRequest(http.MethodGet, "/bot/bot-file/getFile?file_id=garbage", nil)
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400", resp.StatusCode)
	}
}

func TestSendDocument_ByFileID_ReusesWithoutReupload(t *testing.T) {
	chatID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	media := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	destChat := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{chatID: true, destChat: true})
	fileID := setup.codec.Encode(media, chatID, setup.bot.ID)

	body, _ := json.Marshal(map[string]any{
		"chat_id":  destChat.String(),
		"document": fileID,
		"caption":  "see attached",
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendDocument", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if len(*setup.msgBodies) == 0 {
		t.Fatal("expected one downstream messaging call")
	}
	got := (*setup.msgBodies)[0]
	if got["content"] != "see attached" {
		t.Fatalf("content=%v", got["content"])
	}
	mediaIDs, ok := got["media_ids"].([]any)
	if !ok || len(mediaIDs) != 1 || mediaIDs[0] != media.String() {
		t.Fatalf("media_ids=%v, want [%s]", got["media_ids"], media.String())
	}
}

func TestSendDocument_ByFileID_FromBotInDifferentChat_Forbidden(t *testing.T) {
	sourceChat := uuid.MustParse("99999999-9999-9999-9999-999999999999")
	destChat := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	media := uuid.New()
	// Bot is NOT installed in source chat — file_id was minted but bot left.
	setup := newFileTestSetup(t, 100, map[uuid.UUID]bool{
		sourceChat: false,
		destChat:   true,
	})
	fileID := setup.codec.Encode(media, sourceChat, setup.bot.ID)

	body, _ := json.Marshal(map[string]any{
		"chat_id":  destChat.String(),
		"document": fileID,
	})
	req := httptest.NewRequest(http.MethodPost, "/bot/bot-file/sendDocument", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := setup.app.Test(req, -1)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want 403", resp.StatusCode)
	}
}
