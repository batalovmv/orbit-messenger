// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package botfather

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/bots/internal/client"
)

// ChatProvisioner ensures a user has a pinned DM with BotFather. Used by the
// /system/botfather/ensure-chat handler on the user's first authenticated
// load — a "lazy onboarding" approach so we don't have to pre-seed chats for
// every existing user via migration.
type ChatProvisioner struct {
	bf        *BotFather
	msgClient *client.MessagingClient
	logger    *slog.Logger
}

func NewChatProvisioner(bf *BotFather, msgClient *client.MessagingClient, logger *slog.Logger) *ChatProvisioner {
	return &ChatProvisioner{bf: bf, msgClient: msgClient, logger: logger}
}

// EnsureChat creates (or finds) the BotFather DM for the given user and pins
// it to the top of their chat list. The pin is best-effort: a failure here
// does not prevent the chat from being returned.
func (p *ChatProvisioner) EnsureChat(ctx context.Context, userID uuid.UUID) (chatID uuid.UUID, systemBotID uuid.UUID, err error) {
	if p.bf == nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("botfather not provisioned")
	}
	if p.msgClient == nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("messaging client not configured")
	}

	chatID, err = p.msgClient.EnsureDirectChat(ctx, userID, p.bf.UserID())
	if err != nil {
		return uuid.Nil, uuid.Nil, fmt.Errorf("ensure direct chat: %w", err)
	}

	if err := p.msgClient.SetChatPinned(ctx, userID, chatID, true); err != nil {
		// Pin is non-essential — chat is usable without it.
		if p.logger != nil {
			p.logger.Warn(
				"botfather chat pin failed (non-fatal)",
				"error", err, "chat_id", chatID, "user_id", userID,
			)
		}
	}

	return chatID, p.bf.BotID(), nil
}
