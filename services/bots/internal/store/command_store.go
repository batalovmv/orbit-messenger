// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

type CommandStore interface {
	SetCommands(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error
	GetCommands(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error)
	DeleteAllForBot(ctx context.Context, botID uuid.UUID) error
}

type commandStore struct {
	pool *pgxpool.Pool
}

func NewCommandStore(pool *pgxpool.Pool) CommandStore {
	return &commandStore{pool: pool}
}

func (s *commandStore) SetCommands(ctx context.Context, botID uuid.UUID, commands []model.BotCommand) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin set commands tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM bot_commands WHERE bot_id = $1`, botID); err != nil {
		return fmt.Errorf("delete bot commands: %w", err)
	}

	for _, cmd := range commands {
		if _, err := tx.Exec(ctx, `
			INSERT INTO bot_commands (bot_id, command, description)
			VALUES ($1, $2, $3)
		`, botID, cmd.Command, cmd.Description); err != nil {
			return fmt.Errorf("insert bot command: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit set commands tx: %w", err)
	}

	return nil
}

func (s *commandStore) GetCommands(ctx context.Context, botID uuid.UUID) ([]model.BotCommand, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, bot_id, command, description, created_at
		FROM bot_commands
		WHERE bot_id = $1
		ORDER BY command
	`, botID)
	if err != nil {
		return nil, fmt.Errorf("get bot commands: %w", err)
	}
	defer rows.Close()

	commands := make([]model.BotCommand, 0)
	for rows.Next() {
		var cmd model.BotCommand
		if err := rows.Scan(&cmd.ID, &cmd.BotID, &cmd.Command, &cmd.Description, &cmd.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan bot command: %w", err)
		}
		commands = append(commands, cmd)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bot commands: %w", err)
	}

	return commands, nil
}

func (s *commandStore) DeleteAllForBot(ctx context.Context, botID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM bot_commands WHERE bot_id = $1`, botID); err != nil {
		return fmt.Errorf("delete all bot commands: %w", err)
	}

	return nil
}
