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
)

// UserIdentity is the corporate identity bundle injected into bot Updates
// when share_user_emails is enabled. LDAPDN is reserved for future use; the
// users table currently has no LDAP column so it is always empty.
type UserIdentity struct {
	Email  string
	LDAPDN string
}

// UserLookupStore reads user identity fields needed by the Bot API. We keep
// it isolated from BotStore to make the access surface explicit — only
// (id → email) is ever exposed, no other PII.
type UserLookupStore interface {
	GetIdentity(ctx context.Context, userID uuid.UUID) (UserIdentity, error)
}

type userLookupStore struct {
	pool *pgxpool.Pool
}

func NewUserLookupStore(pool *pgxpool.Pool) UserLookupStore {
	return &userLookupStore{pool: pool}
}

func (s *userLookupStore) GetIdentity(ctx context.Context, userID uuid.UUID) (UserIdentity, error) {
	var ident UserIdentity
	err := s.pool.QueryRow(ctx, `
		SELECT email FROM users WHERE id = $1
	`, userID).Scan(&ident.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserIdentity{}, nil
	}
	if err != nil {
		return UserIdentity{}, fmt.Errorf("get user identity: %w", err)
	}
	return ident, nil
}
