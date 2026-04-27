// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FeatureFlag mirrors a row from the feature_flags table.
type FeatureFlag struct {
	Key         string          `json:"key"`
	Enabled     bool            `json:"enabled"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// FeatureFlagStore is the persistence boundary for the feature_flags table.
//
// All writes go through Upsert — there is no delete. Removing a flag means
// removing the registry entry in pkg/featureflags AND running an explicit
// migration; it is not a runtime operation.
type FeatureFlagStore interface {
	List(ctx context.Context) ([]FeatureFlag, error)
	Get(ctx context.Context, key string) (*FeatureFlag, error)
	Upsert(ctx context.Context, key string, enabled bool, description string, metadata json.RawMessage) (*FeatureFlag, error)
}

type featureFlagStore struct {
	pool *pgxpool.Pool
}

func NewFeatureFlagStore(pool *pgxpool.Pool) FeatureFlagStore {
	return &featureFlagStore{pool: pool}
}

func (s *featureFlagStore) List(ctx context.Context) ([]FeatureFlag, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT key, enabled, COALESCE(description,''), COALESCE(metadata, '{}'::jsonb), created_at, updated_at
		 FROM feature_flags
		 ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("list feature flags: %w", err)
	}
	defer rows.Close()

	var out []FeatureFlag
	for rows.Next() {
		var f FeatureFlag
		if err := rows.Scan(&f.Key, &f.Enabled, &f.Description, &f.Metadata, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan feature flag: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *featureFlagStore) Get(ctx context.Context, key string) (*FeatureFlag, error) {
	var f FeatureFlag
	err := s.pool.QueryRow(ctx,
		`SELECT key, enabled, COALESCE(description,''), COALESCE(metadata, '{}'::jsonb), created_at, updated_at
		 FROM feature_flags WHERE key = $1`,
		key,
	).Scan(&f.Key, &f.Enabled, &f.Description, &f.Metadata, &f.CreatedAt, &f.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get feature flag: %w", err)
	}
	return &f, nil
}

// Upsert creates the row if missing or updates enabled/description/metadata.
// Description is updated only when caller passes a non-empty value, so a
// toggle-only call keeps the description from migration.
func (s *featureFlagStore) Upsert(ctx context.Context, key string, enabled bool, description string, metadata json.RawMessage) (*FeatureFlag, error) {
	if metadata == nil {
		metadata = json.RawMessage(`{}`)
	}
	var f FeatureFlag
	err := s.pool.QueryRow(ctx,
		`INSERT INTO feature_flags (key, enabled, description, metadata, updated_at)
		 VALUES ($1, $2, NULLIF($3,''), $4, NOW())
		 ON CONFLICT (key) DO UPDATE SET
		     enabled = EXCLUDED.enabled,
		     description = COALESCE(NULLIF(EXCLUDED.description,''), feature_flags.description),
		     metadata = EXCLUDED.metadata,
		     updated_at = NOW()
		 RETURNING key, enabled, COALESCE(description,''), COALESCE(metadata,'{}'::jsonb), created_at, updated_at`,
		key, enabled, description, metadata,
	).Scan(&f.Key, &f.Enabled, &f.Description, &f.Metadata, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert feature flag: %w", err)
	}
	return &f, nil
}
