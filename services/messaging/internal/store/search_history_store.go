// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

type SearchHistoryStore interface {
	Save(ctx context.Context, userID uuid.UUID, query, scope string) error
	List(ctx context.Context, userID uuid.UUID, limit int) ([]model.SearchHistoryEntry, error)
	Clear(ctx context.Context, userID uuid.UUID) error
}

type searchHistoryStore struct {
	pool *pgxpool.Pool
}

func NewSearchHistoryStore(pool *pgxpool.Pool) SearchHistoryStore {
	return &searchHistoryStore{pool: pool}
}

func (s *searchHistoryStore) Save(ctx context.Context, userID uuid.UUID, query, scope string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO search_history (user_id, query, scope)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, query) DO UPDATE SET
		   created_at = NOW(),
		   scope = EXCLUDED.scope`,
		userID, query, scope,
	)
	if err != nil {
		return fmt.Errorf("searchHistoryStore.Save: %w", err)
	}
	return nil
}

func (s *searchHistoryStore) List(ctx context.Context, userID uuid.UUID, limit int) ([]model.SearchHistoryEntry, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, query, scope, created_at
		 FROM search_history
		 WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("searchHistoryStore.List: %w", err)
	}
	defer rows.Close()

	var entries []model.SearchHistoryEntry
	for rows.Next() {
		var e model.SearchHistoryEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.Query, &e.Scope, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *searchHistoryStore) Clear(ctx context.Context, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM search_history WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("searchHistoryStore.Clear: %w", err)
	}
	return nil
}
