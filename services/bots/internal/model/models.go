package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	ScopePostMessages     int64 = 1 << 0
	ScopeReadCommands     int64 = 1 << 1
	ScopeReceiveCallbacks int64 = 1 << 2
	ScopeReadMessages     int64 = 1 << 3
)

var (
	ErrBotNotFound         = errors.New("bot not found")
	ErrBotAlreadyExists    = errors.New("bot already exists")
	ErrTokenNotFound       = errors.New("token not found")
	ErrBotAlreadyInstalled = errors.New("bot already installed")
	ErrBotNotInstalled     = errors.New("bot not installed")
	ErrInvalidToken        = errors.New("invalid token")
)

// Bot represents a bot identity linked to a user account.
type Bot struct {
	ID                uuid.UUID `json:"id"`
	UserID            uuid.UUID `json:"user_id"`
	OwnerID           uuid.UUID `json:"owner_id"`
	Username          string    `json:"username"`
	DisplayName       string    `json:"display_name"`
	AvatarURL         *string   `json:"avatar_url,omitempty"`
	Description       *string   `json:"description,omitempty"`
	ShortDescription  *string   `json:"short_description,omitempty"`
	IsSystem          bool      `json:"is_system"`
	IsInline          bool      `json:"is_inline"`
	WebhookURL        *string   `json:"webhook_url,omitempty"`
	WebhookSecretHash *string   `json:"-"`
	IsActive          bool      `json:"is_active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// BotToken exposes token metadata without the sensitive hash.
type BotToken struct {
	ID          uuid.UUID  `json:"id"`
	BotID       uuid.UUID  `json:"bot_id"`
	TokenPrefix string     `json:"token_prefix"`
	IsActive    bool       `json:"is_active"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// BotCommand is a slash command registered for a bot.
type BotCommand struct {
	ID          uuid.UUID `json:"id"`
	BotID       uuid.UUID `json:"bot_id"`
	Command     string    `json:"command"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// BotInstallation tracks a bot installed in a specific chat.
type BotInstallation struct {
	BotID       uuid.UUID `json:"bot_id"`
	ChatID      uuid.UUID `json:"chat_id"`
	InstalledBy uuid.UUID `json:"installed_by"`
	Scopes      int64     `json:"scopes"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateBotRequest struct {
	Username         string `json:"username"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description"`
	ShortDescription string `json:"short_description"`
}
