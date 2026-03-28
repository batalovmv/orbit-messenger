package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

type UserStore interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.User, error)
	Update(ctx context.Context, u *model.User) error
	UpdateStatus(ctx context.Context, userID, status string, lastSeenAt *time.Time) error
	Search(ctx context.Context, query string, limit int) ([]model.User, error)
	ListAll(ctx context.Context, limit int) ([]model.User, error)
}

type userStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) UserStore {
	return &userStore{pool: pool}
}

func (s *userStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	u := &model.User{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, email, display_name, avatar_url, bio, phone,
		        status, custom_status, custom_status_emoji, role,
		        last_seen_at, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.Phone,
		&u.Status, &u.CustomStatus, &u.CustomStatusEmoji, &u.Role,
		&u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt)
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

func (s *userStore) UpdateStatus(ctx context.Context, userID, status string, lastSeenAt *time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE users SET status=$1, last_seen_at=COALESCE($2, last_seen_at) WHERE id=$3`,
		status, lastSeenAt, userID,
	)
	return err
}

func (s *userStore) ListAll(ctx context.Context, limit int) ([]model.User, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, email, display_name, avatar_url, bio, phone,
		        status, custom_status, custom_status_emoji, role,
		        last_seen_at, created_at, updated_at
		 FROM users
		 ORDER BY display_name
		 LIMIT $1`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.Phone,
			&u.Status, &u.CustomStatus, &u.CustomStatusEmoji, &u.Role,
			&u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *userStore) Search(ctx context.Context, query string, limit int) ([]model.User, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, email, display_name, avatar_url, bio, phone,
		        status, custom_status, custom_status_emoji, role,
		        last_seen_at, created_at, updated_at
		 FROM users
		 WHERE display_name ILIKE $1 OR email ILIKE $1
		 ORDER BY display_name
		 LIMIT $2`,
		"%"+query+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarURL, &u.Bio, &u.Phone,
			&u.Status, &u.CustomStatus, &u.CustomStatusEmoji, &u.Role,
			&u.LastSeenAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
