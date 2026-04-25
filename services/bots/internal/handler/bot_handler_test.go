// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/service"
)

func newBotHandlerTestApp(botStore *mockBotStore, tokenStore *mockTokenStore) *fiber.App {
	return newBotHandlerTestAppWithInstall(botStore, tokenStore, nil)
}

func newBotHandlerTestAppWithInstall(botStore *mockBotStore, tokenStore *mockTokenStore, installStore *mockInstallationStore) *fiber.App {
	if botStore == nil {
		botStore = &mockBotStore{}
	}
	if tokenStore == nil {
		tokenStore = &mockTokenStore{}
	}
	if installStore == nil {
		installStore = &mockInstallationStore{}
	}

	svc := service.NewBotService(botStore, tokenStore, &mockCommandStore{}, installStore, "test-secret")
	h := NewBotHandler(svc, slog.Default())

	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	h.Register(app)
	return app
}

func TestCreateBot_Success(t *testing.T) {
	ownerID := uuid.New()
	userID := uuid.New()
	botID := uuid.New()
	now := time.Now().UTC()

	var created *model.Bot
	botStore := &mockBotStore{
		createBotUserFn: func(ctx context.Context, username, displayName string) (uuid.UUID, error) {
			return userID, nil
		},
		createFn: func(ctx context.Context, bot *model.Bot) error {
			created = bot
			bot.ID = botID
			bot.CreatedAt = now
			bot.UpdatedAt = now
			return nil
		},
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			if id != botID {
				t.Fatalf("unexpected bot id: %s", id)
			}
			return &model.Bot{
				ID:          botID,
				UserID:      userID,
				OwnerID:     ownerID,
				Username:    "orbit_bot",
				DisplayName: "Orbit Bot",
				IsActive:    true,
				CreatedAt:   now,
				UpdatedAt:   now,
			}, nil
		},
	}

	app := newBotHandlerTestApp(botStore, &mockTokenStore{})
	resp := doBotRequest(t, app, http.MethodPost, "/bots", map[string]any{
		"username":     "orbit_bot",
		"display_name": "Orbit Bot",
	}, map[string]string{
		"X-User-ID":   ownerID.String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if created == nil {
		t.Fatal("expected bot to be created")
	}

	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if token, ok := body["token"].(string); !ok || !strings.HasPrefix(token, "bot_") {
		t.Fatalf("expected bot token in response, got %#v", body["token"])
	}
}

func TestCreateBot_Unauthorized(t *testing.T) {
	app := newBotHandlerTestApp(nil, nil)
	resp := doBotRequest(t, app, http.MethodPost, "/bots", map[string]any{
		"username":     "orbit_bot",
		"display_name": "Orbit Bot",
	}, nil)

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCreateBot_Forbidden(t *testing.T) {
	app := newBotHandlerTestApp(nil, nil)
	resp := doBotRequest(t, app, http.MethodPost, "/bots", map[string]any{
		"username":     "orbit_bot",
		"display_name": "Orbit Bot",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestCreateBot_ValidationError(t *testing.T) {
	app := newBotHandlerTestApp(nil, nil)
	resp := doBotRequest(t, app, http.MethodPost, "/bots", map[string]any{
		"display_name": "Orbit Bot",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetBot_Success(t *testing.T) {
	botID := uuid.New()
	botStore := &mockBotStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			return &model.Bot{
				ID:          botID,
				UserID:      uuid.New(),
				OwnerID:     uuid.New(),
				Username:    "orbit_bot",
				DisplayName: "Orbit Bot",
				IsActive:    true,
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			}, nil
		},
	}

	app := newBotHandlerTestApp(botStore, nil)
	resp := doBotRequest(t, app, http.MethodGet, "/bots/"+botID.String(), nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetBot_NotFound(t *testing.T) {
	botID := uuid.New()
	botStore := &mockBotStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			return nil, nil
		},
	}

	app := newBotHandlerTestApp(botStore, nil)
	resp := doBotRequest(t, app, http.MethodGet, "/bots/"+botID.String(), nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestDeleteBot_Success(t *testing.T) {
	botID := uuid.New()
	deleted := false
	botStore := &mockBotStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			return &model.Bot{
				ID:          botID,
				UserID:      uuid.New(),
				OwnerID:     uuid.New(),
				Username:    "orbit_bot",
				DisplayName: "Orbit Bot",
				IsActive:    true,
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			}, nil
		},
		deleteFn: func(ctx context.Context, id uuid.UUID) error {
			deleted = true
			return nil
		},
	}

	app := newBotHandlerTestApp(botStore, nil)
	resp := doBotRequest(t, app, http.MethodDelete, "/bots/"+botID.String(), nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !deleted {
		t.Fatal("expected delete to be called")
	}
}

func TestDeleteBot_SystemBot(t *testing.T) {
	botID := uuid.New()
	botStore := &mockBotStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			return &model.Bot{
				ID:          botID,
				UserID:      uuid.New(),
				OwnerID:     uuid.New(),
				Username:    "orbit_system",
				DisplayName: "Orbit System",
				IsSystem:    true,
				IsActive:    true,
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			}, nil
		},
	}

	app := newBotHandlerTestApp(botStore, nil)
	resp := doBotRequest(t, app, http.MethodDelete, "/bots/"+botID.String(), nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

type fakeBotFatherProvisioner struct {
	chatID    uuid.UUID
	botID     uuid.UUID
	err       error
	gotUserID uuid.UUID
}

func (f *fakeBotFatherProvisioner) EnsureChat(_ context.Context, userID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	f.gotUserID = userID
	return f.chatID, f.botID, f.err
}

func TestEnsureBotFatherChat_Success(t *testing.T) {
	chatID := uuid.New()
	botID := uuid.New()
	prov := &fakeBotFatherProvisioner{chatID: chatID, botID: botID}

	app := newBotHandlerTestApp(nil, nil)
	// re-register handler with provisioner attached
	svc := service.NewBotService(&mockBotStore{}, &mockTokenStore{}, &mockCommandStore{}, &mockInstallationStore{}, "test-secret")
	h := NewBotHandler(svc, slog.Default()).WithBotFatherChatProvisioner(prov)
	app = fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	h.Register(app)

	userID := uuid.New()
	resp := doBotRequest(t, app, http.MethodPost, "/system/botfather/ensure-chat", nil, map[string]string{
		"X-User-ID":   userID.String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["chat_id"] != chatID.String() {
		t.Fatalf("expected chat_id=%s, got %#v", chatID, body["chat_id"])
	}
	if body["system_bot_id"] != botID.String() {
		t.Fatalf("expected system_bot_id=%s, got %#v", botID, body["system_bot_id"])
	}
	if prov.gotUserID != userID {
		t.Fatalf("provisioner called with wrong user id: %s vs %s", prov.gotUserID, userID)
	}
}

func TestEnsureBotFatherChat_NoProvisioner_500(t *testing.T) {
	app := newBotHandlerTestApp(nil, nil) // no provisioner attached
	resp := doBotRequest(t, app, http.MethodPost, "/system/botfather/ensure-chat", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", resp.StatusCode)
	}
}

func TestEnsureBotFatherChat_Unauthorized(t *testing.T) {
	prov := &fakeBotFatherProvisioner{chatID: uuid.New(), botID: uuid.New()}
	svc := service.NewBotService(&mockBotStore{}, &mockTokenStore{}, &mockCommandStore{}, &mockInstallationStore{}, "test-secret")
	h := NewBotHandler(svc, slog.Default()).WithBotFatherChatProvisioner(prov)
	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	h.Register(app)

	resp := doBotRequest(t, app, http.MethodPost, "/system/botfather/ensure-chat", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestCheckBotUsername_Available(t *testing.T) {
	botStore := &mockBotStore{
		getByUsernameFn: func(ctx context.Context, username string) (*model.Bot, error) {
			return nil, nil
		},
	}

	app := newBotHandlerTestApp(botStore, nil)
	resp := doBotRequest(t, app, http.MethodGet, "/bots/check-username?username=fresh_bot", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["available"] != true || body["valid"] != true {
		t.Fatalf("expected available=true valid=true, got %#v", body)
	}
}

func TestCheckBotUsername_Taken(t *testing.T) {
	taken := "datadog_bot"
	botStore := &mockBotStore{
		getByUsernameFn: func(ctx context.Context, username string) (*model.Bot, error) {
			if username != taken {
				t.Fatalf("unexpected lookup for %q", username)
			}
			return &model.Bot{Username: taken}, nil
		},
	}

	app := newBotHandlerTestApp(botStore, nil)
	resp := doBotRequest(t, app, http.MethodGet, "/bots/check-username?username="+taken, nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["available"] != false || body["valid"] != true || body["reason"] != "taken" {
		t.Fatalf("expected available=false valid=true reason=taken, got %#v", body)
	}
}

func TestCheckBotUsername_InvalidFormat(t *testing.T) {
	app := newBotHandlerTestApp(nil, nil)
	resp := doBotRequest(t, app, http.MethodGet, "/bots/check-username?username=ab", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]any
	decodeJSON(t, resp.Body, &body)
	if body["valid"] != false {
		t.Fatalf("expected valid=false, got %#v", body)
	}
}

func TestCheckBotUsername_Forbidden(t *testing.T) {
	app := newBotHandlerTestApp(nil, nil)
	resp := doBotRequest(t, app, http.MethodGet, "/bots/check-username?username=ok_bot", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func doBotRequest(t *testing.T, app *fiber.App, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func decodeJSON(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("decode json: %v", err)
	}
}
