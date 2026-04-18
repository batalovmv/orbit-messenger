package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/bots/internal/model"
)

func TestInstallBot_Success(t *testing.T) {
	botID := uuid.New()
	chatID := uuid.New()
	userID := uuid.New()

	var captured *model.BotInstallation
	installStore := &mockInstallationStore{
		installFn: func(ctx context.Context, inst *model.BotInstallation) error {
			captured = inst
			return nil
		},
	}
	botStore := &mockBotStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			return &model.Bot{ID: id}, nil
		},
	}

	app := newBotHandlerTestAppWithInstall(botStore, nil, installStore)
	resp := doBotRequest(t, app, http.MethodPost, "/bots/"+botID.String()+"/install", map[string]any{
		"chat_id": chatID.String(),
		"scopes":  int64(1) | int64(4), // send + delete for example
	}, map[string]string{
		"X-User-ID":   userID.String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if captured == nil {
		t.Fatalf("expected Install to be invoked")
	}
	if captured.BotID != botID || captured.ChatID != chatID || captured.InstalledBy != userID {
		t.Fatalf("unexpected install args: %+v", captured)
	}
	if captured.Scopes != int64(1)|int64(4) {
		t.Fatalf("expected scopes %d, got %d", int64(1)|int64(4), captured.Scopes)
	}
	if !captured.IsActive {
		t.Fatalf("expected IsActive=true")
	}
}

func TestInstallBot_MemberForbidden(t *testing.T) {
	app := newBotHandlerTestAppWithInstall(nil, nil, nil)
	resp := doBotRequest(t, app, http.MethodPost, "/bots/"+uuid.New().String()+"/install", map[string]any{
		"chat_id": uuid.New().String(),
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestInstallBot_BotNotFound(t *testing.T) {
	botStore := &mockBotStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			return nil, nil
		},
	}

	app := newBotHandlerTestAppWithInstall(botStore, nil, nil)
	resp := doBotRequest(t, app, http.MethodPost, "/bots/"+uuid.New().String()+"/install", map[string]any{
		"chat_id": uuid.New().String(),
		"scopes":  int64(1),
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestInstallBot_AlreadyInstalled_Conflict(t *testing.T) {
	botStore := &mockBotStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
			return &model.Bot{ID: id}, nil
		},
	}
	installStore := &mockInstallationStore{
		installFn: func(ctx context.Context, inst *model.BotInstallation) error {
			return model.ErrBotAlreadyInstalled
		},
	}

	app := newBotHandlerTestAppWithInstall(botStore, nil, installStore)
	resp := doBotRequest(t, app, http.MethodPost, "/bots/"+uuid.New().String()+"/install", map[string]any{
		"chat_id": uuid.New().String(),
		"scopes":  int64(1),
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", resp.StatusCode)
	}
}

func TestInstallBot_InvalidChatID_BadRequest(t *testing.T) {
	app := newBotHandlerTestAppWithInstall(nil, nil, nil)
	resp := doBotRequest(t, app, http.MethodPost, "/bots/"+uuid.New().String()+"/install", map[string]any{
		"chat_id": "not-a-uuid",
		"scopes":  int64(1),
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestUninstallBot_Success(t *testing.T) {
	var uninstallCalled bool
	installStore := &mockInstallationStore{
		uninstallFn: func(ctx context.Context, botID, chatID uuid.UUID) error {
			uninstallCalled = true
			return nil
		},
	}

	app := newBotHandlerTestAppWithInstall(nil, nil, installStore)
	resp := doBotRequest(t, app, http.MethodDelete, "/bots/"+uuid.New().String()+"/install", map[string]any{
		"chat_id": uuid.New().String(),
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !uninstallCalled {
		t.Fatalf("expected Uninstall to be invoked")
	}
}

func TestUninstallBot_NotInstalled_NotFound(t *testing.T) {
	installStore := &mockInstallationStore{
		uninstallFn: func(ctx context.Context, botID, chatID uuid.UUID) error {
			return model.ErrBotNotInstalled
		},
	}

	app := newBotHandlerTestAppWithInstall(nil, nil, installStore)
	resp := doBotRequest(t, app, http.MethodDelete, "/bots/"+uuid.New().String()+"/install", map[string]any{
		"chat_id": uuid.New().String(),
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestListChatBots_Success(t *testing.T) {
	chatID := uuid.New()
	installs := []model.BotInstallation{
		{BotID: uuid.New(), ChatID: chatID, Scopes: 1, IsActive: true},
		{BotID: uuid.New(), ChatID: chatID, Scopes: 0, IsActive: true}, // legacy full-access
	}
	installStore := &mockInstallationStore{
		listByChatFn: func(ctx context.Context, id uuid.UUID) ([]model.BotInstallation, error) {
			if id != chatID {
				t.Fatalf("unexpected chat id: %s", id)
			}
			return installs, nil
		},
	}

	app := newBotHandlerTestAppWithInstall(nil, nil, installStore)
	resp := doBotRequest(t, app, http.MethodGet, "/chats/"+chatID.String()+"/bots", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body []map[string]any
	decodeJSON(t, resp.Body, &body)
	if len(body) != 2 {
		t.Fatalf("expected 2 installs, got %d", len(body))
	}
}

func TestListChatBots_MemberForbidden(t *testing.T) {
	app := newBotHandlerTestAppWithInstall(nil, nil, nil)
	resp := doBotRequest(t, app, http.MethodGet, "/chats/"+uuid.New().String()+"/bots", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
