// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"time"
)

type InviteStore interface {
	Create(ctx context.Context, link *model.InviteLink) error
	GetByHash(ctx context.Context, hash string) (*model.InviteLink, error)
	GetByID(ctx context.Context, linkID uuid.UUID) (*model.InviteLink, error)
	ListByChatID(ctx context.Context, chatID uuid.UUID) ([]model.InviteLink, error)
	Update(ctx context.Context, linkID uuid.UUID, title *string, expireAt *time.Time, usageLimit *int, requiresApproval *bool) error
	Revoke(ctx context.Context, linkID uuid.UUID) error
	IncrementUsage(ctx context.Context, linkID uuid.UUID) error
	DecrementUsage(ctx context.Context, linkID uuid.UUID) error
	CreateJoinRequest(ctx context.Context, req *model.JoinRequest) error
	ListJoinRequests(ctx context.Context, chatID uuid.UUID) ([]model.JoinRequest, error)
	UpdateJoinRequestStatus(ctx context.Context, chatID, userID uuid.UUID, status string, reviewedBy uuid.UUID) error
	DeleteJoinRequest(ctx context.Context, chatID, userID uuid.UUID) error
}

type inviteStore struct {
	pool *pgxpool.Pool
}

func NewInviteStore(pool *pgxpool.Pool) InviteStore {
	return &inviteStore{pool: pool}
}

func (s *inviteStore) Create(ctx context.Context, link *model.InviteLink) error {
	return s.pool.QueryRow(ctx,
		`INSERT INTO chat_invite_links (chat_id, creator_id, hash, title, expire_at, usage_limit, requires_approval)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, usage_count, is_revoked, created_at`,
		link.ChatID, link.CreatorID, link.Hash, link.Title, link.ExpireAt, link.UsageLimit, link.RequiresApproval,
	).Scan(&link.ID, &link.UsageCount, &link.IsRevoked, &link.CreatedAt)
}

func (s *inviteStore) GetByHash(ctx context.Context, hash string) (*model.InviteLink, error) {
	l := &model.InviteLink{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, chat_id, creator_id, hash, title, expire_at, usage_limit,
		        usage_count, requires_approval, is_revoked, created_at
		 FROM chat_invite_links WHERE hash = $1`, hash,
	).Scan(&l.ID, &l.ChatID, &l.CreatorID, &l.Hash, &l.Title, &l.ExpireAt,
		&l.UsageLimit, &l.UsageCount, &l.RequiresApproval, &l.IsRevoked, &l.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func (s *inviteStore) GetByID(ctx context.Context, linkID uuid.UUID) (*model.InviteLink, error) {
	l := &model.InviteLink{}
	err := s.pool.QueryRow(ctx,
		`SELECT id, chat_id, creator_id, hash, title, expire_at, usage_limit,
		        usage_count, requires_approval, is_revoked, created_at
		 FROM chat_invite_links WHERE id = $1`, linkID,
	).Scan(&l.ID, &l.ChatID, &l.CreatorID, &l.Hash, &l.Title, &l.ExpireAt,
		&l.UsageLimit, &l.UsageCount, &l.RequiresApproval, &l.IsRevoked, &l.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func (s *inviteStore) ListByChatID(ctx context.Context, chatID uuid.UUID) ([]model.InviteLink, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, chat_id, creator_id, hash, title, expire_at, usage_limit,
		        usage_count, requires_approval, is_revoked, created_at
		 FROM chat_invite_links
		 WHERE chat_id = $1
		 ORDER BY created_at DESC
		 LIMIT 200`, chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []model.InviteLink
	for rows.Next() {
		var l model.InviteLink
		if err := rows.Scan(&l.ID, &l.ChatID, &l.CreatorID, &l.Hash, &l.Title, &l.ExpireAt,
			&l.UsageLimit, &l.UsageCount, &l.RequiresApproval, &l.IsRevoked, &l.CreatedAt); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

func (s *inviteStore) Update(ctx context.Context, linkID uuid.UUID, title *string, expireAt *time.Time, usageLimit *int, requiresApproval *bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_invite_links SET
		   title = COALESCE($2, title),
		   expire_at = COALESCE($3, expire_at),
		   usage_limit = COALESCE($4, usage_limit),
		   requires_approval = COALESCE($5, requires_approval)
		 WHERE id = $1`,
		linkID, title, expireAt, usageLimit, requiresApproval,
	)
	return err
}

func (s *inviteStore) Revoke(ctx context.Context, linkID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_invite_links SET is_revoked = true WHERE id = $1`, linkID,
	)
	return err
}

func (s *inviteStore) IncrementUsage(ctx context.Context, linkID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE chat_invite_links
		 SET usage_count = usage_count + 1
		 WHERE id = $1
		   AND is_revoked = false
		   AND (usage_limit = 0 OR usage_count < usage_limit)`, linkID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("invite link usage limit reached or link invalid")
	}
	return nil
}

func (s *inviteStore) DecrementUsage(ctx context.Context, linkID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE chat_invite_links SET usage_count = usage_count - 1
		 WHERE id = $1 AND usage_count > 0`, linkID,
	)
	return err
}

func (s *inviteStore) CreateJoinRequest(ctx context.Context, req *model.JoinRequest) error {
	err := s.pool.QueryRow(ctx,
		`INSERT INTO chat_join_requests (chat_id, user_id, message)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (chat_id, user_id) DO NOTHING
		 RETURNING created_at`,
		req.ChatID, req.UserID, req.Message,
	).Scan(&req.CreatedAt)
	// ON CONFLICT DO NOTHING returns no rows → pgx.ErrNoRows means request already exists (idempotent)
	if err == pgx.ErrNoRows {
		return nil
	}
	return err
}

func (s *inviteStore) ListJoinRequests(ctx context.Context, chatID uuid.UUID) ([]model.JoinRequest, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT jr.chat_id, jr.user_id, jr.message, jr.status, jr.reviewed_by, jr.created_at,
		        u.display_name, u.avatar_url
		 FROM chat_join_requests jr
		 JOIN users u ON u.id = jr.user_id
		 WHERE jr.chat_id = $1 AND jr.status = 'pending'
		 ORDER BY jr.created_at
		 LIMIT 200`, chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var requests []model.JoinRequest
	for rows.Next() {
		var r model.JoinRequest
		if err := rows.Scan(&r.ChatID, &r.UserID, &r.Message, &r.Status, &r.ReviewedBy, &r.CreatedAt,
			&r.DisplayName, &r.AvatarURL); err != nil {
			return nil, err
		}
		requests = append(requests, r)
	}
	return requests, rows.Err()
}

func (s *inviteStore) UpdateJoinRequestStatus(ctx context.Context, chatID, userID uuid.UUID, status string, reviewedBy uuid.UUID) error {
	ct, err := s.pool.Exec(ctx,
		`UPDATE chat_join_requests SET status = $3, reviewed_by = $4
		 WHERE chat_id = $1 AND user_id = $2 AND status = 'pending'`,
		chatID, userID, status, reviewedBy,
	)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("join request not found or already processed")
	}
	return nil
}

func (s *inviteStore) DeleteJoinRequest(ctx context.Context, chatID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM chat_join_requests WHERE chat_id = $1 AND user_id = $2`,
		chatID, userID,
	)
	return err
}
