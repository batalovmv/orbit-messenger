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
	Update(ctx context.Context, u *model.User) error
	CountAdmins(ctx context.Context) (int, error)
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string) error
	UpdateTOTP(ctx context.Context, id uuid.UUID, secret *string, enabled bool) error
}

type userStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) UserStore {
	return &userStore{pool: pool}
}

func (s *userStore) Create(ctx context.Context, u *model.User) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name, role, invited_by, invite_code)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, status, is_active, totp_enabled, created_at, updated_at`,
		u.Email, u.PasswordHash, u.DisplayName, u.Role, u.InvitedBy, u.InviteCode,
	).Scan(&u.ID, &u.Status, &u.IsActive, &u.TOTPEnabled, &u.CreatedAt, &u.UpdatedAt)
}

func (s *userStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	u := &model.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, phone, username, display_name, avatar_url, bio,
		        status, custom_status, custom_status_emoji, role, account_type,
		        is_active, deactivated_at, deactivated_by,
		        totp_secret, totp_enabled,
		        invited_by, invite_code, last_seen_at, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Phone, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio,
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
		`SELECT id, email, password_hash, phone, username, display_name, avatar_url, bio,
		        status, custom_status, custom_status_emoji, role, account_type,
		        is_active, deactivated_at, deactivated_by,
		        totp_secret, totp_enabled,
		        invited_by, invite_code, last_seen_at, created_at, updated_at
		 FROM users WHERE email = $1`, email,
	).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.Phone, &u.Username, &u.DisplayName, &u.AvatarURL, &u.Bio,
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
	err := s.pool.QueryRow(ctx,
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
	return nil
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
