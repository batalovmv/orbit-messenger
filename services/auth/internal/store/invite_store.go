package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/auth/internal/model"
)

type InviteStore interface {
	Create(ctx context.Context, inv *model.Invite) error
	GetByCode(ctx context.Context, code string) (*model.Invite, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.Invite, error)
	ListAll(ctx context.Context) ([]model.Invite, error)
	UseInvite(ctx context.Context, code string, userID uuid.UUID) error
	Revoke(ctx context.Context, id uuid.UUID, createdBy uuid.UUID) error
}

type inviteStore struct {
	pool *pgxpool.Pool
}

func NewInviteStore(pool *pgxpool.Pool) InviteStore {
	return &inviteStore{pool: pool}
}

func (s *inviteStore) Create(ctx context.Context, inv *model.Invite) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO invites (code, created_by, email, role, max_uses, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, is_active, use_count, created_at`,
		inv.Code, inv.CreatedBy, inv.Email, inv.Role, inv.MaxUses, inv.ExpiresAt,
	).Scan(&inv.ID, &inv.IsActive, &inv.UseCount, &inv.CreatedAt)
}

func (s *inviteStore) GetByCode(ctx context.Context, code string) (*model.Invite, error) {
	inv := &model.Invite{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, code, created_by, email, role, max_uses, use_count,
		        used_by, used_at, expires_at, is_active, created_at
		 FROM invites WHERE code = $1`, code,
	).Scan(
		&inv.ID, &inv.Code, &inv.CreatedBy, &inv.Email, &inv.Role, &inv.MaxUses, &inv.UseCount,
		&inv.UsedBy, &inv.UsedAt, &inv.ExpiresAt, &inv.IsActive, &inv.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return inv, err
}

func (s *inviteStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Invite, error) {
	inv := &model.Invite{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, code, created_by, email, role, max_uses, use_count,
		        used_by, used_at, expires_at, is_active, created_at
		 FROM invites WHERE id = $1`, id,
	).Scan(
		&inv.ID, &inv.Code, &inv.CreatedBy, &inv.Email, &inv.Role, &inv.MaxUses, &inv.UseCount,
		&inv.UsedBy, &inv.UsedAt, &inv.ExpiresAt, &inv.IsActive, &inv.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return inv, err
}

func (s *inviteStore) ListAll(ctx context.Context) ([]model.Invite, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, code, created_by, email, role, max_uses, use_count,
		        used_by, used_at, expires_at, is_active, created_at
		 FROM invites ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var invites []model.Invite
	for rows.Next() {
		var inv model.Invite
		if err := rows.Scan(
			&inv.ID, &inv.Code, &inv.CreatedBy, &inv.Email, &inv.Role, &inv.MaxUses, &inv.UseCount,
			&inv.UsedBy, &inv.UsedAt, &inv.ExpiresAt, &inv.IsActive, &inv.CreatedAt,
		); err != nil {
			return nil, err
		}
		invites = append(invites, inv)
	}
	return invites, rows.Err()
}

func (s *inviteStore) UseInvite(ctx context.Context, code string, userID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE invites SET use_count = use_count + 1, used_by = $1, used_at = now()
		 WHERE code = $2 AND is_active = true AND use_count < max_uses
		 AND (expires_at IS NULL OR expires_at > now())`,
		userID, code,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *inviteStore) Revoke(ctx context.Context, id uuid.UUID, createdBy uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE invites SET is_active = false WHERE id = $1 AND created_by = $2`,
		id, createdBy,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}
