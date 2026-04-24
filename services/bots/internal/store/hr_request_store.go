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
	"github.com/mst-corp/orbit/services/bots/internal/model"
)

type HRRequestStore interface {
	Create(ctx context.Context, req *model.HRRequest) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.HRRequest, error)
	List(ctx context.Context, filter HRRequestFilter) ([]model.HRRequest, error)
	Decide(ctx context.Context, id uuid.UUID, approverID uuid.UUID, approve bool, note string) (*model.HRRequest, error)
}

type HRRequestFilter struct {
	BotID  uuid.UUID
	ChatID *uuid.UUID
	UserID *uuid.UUID
	Status *string
	Limit  int
}

type hrRequestStore struct {
	pool *pgxpool.Pool
}

func NewHRRequestStore(pool *pgxpool.Pool) HRRequestStore {
	return &hrRequestStore{pool: pool}
}

func (s *hrRequestStore) Create(ctx context.Context, req *model.HRRequest) error {
	return s.pool.QueryRow(ctx, `
		INSERT INTO bot_hr_requests
			(bot_id, chat_id, user_id, request_type, start_date, end_date, reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, status, created_at, updated_at
	`,
		req.BotID, req.ChatID, req.UserID, req.RequestType,
		req.StartDate, req.EndDate, req.Reason,
	).Scan(&req.ID, &req.Status, &req.CreatedAt, &req.UpdatedAt)
}

func (s *hrRequestStore) GetByID(ctx context.Context, id uuid.UUID) (*model.HRRequest, error) {
	var r model.HRRequest
	err := s.pool.QueryRow(ctx, `
		SELECT id, bot_id, chat_id, user_id, request_type, start_date, end_date,
		       reason, status, approver_id, decision_note, created_at, updated_at
		FROM bot_hr_requests
		WHERE id = $1
	`, id).Scan(
		&r.ID, &r.BotID, &r.ChatID, &r.UserID, &r.RequestType,
		&r.StartDate, &r.EndDate, &r.Reason, &r.Status,
		&r.ApproverID, &r.DecisionNote, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrHRRequestNotFound
		}
		return nil, fmt.Errorf("get hr request: %w", err)
	}
	return &r, nil
}

func (s *hrRequestStore) List(ctx context.Context, filter HRRequestFilter) ([]model.HRRequest, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `
		SELECT id, bot_id, chat_id, user_id, request_type, start_date, end_date,
		       reason, status, approver_id, decision_note, created_at, updated_at
		FROM bot_hr_requests
		WHERE bot_id = $1`
	args := []any{filter.BotID}

	if filter.ChatID != nil {
		args = append(args, *filter.ChatID)
		query += fmt.Sprintf(" AND chat_id = $%d", len(args))
	}
	if filter.UserID != nil {
		args = append(args, *filter.UserID)
		query += fmt.Sprintf(" AND user_id = $%d", len(args))
	}
	if filter.Status != nil {
		args = append(args, *filter.Status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list hr requests: %w", err)
	}
	defer rows.Close()

	out := make([]model.HRRequest, 0)
	for rows.Next() {
		var r model.HRRequest
		if err := rows.Scan(
			&r.ID, &r.BotID, &r.ChatID, &r.UserID, &r.RequestType,
			&r.StartDate, &r.EndDate, &r.Reason, &r.Status,
			&r.ApproverID, &r.DecisionNote, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan hr request: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Decide atomically transitions a pending request to approved or rejected.
// Returns ErrHRRequestAlreadyFinal if the request is not in pending state.
func (s *hrRequestStore) Decide(ctx context.Context, id uuid.UUID, approverID uuid.UUID, approve bool, note string) (*model.HRRequest, error) {
	newStatus := model.HRStatusRejected
	if approve {
		newStatus = model.HRStatusApproved
	}

	var notePtr *string
	if note != "" {
		notePtr = &note
	}

	var r model.HRRequest
	err := s.pool.QueryRow(ctx, `
		UPDATE bot_hr_requests
		SET status = $1,
		    approver_id = $2,
		    decision_note = $3,
		    updated_at = NOW()
		WHERE id = $4 AND status = 'pending'
		RETURNING id, bot_id, chat_id, user_id, request_type, start_date, end_date,
		          reason, status, approver_id, decision_note, created_at, updated_at
	`, newStatus, approverID, notePtr, id).Scan(
		&r.ID, &r.BotID, &r.ChatID, &r.UserID, &r.RequestType,
		&r.StartDate, &r.EndDate, &r.Reason, &r.Status,
		&r.ApproverID, &r.DecisionNote, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Either the row doesn't exist or it's already decided.
			if _, getErr := s.GetByID(ctx, id); errors.Is(getErr, model.ErrHRRequestNotFound) {
				return nil, model.ErrHRRequestNotFound
			}
			return nil, model.ErrHRRequestAlreadyFinal
		}
		return nil, fmt.Errorf("decide hr request: %w", err)
	}
	return &r, nil
}
