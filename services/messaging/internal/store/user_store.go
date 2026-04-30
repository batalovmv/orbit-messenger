// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

type UserStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	Update(ctx context.Context, u *model.User) error
	UpdateStatus(ctx context.Context, userID, status string, lastSeenAt *time.Time) error
	Search(ctx context.Context, query string, limit int) ([]model.User, error)
	ListAll(ctx context.Context, limit int) ([]model.User, error)

	// Admin methods
	ListAllPaginated(ctx context.Context, cursor string, limit int) ([]model.User, string, bool, error)
	Deactivate(ctx context.Context, userID, actorID uuid.UUID) error
	Reactivate(ctx context.Context, userID uuid.UUID) error
	UpdateRole(ctx context.Context, userID uuid.UUID, newRole string) error
	CountByRole(ctx context.Context, role string) (int, error)
}

type userStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) UserStore {
	return &userStore{pool: pool}
}

const userSelectCols = `id, email, username, display_name, avatar_url, bio, phone,
		status, custom_status, custom_status_emoji, role, account_type,
		is_active, deactivated_at, deactivated_by,
		last_seen_at, created_at, updated_at`

func scanUser(row pgx.Row) (*model.User, error) {
	u := &model.User{}
	err := row.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.Phone,
		&u.Status, &u.CustomStatus, &u.CustomStatusEmoji, &u.Role, &u.AccountType,
		&u.IsActive, &u.DeactivatedAt, &u.DeactivatedBy,
		&u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func scanUsers(rows pgx.Rows) ([]model.User, error) {
	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.Phone,
			&u.Status, &u.CustomStatus, &u.CustomStatusEmoji, &u.Role, &u.AccountType,
			&u.IsActive, &u.DeactivatedAt, &u.DeactivatedBy,
			&u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *userStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE id = $1`, id))
}

// GetByEmail resolves a user by email. Used by admin tooling where the
// operator types in the user's address rather than memorizing UUIDs. The
// users table does not enforce case-folding on the email column (auth's
// register/login both store the raw input), so we LOWER() both sides for
// case-insensitive lookup. Returns (nil, nil) on no match.
func (s *userStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}
	return scanUser(s.pool.QueryRow(ctx,
		`SELECT `+userSelectCols+` FROM users WHERE LOWER(email) = LOWER($1)`, email))
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

func (s *userStore) UpdateStatus(ctx context.Context, userID, status string, lastSeenAt *time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET status=$1, last_seen_at=COALESCE($2, last_seen_at) WHERE id=$3`,
		status, lastSeenAt, userID,
	)
	return err
}

func (s *userStore) ListAll(ctx context.Context, limit int) ([]model.User, error) {
	if limit <= 0 {
		limit = 10000
	}

	rows, err := s.pool.Query(ctx,
		`SELECT `+userSelectCols+`
		 FROM users
		 ORDER BY display_name
		 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanUsers(rows)
}

// escapeILIKE escapes special ILIKE characters (%, _) in search terms.
func escapeILIKE(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func (s *userStore) Search(ctx context.Context, query string, limit int) ([]model.User, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	escaped := "%" + escapeILIKE(query) + "%"
	rows, err := s.pool.Query(ctx,
		`SELECT `+userSelectCols+`
		 FROM users
		 WHERE display_name ILIKE $1 OR email ILIKE $1 OR username ILIKE $1
		 ORDER BY display_name
		 LIMIT $2`,
		escaped, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanUsers(rows)
}

// --- Admin methods ---

func (s *userStore) ListAllPaginated(ctx context.Context, cursor string, limit int) ([]model.User, string, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	var rows pgx.Rows
	var err error

	if cursor != "" {
		rows, err = s.pool.Query(ctx,
			`SELECT `+userSelectCols+`
			 FROM users
			 WHERE display_name > (SELECT display_name FROM users WHERE id = $1)
			    OR (display_name = (SELECT display_name FROM users WHERE id = $1) AND id > $1)
			 ORDER BY display_name, id
			 LIMIT $2`,
			cursor, limit+1,
		)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT `+userSelectCols+`
			 FROM users
			 ORDER BY display_name, id
			 LIMIT $1`,
			limit+1,
		)
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("list all users paginated: %w", err)
	}
	defer rows.Close()

	users, err := scanUsers(rows)
	if err != nil {
		return nil, "", false, err
	}

	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit]
	}

	nextCursor := ""
	if len(users) > 0 {
		nextCursor = users[len(users)-1].ID.String()
	}

	return users, nextCursor, hasMore, nil
}

func (s *userStore) Deactivate(ctx context.Context, userID, actorID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET is_active = false, deactivated_at = now(), deactivated_by = $2, updated_at = now()
		 WHERE id = $1 AND is_active = true`,
		userID, actorID,
	)
	if err != nil {
		return fmt.Errorf("deactivate user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found or already deactivated")
	}
	return nil
}

func (s *userStore) Reactivate(ctx context.Context, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET is_active = true, deactivated_at = NULL, deactivated_by = NULL, updated_at = now()
		 WHERE id = $1 AND is_active = false`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("reactivate user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found or already active")
	}
	return nil
}

func (s *userStore) UpdateRole(ctx context.Context, userID uuid.UUID, newRole string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET role = $2, updated_at = now() WHERE id = $1`,
		userID, newRole,
	)
	if err != nil {
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

func (s *userStore) CountByRole(ctx context.Context, role string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = $1`, role).Scan(&count)
	return count, err
}
