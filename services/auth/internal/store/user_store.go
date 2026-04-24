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
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

// ErrAdminExists is returned by CreateIfNoAdmins when an admin account already exists.
var ErrAdminExists = errors.New("admin account already exists")

type UserStore interface {
	Create(ctx context.Context, u *model.User) error
	CreateIfNoAdmins(ctx context.Context, u *model.User) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	GetNotificationPriorityMode(ctx context.Context, userID uuid.UUID) (string, error)
	Update(ctx context.Context, u *model.User) error
	CountAdmins(ctx context.Context) (int, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error
	UpdateTOTP(ctx context.Context, id uuid.UUID, secret *string, enabled bool) error
	// EnableTOTPAndRevokeSessions atomically enables TOTP and deletes all sessions
	// in a single DB transaction, closing the TOCTOU window between the two operations.
	EnableTOTPAndRevokeSessions(ctx context.Context, id uuid.UUID, secret string) error
	UpdateNotificationPriorityMode(ctx context.Context, userID uuid.UUID, mode string) error
}

type userStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) UserStore {
	return &userStore{pool: pool}
}

func (s *userStore) Create(ctx context.Context, u *model.User) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("create user: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // safe after Commit

	if err := tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name, role, invited_by, invite_code)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, status, is_active, totp_enabled, created_at, updated_at`,
		u.Email, u.PasswordHash, u.DisplayName, u.Role, u.InvitedBy, u.InviteCode,
	).Scan(&u.ID, &u.Status, &u.IsActive, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return err
	}
	if err := s.installDefaultStickerPacks(ctx, tx, u.ID); err != nil {
		return fmt.Errorf("create user: install default sticker packs: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *userStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	u := &model.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, notification_priority_mode, phone, username, display_name, avatar_url, bio,
		        status, custom_status, custom_status_emoji, role, account_type,
		        is_active, deactivated_at, deactivated_by,
		        totp_secret, totp_enabled,
		        invited_by, invite_code, last_seen_at, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.NotificationPriorityMode, &u.Phone, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio,
		&u.Status, &u.CustomStatus, &u.CustomStatusEmoji, &u.Role, &u.AccountType,
		&u.IsActive, &u.DeactivatedAt, &u.DeactivatedBy,
		&u.TOTPSecret, &u.TOTPEnabled,
		&u.InvitedBy, &u.InviteCode, &u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (s *userStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	u := &model.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, notification_priority_mode, phone, username, display_name, avatar_url, bio,
		        status, custom_status, custom_status_emoji, role, account_type,
		        is_active, deactivated_at, deactivated_by,
		        totp_secret, totp_enabled,
		        invited_by, invite_code, last_seen_at, created_at, updated_at
		 FROM users WHERE email = $1`, email,
	).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.NotificationPriorityMode, &u.Phone, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio,
		&u.Status, &u.CustomStatus, &u.CustomStatusEmoji, &u.Role, &u.AccountType,
		&u.IsActive, &u.DeactivatedAt, &u.DeactivatedBy,
		&u.TOTPSecret, &u.TOTPEnabled,
		&u.InvitedBy, &u.InviteCode, &u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (s *userStore) GetNotificationPriorityMode(ctx context.Context, userID uuid.UUID) (string, error) {
	var mode string
	err := s.pool.QueryRow(ctx,
		`SELECT COALESCE(notification_priority_mode, 'all') FROM users WHERE id = $1`,
		userID,
	).Scan(&mode)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("get notification priority mode %s: %w", userID, pgx.ErrNoRows)
		}
		return "", fmt.Errorf("get notification priority mode %s: %w", userID, err)
	}
	return mode, nil
}

func (s *userStore) Update(ctx context.Context, u *model.User) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET display_name=$1, bio=$2, phone=$3, avatar_url=$4,
		 custom_status=$5, custom_status_emoji=$6
		 WHERE id=$7`,
		u.DisplayName, u.Bio, u.Phone, u.AvatarURL,
		u.CustomStatus, u.CustomStatusEmoji, u.ID,
	)
	return err
}

// CreateIfNoAdmins atomically checks that no superadmin/admin exists and inserts the user.
// Returns ErrAdminExists if a privileged admin already exists (race-safe).
func (s *userStore) CreateIfNoAdmins(ctx context.Context, u *model.User) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("create admin: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // safe after Commit

	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name, role, invited_by, invite_code)
		 SELECT $1, $2, $3, $4, $5, $6
		 WHERE NOT EXISTS (SELECT 1 FROM users WHERE role IN ('superadmin', 'admin'))
		 RETURNING id, status, is_active, totp_enabled, created_at, updated_at`,
		u.Email, u.PasswordHash, u.DisplayName, u.Role, u.InvitedBy, u.InviteCode,
	).Scan(&u.ID, &u.Status, &u.IsActive, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAdminExists
		}
		return fmt.Errorf("create admin: %w", err)
	}
	if err := s.installDefaultStickerPacks(ctx, tx, u.ID); err != nil {
		return fmt.Errorf("create admin: install default sticker packs: %w", err)
	}
	return tx.Commit(ctx)
}

func (s *userStore) installDefaultStickerPacks(ctx context.Context, tx pgx.Tx, userID uuid.UUID) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO user_installed_stickers (user_id, pack_id, position)
		 SELECT $1, pack_id::uuid, position
		 FROM (VALUES
		     ('5f52fd0a-8c59-4f3b-a9b3-6c26e3e95c51'::uuid, 0),
		     ('10000000-0000-4000-8000-0000000000a1'::uuid, 1),
		     ('10000000-0000-4000-8000-0000000000b1'::uuid, 2)
		 ) AS packs(pack_id, position)
		 ON CONFLICT (user_id, pack_id) DO NOTHING`,
		userID,
	)
	return err
}

func (s *userStore) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role IN ('superadmin', 'admin')`).Scan(&count)
	return count, err
}

func (s *userStore) UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, hash, id)
	return err
}

func (s *userStore) UpdateTOTP(ctx context.Context, id uuid.UUID, secret *string, enabled bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET totp_secret = $1, totp_enabled = $2 WHERE id = $3`,
		secret, enabled, id,
	)
	return err
}

// EnableTOTPAndRevokeSessions atomically enables TOTP and deletes all sessions
// in a single DB transaction. This closes the TOCTOU window where a concurrent
// Login could create a new session between revocation and TOTP enable.
func (s *userStore) EnableTOTPAndRevokeSessions(ctx context.Context, id uuid.UUID, secret string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("enable totp: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // safe after Commit

	if _, err := tx.Exec(ctx,
		`UPDATE users SET totp_secret = $1, totp_enabled = true WHERE id = $2`,
		secret, id,
	); err != nil {
		return fmt.Errorf("enable totp: update user: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM sessions WHERE user_id = $1`, id,
	); err != nil {
		return fmt.Errorf("enable totp: revoke sessions: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *userStore) UpdateNotificationPriorityMode(ctx context.Context, userID uuid.UUID, mode string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET notification_priority_mode = $1, updated_at = NOW() WHERE id = $2`,
		mode, userID)
	return err
}
