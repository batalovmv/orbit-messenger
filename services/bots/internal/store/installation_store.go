// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

type WebhookBotInfo struct {
	BotID      uuid.UUID
	UserID     uuid.UUID
	WebhookURL string
	SecretHash *string
	Scopes     int64
}

type InstallationStore interface {
	Install(ctx context.Context, inst *model.BotInstallation) error
	Uninstall(ctx context.Context, botID, chatID uuid.UUID) error
	GetByBotAndChat(ctx context.Context, botID, chatID uuid.UUID) (*model.BotInstallation, error)
	ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.BotInstallation, error)
	ListByBot(ctx context.Context, botID uuid.UUID) ([]model.BotInstallation, error)
	ListChatsWithWebhookBots(ctx context.Context, chatID uuid.UUID) ([]WebhookBotInfo, error)
}

type installationStore struct {
	pool *pgxpool.Pool
}

func NewInstallationStore(pool *pgxpool.Pool) InstallationStore {
	return &installationStore{pool: pool}
}

func (s *installationStore) Install(ctx context.Context, inst *model.BotInstallation) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin install bot tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var botUserID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM bots WHERE id = $1`, inst.BotID).Scan(&botUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrBotNotFound
	}
	if err != nil {
		return fmt.Errorf("get bot user for install: %w", err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO bot_installations (bot_id, chat_id, installed_by, scopes, is_active)
		VALUES ($1, $2, $3, $4, true)
		ON CONFLICT (bot_id, chat_id) DO UPDATE
		SET installed_by = EXCLUDED.installed_by,
		    scopes = EXCLUDED.scopes,
		    is_active = true,
		    updated_at = NOW()
		WHERE bot_installations.is_active = false
		RETURNING created_at, updated_at, is_active
	`, inst.BotID, inst.ChatID, inst.InstalledBy, inst.Scopes).Scan(&inst.CreatedAt, &inst.UpdatedAt, &inst.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrBotAlreadyInstalled
	}
	if err != nil {
		return fmt.Errorf("insert bot installation: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO chat_members (chat_id, user_id, role, permissions, notification_level)
		VALUES ($1, $2, 'member', -1, 'all')
		ON CONFLICT (chat_id, user_id) DO NOTHING
	`, inst.ChatID, botUserID); err != nil {
		return fmt.Errorf("add bot chat member: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit install bot tx: %w", err)
	}

	return nil
}

func (s *installationStore) Uninstall(ctx context.Context, botID, chatID uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin uninstall bot tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var botUserID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM bots WHERE id = $1`, botID).Scan(&botUserID)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrBotNotFound
	}
	if err != nil {
		return fmt.Errorf("get bot user for uninstall: %w", err)
	}

	tag, err := tx.Exec(ctx, `
		UPDATE bot_installations
		SET is_active = false, updated_at = NOW()
		WHERE bot_id = $1 AND chat_id = $2 AND is_active = true
	`, botID, chatID)
	if err != nil {
		return fmt.Errorf("deactivate bot installation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrBotNotInstalled
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM chat_members
		WHERE chat_id = $1 AND user_id = $2
	`, chatID, botUserID); err != nil {
		return fmt.Errorf("delete bot chat member: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit uninstall bot tx: %w", err)
	}

	return nil
}

func (s *installationStore) GetByBotAndChat(ctx context.Context, botID, chatID uuid.UUID) (*model.BotInstallation, error) {
	inst := &model.BotInstallation{}
	err := s.pool.QueryRow(ctx, `
		SELECT bot_id, chat_id, installed_by, scopes, is_active, created_at, updated_at
		FROM bot_installations
		WHERE bot_id = $1 AND chat_id = $2
	`, botID, chatID).Scan(
		&inst.BotID,
		&inst.ChatID,
		&inst.InstalledBy,
		&inst.Scopes,
		&inst.IsActive,
		&inst.CreatedAt,
		&inst.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bot installation: %w", err)
	}

	return inst, nil
}

func (s *installationStore) ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.BotInstallation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bot_id, chat_id, installed_by, scopes, is_active, created_at, updated_at
		FROM bot_installations
		WHERE chat_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`, chatID)
	if err != nil {
		return nil, fmt.Errorf("list bot installations by chat: %w", err)
	}
	defer rows.Close()

	installations := make([]model.BotInstallation, 0)
	for rows.Next() {
		var inst model.BotInstallation
		if err := rows.Scan(
			&inst.BotID,
			&inst.ChatID,
			&inst.InstalledBy,
			&inst.Scopes,
			&inst.IsActive,
			&inst.CreatedAt,
			&inst.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan bot installation by chat: %w", err)
		}
		installations = append(installations, inst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bot installations by chat: %w", err)
	}

	return installations, nil
}

func (s *installationStore) ListByBot(ctx context.Context, botID uuid.UUID) ([]model.BotInstallation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bot_id, chat_id, installed_by, scopes, is_active, created_at, updated_at
		FROM bot_installations
		WHERE bot_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`, botID)
	if err != nil {
		return nil, fmt.Errorf("list bot installations by bot: %w", err)
	}
	defer rows.Close()

	installations := make([]model.BotInstallation, 0)
	for rows.Next() {
		var inst model.BotInstallation
		if err := rows.Scan(
			&inst.BotID,
			&inst.ChatID,
			&inst.InstalledBy,
			&inst.Scopes,
			&inst.IsActive,
			&inst.CreatedAt,
			&inst.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan bot installation by bot: %w", err)
		}
		installations = append(installations, inst)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bot installations by bot: %w", err)
	}

	return installations, nil
}

func (s *installationStore) ListChatsWithWebhookBots(ctx context.Context, chatID uuid.UUID) ([]WebhookBotInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT bi.bot_id, b.user_id, COALESCE(b.webhook_url, ''), b.webhook_secret_hash, bi.scopes
		FROM bot_installations bi
		JOIN bots b ON b.id = bi.bot_id
		WHERE bi.chat_id = $1
		  AND bi.is_active = true
	`, chatID)
	if err != nil {
		return nil, fmt.Errorf("list chat webhook bots: %w", err)
	}
	defer rows.Close()

	bots := make([]WebhookBotInfo, 0)
	for rows.Next() {
		var info WebhookBotInfo
		if err := rows.Scan(&info.BotID, &info.UserID, &info.WebhookURL, &info.SecretHash, &info.Scopes); err != nil {
			return nil, fmt.Errorf("scan chat webhook bot: %w", err)
		}
		bots = append(bots, info)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chat webhook bots: %w", err)
	}

	return bots, nil
}
