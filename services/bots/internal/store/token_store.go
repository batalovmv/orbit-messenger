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

type TokenStore interface {
	Create(ctx context.Context, botID uuid.UUID, tokenHash, tokenPrefix string) (*model.BotToken, error)
	GetByHash(ctx context.Context, tokenHash string) (*model.BotToken, error)
	ListByBot(ctx context.Context, botID uuid.UUID) ([]model.BotToken, error)
	RevokeAllForBot(ctx context.Context, botID uuid.UUID) error
	UpdateLastUsed(ctx context.Context, tokenID uuid.UUID) error
}

type tokenStore struct {
	pool *pgxpool.Pool
}

func NewTokenStore(pool *pgxpool.Pool) TokenStore {
	return &tokenStore{pool: pool}
}

func (s *tokenStore) Create(ctx context.Context, botID uuid.UUID, tokenHash, tokenPrefix string) (*model.BotToken, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin create token tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE bot_tokens
		SET is_active = false
		WHERE bot_id = $1 AND is_active = true
	`, botID); err != nil {
		return nil, fmt.Errorf("revoke existing bot tokens: %w", err)
	}

	token := &model.BotToken{}
	err = tx.QueryRow(ctx, `
		INSERT INTO bot_tokens (bot_id, token_hash, token_prefix, is_active)
		VALUES ($1, $2, $3, true)
		RETURNING id, bot_id, token_prefix, is_active, last_used_at, created_at
	`, botID, tokenHash, tokenPrefix).Scan(
		&token.ID,
		&token.BotID,
		&token.TokenPrefix,
		&token.IsActive,
		&token.LastUsedAt,
		&token.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert bot token: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit create token tx: %w", err)
	}

	return token, nil
}

func (s *tokenStore) GetByHash(ctx context.Context, tokenHash string) (*model.BotToken, error) {
	token := &model.BotToken{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, bot_id, token_prefix, is_active, last_used_at, created_at
		FROM bot_tokens
		WHERE token_hash = $1 AND is_active = true
	`, tokenHash).Scan(
		&token.ID,
		&token.BotID,
		&token.TokenPrefix,
		&token.IsActive,
		&token.LastUsedAt,
		&token.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, model.ErrTokenNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get token by hash: %w", err)
	}

	return token, nil
}

func (s *tokenStore) ListByBot(ctx context.Context, botID uuid.UUID) ([]model.BotToken, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, bot_id, token_prefix, is_active, last_used_at, created_at
		FROM bot_tokens
		WHERE bot_id = $1 AND is_active = true
		ORDER BY created_at DESC
	`, botID)
	if err != nil {
		return nil, fmt.Errorf("list tokens by bot: %w", err)
	}
	defer rows.Close()

	var tokens []model.BotToken
	for rows.Next() {
		var t model.BotToken
		if err := rows.Scan(&t.ID, &t.BotID, &t.TokenPrefix, &t.IsActive, &t.LastUsedAt, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan bot token: %w", err)
		}
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (s *tokenStore) RevokeAllForBot(ctx context.Context, botID uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `
		UPDATE bot_tokens
		SET is_active = false
		WHERE bot_id = $1
	`, botID); err != nil {
		return fmt.Errorf("revoke all bot tokens: %w", err)
	}

	return nil
}

func (s *tokenStore) UpdateLastUsed(ctx context.Context, tokenID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE bot_tokens
		SET last_used_at = NOW()
		WHERE id = $1
	`, tokenID)
	if err != nil {
		return fmt.Errorf("update token last used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrTokenNotFound
	}

	return nil
}
