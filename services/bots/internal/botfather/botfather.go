package botfather

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/bots/internal/client"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/service"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

const (
	BotFatherUsername = "botfather"
	BotFatherDisplay = "BotFather"
	BotFatherDesc    = "Я помогу создать и настроить ботов для Orbit."
	MaxBotsPerUser   = 20
)

// BotFather is the system bot that manages other bots via DM conversation.
type BotFather struct {
	botID  uuid.UUID // bots table ID
	userID uuid.UUID // users table ID (the bot's user account)

	svc              *service.BotService
	botStore         store.BotStore
	tokenStore       store.TokenStore
	msgClient        *client.MessagingClient
	integrationsClient *client.IntegrationsClient
	state            *RedisStateStore
	encryptionKey    []byte
	logger           *slog.Logger
}

// Provision ensures the BotFather bot exists in the database.
// Called once at startup. If the bot doesn't exist, it creates it.
// Handles partial state (user exists but bot record doesn't).
func Provision(
	ctx context.Context,
	svc *service.BotService,
	botStore store.BotStore,
	tokenStore store.TokenStore,
	cmdStore store.CommandStore,
	msgClient *client.MessagingClient,
	integrationsClient *client.IntegrationsClient,
	stateStore *RedisStateStore,
	encryptionKey []byte,
	logger *slog.Logger,
) (*BotFather, error) {
	existing, err := botStore.GetByUsername(ctx, BotFatherUsername)
	if err != nil {
		return nil, fmt.Errorf("check existing botfather: %w", err)
	}

	var botID, userID uuid.UUID

	if existing != nil {
		botID = existing.ID
		userID = existing.UserID

		// Ensure is_system flag is set (idempotent)
		if !existing.IsSystem {
			existing.IsSystem = true
			if err := botStore.Update(ctx, existing); err != nil {
				logger.Warn("failed to set botfather is_system flag", "error", err)
			}
		}

		logger.Info("botfather already provisioned", "bot_id", botID, "user_id", userID)
	} else {
		// Step 1: Create bot user (or find orphaned one from a previous partial failure)
		userID, err = botStore.CreateBotUser(ctx, BotFatherUsername, BotFatherDisplay)
		if err != nil {
			if err == model.ErrBotAlreadyExists {
				// Orphaned user exists from a previous failed provisioning — recover
				logger.Warn("botfather user exists but bot record missing, recovering orphan")
				userID, err = botStore.GetBotUserIDByUsername(ctx, BotFatherUsername)
				if err != nil {
					return nil, fmt.Errorf("recover orphaned botfather user: %w", err)
				}
			} else {
				return nil, fmt.Errorf("create botfather user: %w", err)
			}
		}

		// Step 2: Create bot record (self-owned: owner_id = user_id)
		desc := BotFatherDesc
		bot := &model.Bot{
			UserID:      userID,
			OwnerID:     userID, // self-owned, avoids FK issues
			Username:    BotFatherUsername,
			DisplayName: BotFatherDisplay,
			Description: &desc,
			IsSystem:    true,
			IsActive:    true,
		}
		if err := botStore.Create(ctx, bot); err != nil {
			return nil, fmt.Errorf("create botfather bot record: %w", err)
		}
		botID = bot.ID

		// Step 3: Generate token (required by the system even though BotFather doesn't use it)
		rawToken, tokenHash, err := service.GenerateToken(bot.ID, svc.TokenSecret())
		if err != nil {
			return nil, fmt.Errorf("generate botfather token: %w", err)
		}
		if _, err := tokenStore.Create(ctx, bot.ID, tokenHash, service.TokenPrefix(rawToken)); err != nil {
			return nil, fmt.Errorf("store botfather token: %w", err)
		}

		// Step 4: Register commands
		commands := []model.BotCommand{
			{BotID: botID, Command: "start", Description: "Начать работу с BotFather"},
			{BotID: botID, Command: "help", Description: "Список команд"},
			{BotID: botID, Command: "newbot", Description: "Создать нового бота"},
			{BotID: botID, Command: "mybots", Description: "Управление моими ботами"},
			{BotID: botID, Command: "setname", Description: "Изменить имя бота"},
			{BotID: botID, Command: "setdescription", Description: "Изменить описание бота"},
			{BotID: botID, Command: "setcommands", Description: "Задать команды бота"},
			{BotID: botID, Command: "setwebhook", Description: "Настроить вебхук"},
			{BotID: botID, Command: "token", Description: "Управление токеном"},
			{BotID: botID, Command: "deletebot", Description: "Удалить бота"},
			{BotID: botID, Command: "setintegration", Description: "Привязать бота к коннектору"},
			{BotID: botID, Command: "cancel", Description: "Отменить текущую операцию"},
		}
		if err := svc.SetCommands(ctx, botID, commands); err != nil {
			return nil, fmt.Errorf("register botfather commands: %w", err)
		}

		logger.Info("botfather provisioned successfully", "bot_id", botID, "user_id", userID)
	}

	return &BotFather{
		botID:              botID,
		userID:             userID,
		svc:                svc,
		botStore:           botStore,
		tokenStore:         tokenStore,
		msgClient:          msgClient,
		integrationsClient: integrationsClient,
		state:              stateStore,
		encryptionKey:      encryptionKey,
		logger:             logger,
	}, nil
}

// BotID returns the BotFather's bot table ID.
func (bf *BotFather) BotID() uuid.UUID { return bf.botID }

// UserID returns the BotFather's user account ID.
func (bf *BotFather) UserID() uuid.UUID { return bf.userID }

