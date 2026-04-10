package handler

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

type mockBotStore struct {
	createFn        func(ctx context.Context, bot *model.Bot) error
	getByIDFn       func(ctx context.Context, id uuid.UUID) (*model.Bot, error)
	getByUserIDFn   func(ctx context.Context, userID uuid.UUID) (*model.Bot, error)
	getByUsernameFn func(ctx context.Context, username string) (*model.Bot, error)
	listFn          func(ctx context.Context, ownerID *uuid.UUID, limit int, offset int) ([]model.Bot, int, error)
	updateFn        func(ctx context.Context, bot *model.Bot) error
	deleteFn        func(ctx context.Context, id uuid.UUID) error
	createBotUserFn func(ctx context.Context, username, displayName string) (uuid.UUID, error)
}

func (m *mockBotStore) Create(ctx context.Context, bot *model.Bot) error {
	if m.createFn != nil {
		return m.createFn(ctx, bot)
	}
	bot.ID = uuid.New()
	bot.CreatedAt = time.Now()
	bot.UpdatedAt = bot.CreatedAt
	return nil
}

func (m *mockBotStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockBotStore) GetByUserID(ctx context.Context, userID uuid.UUID) (*model.Bot, error) {
	if m.getByUserIDFn != nil {
		return m.getByUserIDFn(ctx, userID)
	}
	return nil, nil
}

func (m *mockBotStore) GetByUsername(ctx context.Context, username string) (*model.Bot, error) {
	if m.getByUsernameFn != nil {
		return m.getByUsernameFn(ctx, username)
	}
	return nil, nil
}

func (m *mockBotStore) List(ctx context.Context, ownerID *uuid.UUID, limit int, offset int) ([]model.Bot, int, error) {
	if m.listFn != nil {
		return m.listFn(ctx, ownerID, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockBotStore) Update(ctx context.Context, bot *model.Bot) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, bot)
	}
	return nil
}

func (m *mockBotStore) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockBotStore) CreateBotUser(ctx context.Context, username, displayName string) (uuid.UUID, error) {
	if m.createBotUserFn != nil {
		return m.createBotUserFn(ctx, username, displayName)
	}
	return uuid.New(), nil
}

type mockTokenStore struct {
	createFn          func(ctx context.Context, botID uuid.UUID, tokenHash, tokenPrefix string) (*model.BotToken, error)
	getByHashFn       func(ctx context.Context, tokenHash string) (*model.BotToken, error)
	revokeAllForBotFn func(ctx context.Context, botID uuid.UUID) error
	updateLastUsedFn  func(ctx context.Context, tokenID uuid.UUID) error
}

func (m *mockTokenStore) Create(ctx context.Context, botID uuid.UUID, tokenHash, tokenPrefix string) (*model.BotToken, error) {
	if m.createFn != nil {
		return m.createFn(ctx, botID, tokenHash, tokenPrefix)
	}
	return &model.BotToken{
		ID:          uuid.New(),
		BotID:       botID,
		TokenPrefix: tokenPrefix,
		IsActive:    true,
		CreatedAt:   time.Now(),
	}, nil
}

func (m *mockTokenStore) GetByHash(ctx context.Context, tokenHash string) (*model.BotToken, error) {
	if m.getByHashFn != nil {
		return m.getByHashFn(ctx, tokenHash)
	}
	return nil, nil
}

func (m *mockTokenStore) RevokeAllForBot(ctx context.Context, botID uuid.UUID) error {
	if m.revokeAllForBotFn != nil {
		return m.revokeAllForBotFn(ctx, botID)
	}
	return nil
}

func (m *mockTokenStore) UpdateLastUsed(ctx context.Context, tokenID uuid.UUID) error {
	if m.updateLastUsedFn != nil {
		return m.updateLastUsedFn(ctx, tokenID)
	}
	return nil
}

type mockCommandStore struct {
	setCommandsFn     func(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error
	getCommandsFn     func(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error)
	deleteAllForBotFn func(ctx context.Context, botID uuid.UUID) error
}

func (m *mockCommandStore) SetCommands(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error {
	if m.setCommandsFn != nil {
		return m.setCommandsFn(ctx, botID, commands)
	}
	return nil
}

func (m *mockCommandStore) GetCommands(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error) {
	if m.getCommandsFn != nil {
		return m.getCommandsFn(ctx, botID)
	}
	return nil, nil
}

func (m *mockCommandStore) DeleteAllForBot(ctx context.Context, botID uuid.UUID) error {
	if m.deleteAllForBotFn != nil {
		return m.deleteAllForBotFn(ctx, botID)
	}
	return nil
}

type mockInstallationStore struct {
	installFn                  func(ctx context.Context, inst *model.BotInstallation) error
	uninstallFn                func(ctx context.Context, botID, chatID uuid.UUID) error
	getByBotAndChatFn          func(ctx context.Context, botID, chatID uuid.UUID) (*model.BotInstallation, error)
	listByChatFn               func(ctx context.Context, chatID uuid.UUID) ([]model.BotInstallation, error)
	listByBotFn                func(ctx context.Context, botID uuid.UUID) ([]model.BotInstallation, error)
	listChatsWithWebhookBotsFn func(ctx context.Context, chatID uuid.UUID) ([]store.WebhookBotInfo, error)
}

func (m *mockInstallationStore) Install(ctx context.Context, inst *model.BotInstallation) error {
	if m.installFn != nil {
		return m.installFn(ctx, inst)
	}
	return nil
}

func (m *mockInstallationStore) Uninstall(ctx context.Context, botID, chatID uuid.UUID) error {
	if m.uninstallFn != nil {
		return m.uninstallFn(ctx, botID, chatID)
	}
	return nil
}

func (m *mockInstallationStore) GetByBotAndChat(ctx context.Context, botID, chatID uuid.UUID) (*model.BotInstallation, error) {
	if m.getByBotAndChatFn != nil {
		return m.getByBotAndChatFn(ctx, botID, chatID)
	}
	return nil, nil
}

func (m *mockInstallationStore) ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.BotInstallation, error) {
	if m.listByChatFn != nil {
		return m.listByChatFn(ctx, chatID)
	}
	return nil, nil
}

func (m *mockInstallationStore) ListByBot(ctx context.Context, botID uuid.UUID) ([]model.BotInstallation, error) {
	if m.listByBotFn != nil {
		return m.listByBotFn(ctx, botID)
	}
	return nil, nil
}

func (m *mockInstallationStore) ListChatsWithWebhookBots(ctx context.Context, chatID uuid.UUID) ([]store.WebhookBotInfo, error) {
	if m.listChatsWithWebhookBotsFn != nil {
		return m.listChatsWithWebhookBotsFn(ctx, chatID)
	}
	return nil, nil
}
