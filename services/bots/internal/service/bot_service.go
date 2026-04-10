package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/bots/internal/model"
	"github.com/mst-corp/orbit/services/bots/internal/store"
)

type BotService struct {
	bots          store.BotStore
	tokens        store.TokenStore
	commands      store.CommandStore
	installations store.InstallationStore
	tokenSecret   string
}

type UpdateBotInput struct {
	Username         *string
	DisplayName      *string
	Description      *string
	ShortDescription *string
	IsInline         *bool
	WebhookURL       *string
	IsActive         *bool
}

func NewBotService(
	bots store.BotStore,
	tokens store.TokenStore,
	commands store.CommandStore,
	installations store.InstallationStore,
	tokenSecret string,
) *BotService {
	return &BotService{
		bots:          bots,
		tokens:        tokens,
		commands:      commands,
		installations: installations,
		tokenSecret:   tokenSecret,
	}
}

func (s *BotService) CreateBot(ctx context.Context, ownerID uuid.UUID, req model.CreateBotRequest) (*model.Bot, string, error) {
	username := strings.TrimSpace(req.Username)
	displayName := strings.TrimSpace(req.DisplayName)

	userID, err := s.bots.CreateBotUser(ctx, username, displayName)
	if err != nil {
		if errors.Is(err, model.ErrBotAlreadyExists) {
			return nil, "", apperror.Conflict("Bot already exists")
		}
		return nil, "", fmt.Errorf("create bot user: %w", err)
	}

	bot := &model.Bot{
		UserID:           userID,
		OwnerID:          ownerID,
		Username:         username,
		DisplayName:      displayName,
		Description:      normalizeNullableString(req.Description),
		ShortDescription: normalizeNullableString(req.ShortDescription),
		IsActive:         true,
	}

	if err := s.bots.Create(ctx, bot); err != nil {
		if errors.Is(err, model.ErrBotAlreadyExists) {
			return nil, "", apperror.Conflict("Bot already exists")
		}
		return nil, "", fmt.Errorf("create bot: %w", err)
	}

	rawToken, tokenHash, err := generateToken(bot.ID, s.tokenSecret)
	if err != nil {
		return nil, "", fmt.Errorf("generate bot token: %w", err)
	}

	if _, err := s.tokens.Create(ctx, bot.ID, tokenHash, tokenPrefix(rawToken)); err != nil {
		return nil, "", fmt.Errorf("store bot token: %w", err)
	}

	created, err := s.bots.GetByID(ctx, bot.ID)
	if err != nil {
		return nil, "", fmt.Errorf("get created bot: %w", err)
	}
	if created == nil {
		return nil, "", apperror.NotFound("Bot not found")
	}

	return created, rawToken, nil
}

func (s *BotService) GetBot(ctx context.Context, id uuid.UUID) (*model.Bot, error) {
	bot, err := s.bots.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get bot: %w", err)
	}
	if bot == nil {
		return nil, apperror.NotFound("Bot not found")
	}

	return bot, nil
}

func (s *BotService) ListBots(ctx context.Context, ownerID *uuid.UUID, limit, offset int) ([]model.Bot, int, error) {
	bots, total, err := s.bots.List(ctx, ownerID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list bots: %w", err)
	}

	return bots, total, nil
}

func (s *BotService) UpdateBot(ctx context.Context, actorID uuid.UUID, actorRole string, id uuid.UUID, input UpdateBotInput) (*model.Bot, error) {
	bot, err := s.bots.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get bot for update: %w", err)
	}
	if bot == nil {
		return nil, apperror.NotFound("Bot not found")
	}
	if !canManageBot(actorID, actorRole, bot) {
		return nil, apperror.Forbidden("Insufficient permissions")
	}

	if input.Username != nil {
		bot.Username = strings.TrimSpace(*input.Username)
	}
	if input.DisplayName != nil {
		bot.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.Description != nil {
		bot.Description = normalizeNullableString(*input.Description)
	}
	if input.ShortDescription != nil {
		bot.ShortDescription = normalizeNullableString(*input.ShortDescription)
	}
	if input.IsInline != nil {
		bot.IsInline = *input.IsInline
	}
	if input.WebhookURL != nil {
		bot.WebhookURL = normalizeNullableString(*input.WebhookURL)
	}
	if input.IsActive != nil {
		bot.IsActive = *input.IsActive
	}

	if err := s.bots.Update(ctx, bot); err != nil {
		if errors.Is(err, model.ErrBotAlreadyExists) {
			return nil, apperror.Conflict("Bot already exists")
		}
		if errors.Is(err, model.ErrBotNotFound) {
			return nil, apperror.NotFound("Bot not found")
		}
		return nil, fmt.Errorf("update bot: %w", err)
	}

	updated, err := s.bots.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get updated bot: %w", err)
	}
	if updated == nil {
		return nil, apperror.NotFound("Bot not found")
	}

	return updated, nil
}

func (s *BotService) DeleteBot(ctx context.Context, actorID uuid.UUID, actorRole string, id uuid.UUID) error {
	bot, err := s.bots.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get bot for delete: %w", err)
	}
	if bot == nil {
		return apperror.NotFound("Bot not found")
	}
	if bot.IsSystem {
		return apperror.Forbidden("System bots cannot be deleted")
	}
	if !canManageBot(actorID, actorRole, bot) {
		return apperror.Forbidden("Insufficient permissions")
	}

	if err := s.bots.Delete(ctx, id); err != nil {
		if errors.Is(err, model.ErrBotNotFound) {
			return apperror.NotFound("Bot not found")
		}
		return fmt.Errorf("delete bot: %w", err)
	}

	return nil
}

func (s *BotService) RotateToken(ctx context.Context, actorID uuid.UUID, actorRole string, botID uuid.UUID) (string, error) {
	bot, err := s.bots.GetByID(ctx, botID)
	if err != nil {
		return "", fmt.Errorf("get bot for token rotation: %w", err)
	}
	if bot == nil {
		return "", apperror.NotFound("Bot not found")
	}
	if !canManageBot(actorID, actorRole, bot) {
		return "", apperror.Forbidden("Insufficient permissions")
	}

	rawToken, tokenHash, err := generateToken(bot.ID, s.tokenSecret)
	if err != nil {
		return "", fmt.Errorf("generate bot token: %w", err)
	}

	if _, err := s.tokens.Create(ctx, bot.ID, tokenHash, tokenPrefix(rawToken)); err != nil {
		return "", fmt.Errorf("store rotated token: %w", err)
	}

	return rawToken, nil
}

func (s *BotService) ValidateToken(ctx context.Context, rawToken string) (*model.Bot, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, model.ErrInvalidToken
	}

	tokenHash := hashToken(rawToken, s.tokenSecret)
	token, err := s.tokens.GetByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, model.ErrTokenNotFound) {
			return nil, model.ErrInvalidToken
		}
		return nil, fmt.Errorf("get token by hash: %w", err)
	}

	if err := s.tokens.UpdateLastUsed(ctx, token.ID); err != nil {
		return nil, fmt.Errorf("update token last used: %w", err)
	}

	bot, err := s.bots.GetByID(ctx, token.BotID)
	if err != nil {
		return nil, fmt.Errorf("get bot by token: %w", err)
	}
	if bot == nil || !bot.IsActive {
		return nil, model.ErrInvalidToken
	}

	return bot, nil
}

func (s *BotService) SetCommands(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error {
	bot, err := s.bots.GetByID(ctx, botID)
	if err != nil {
		return fmt.Errorf("get bot for set commands: %w", err)
	}
	if bot == nil {
		return apperror.NotFound("Bot not found")
	}

	for i := range commands {
		commands[i].BotID = botID
	}

	if err := s.commands.SetCommands(ctx, botID, commands); err != nil {
		return fmt.Errorf("set bot commands: %w", err)
	}

	return nil
}

func (s *BotService) GetCommands(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error) {
	bot, err := s.bots.GetByID(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("get bot for commands: %w", err)
	}
	if bot == nil {
		return nil, apperror.NotFound("Bot not found")
	}

	commands, err := s.commands.GetCommands(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("get bot commands: %w", err)
	}

	return commands, nil
}

func (s *BotService) InstallBot(ctx context.Context, botID, chatID, installedBy uuid.UUID, scopes int64) error {
	bot, err := s.bots.GetByID(ctx, botID)
	if err != nil {
		return fmt.Errorf("get bot for install: %w", err)
	}
	if bot == nil {
		return apperror.NotFound("Bot not found")
	}

	err = s.installations.Install(ctx, &model.BotInstallation{
		BotID:       botID,
		ChatID:      chatID,
		InstalledBy: installedBy,
		Scopes:      scopes,
		IsActive:    true,
	})
	if err != nil {
		if errors.Is(err, model.ErrBotAlreadyInstalled) {
			return apperror.Conflict("Bot already installed in chat")
		}
		if errors.Is(err, model.ErrBotNotFound) {
			return apperror.NotFound("Bot not found")
		}
		return fmt.Errorf("install bot: %w", err)
	}

	return nil
}

func (s *BotService) UninstallBot(ctx context.Context, botID, chatID uuid.UUID) error {
	err := s.installations.Uninstall(ctx, botID, chatID)
	if err != nil {
		if errors.Is(err, model.ErrBotNotFound) {
			return apperror.NotFound("Bot not found")
		}
		if errors.Is(err, model.ErrBotNotInstalled) {
			return apperror.NotFound("Bot is not installed in this chat")
		}
		return fmt.Errorf("uninstall bot: %w", err)
	}

	return nil
}

func (s *BotService) ListChatBots(ctx context.Context, chatID uuid.UUID) ([]model.BotInstallation, error) {
	installations, err := s.installations.ListByChat(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("list chat bots: %w", err)
	}

	return installations, nil
}

func (s *BotService) GetBotByUserID(ctx context.Context, userID uuid.UUID) (*model.Bot, error) {
	bot, err := s.bots.GetByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get bot by user id: %w", err)
	}
	if bot == nil {
		return nil, apperror.NotFound("Bot not found")
	}

	return bot, nil
}

func (s *BotService) IsBotInstalled(ctx context.Context, botID, chatID uuid.UUID) (bool, error) {
	inst, err := s.installations.GetByBotAndChat(ctx, botID, chatID)
	if err != nil {
		return false, fmt.Errorf("get bot installation: %w", err)
	}
	if inst == nil {
		return false, nil
	}

	return inst.IsActive, nil
}

func (s *BotService) SetWebhook(ctx context.Context, botID uuid.UUID, webhookURL, secretHash *string) (*model.Bot, error) {
	bot, err := s.bots.GetByID(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("get bot for webhook update: %w", err)
	}
	if bot == nil {
		return nil, apperror.NotFound("Bot not found")
	}

	bot.WebhookURL = webhookURL
	bot.WebhookSecretHash = secretHash

	if err := s.bots.Update(ctx, bot); err != nil {
		if errors.Is(err, model.ErrBotNotFound) {
			return nil, apperror.NotFound("Bot not found")
		}
		if errors.Is(err, model.ErrBotAlreadyExists) {
			return nil, apperror.Conflict("Bot already exists")
		}
		return nil, fmt.Errorf("set bot webhook: %w", err)
	}

	updated, err := s.bots.GetByID(ctx, botID)
	if err != nil {
		return nil, fmt.Errorf("get bot after webhook update: %w", err)
	}
	if updated == nil {
		return nil, apperror.NotFound("Bot not found")
	}

	return updated, nil
}

func canManageBot(actorID uuid.UUID, actorRole string, bot *model.Bot) bool {
	if bot == nil {
		return false
	}
	if actorID == bot.OwnerID {
		return true
	}

	return permissions.HasSysPermission(actorRole, permissions.SysManageBots)
}

func normalizeNullableString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func tokenPrefix(raw string) string {
	if len(raw) <= 20 {
		return raw
	}
	return raw[:20]
}

func hashToken(raw, secret string) string {
	if secret == "" {
		sum := sha256.Sum256([]byte(raw))
		return hex.EncodeToString(sum[:])
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil))
}

func generateToken(botID uuid.UUID, secret string) (string, string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", "", fmt.Errorf("read random bytes: %w", err)
	}

	raw := fmt.Sprintf("bot_%s_%s", botID.String()[:8], hex.EncodeToString(random))
	return raw, hashToken(raw, secret), nil
}
