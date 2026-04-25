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

// ApprovalFilter constrains the List query. BotID is always required.
type ApprovalFilter struct {
	BotID       uuid.UUID
	ChatID      *uuid.UUID
	RequesterID *uuid.UUID
	Status      *string
}

// ApprovalStore is the persistence interface for bot_approval_requests.
type ApprovalStore interface {
	Create(ctx context.Context, req *model.ApprovalRequest) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.ApprovalRequest, error)
	List(ctx context.Context, filter ApprovalFilter) ([]model.ApprovalRequest, error)
	Decide(ctx context.Context, id, deciderID uuid.UUID, decision string, note string) (*model.ApprovalRequest, error)
	Cancel(ctx context.Context, id, callerID uuid.UUID, isOwner bool) (*model.ApprovalRequest, error)
}

type pgApprovalStore struct {
	db *pgxpool.Pool
}

// NewApprovalStore returns a PostgreSQL-backed ApprovalStore.
func NewApprovalStore(db *pgxpool.Pool) ApprovalStore {
	return &pgApprovalStore{db: db}
}

func (s *pgApprovalStore) Create(ctx context.Context, req *model.ApprovalRequest) error {
	payload := req.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	return s.db.QueryRow(ctx,
		`INSERT INTO bot_approval_requests (bot_id, chat_id, requester_id, approval_type, subject, payload)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, status, version, created_at, updated_at`,
		req.BotID, req.ChatID, req.RequesterID, req.ApprovalType, req.Subject, payload,
	).Scan(&req.ID, &req.Status, &req.Version, &req.CreatedAt, &req.UpdatedAt)
}

func (s *pgApprovalStore) GetByID(ctx context.Context, id uuid.UUID) (*model.ApprovalRequest, error) {
	var r model.ApprovalRequest
	err := s.db.QueryRow(ctx,
		`SELECT id, bot_id, chat_id, requester_id, approval_type, subject, payload,
		        status, version, decided_by, decided_at, decision_note, created_at, updated_at
		 FROM bot_approval_requests
		 WHERE id = $1`,
		id,
	).Scan(
		&r.ID, &r.BotID, &r.ChatID, &r.RequesterID, &r.ApprovalType, &r.Subject, &r.Payload,
		&r.Status, &r.Version, &r.DecidedBy, &r.DecidedAt, &r.DecisionNote, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrApprovalNotFound
		}
		return nil, fmt.Errorf("get approval request: %w", err)
	}
	return &r, nil
}

func (s *pgApprovalStore) List(ctx context.Context, filter ApprovalFilter) ([]model.ApprovalRequest, error) {
	query := `SELECT id, bot_id, chat_id, requester_id, approval_type, subject, payload,
	                 status, version, decided_by, decided_at, decision_note, created_at, updated_at
	          FROM bot_approval_requests
	          WHERE bot_id = $1`
	args := []any{filter.BotID}

	if filter.ChatID != nil {
		args = append(args, *filter.ChatID)
		query += fmt.Sprintf(" AND chat_id = $%d", len(args))
	}
	if filter.RequesterID != nil {
		args = append(args, *filter.RequesterID)
		query += fmt.Sprintf(" AND requester_id = $%d", len(args))
	}
	if filter.Status != nil {
		args = append(args, *filter.Status)
		query += fmt.Sprintf(" AND status = $%d", len(args))
	}
	query += " ORDER BY created_at DESC LIMIT 100"

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list approval requests: %w", err)
	}
	defer rows.Close()

	out := make([]model.ApprovalRequest, 0)
	for rows.Next() {
		var r model.ApprovalRequest
		if err := rows.Scan(
			&r.ID, &r.BotID, &r.ChatID, &r.RequesterID, &r.ApprovalType, &r.Subject, &r.Payload,
			&r.Status, &r.Version, &r.DecidedBy, &r.DecidedAt, &r.DecisionNote, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan approval request: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Decide transitions a pending approval to approved or rejected using version-CAS
// to prevent double-decide races. Returns ErrApprovalAlreadyFinal if the request
// is no longer pending (already decided, cancelled, or concurrent modification).
func (s *pgApprovalStore) Decide(ctx context.Context, id, deciderID uuid.UUID, decision string, note string) (*model.ApprovalRequest, error) {
	if decision != model.ApprovalStatusApproved && decision != model.ApprovalStatusRejected {
		return nil, fmt.Errorf("decide: invalid decision %q", decision)
	}

	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status != model.ApprovalStatusPending {
		return nil, model.ErrApprovalAlreadyFinal
	}

	var notePtr *string
	if note != "" {
		notePtr = &note
	}

	var r model.ApprovalRequest
	err = s.db.QueryRow(ctx,
		`UPDATE bot_approval_requests
		 SET status = $1, decided_by = $2, decided_at = NOW(), decision_note = $3, version = version + 1
		 WHERE id = $4 AND version = $5 AND status = 'pending'
		 RETURNING id, bot_id, chat_id, requester_id, approval_type, subject, payload,
		           status, version, decided_by, decided_at, decision_note, created_at, updated_at`,
		decision, deciderID, notePtr, id, existing.Version,
	).Scan(
		&r.ID, &r.BotID, &r.ChatID, &r.RequesterID, &r.ApprovalType, &r.Subject, &r.Payload,
		&r.Status, &r.Version, &r.DecidedBy, &r.DecidedAt, &r.DecisionNote, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrApprovalAlreadyFinal
		}
		return nil, fmt.Errorf("decide approval request: %w", err)
	}
	return &r, nil
}

// Cancel transitions a pending approval to cancelled. Only the original requester
// or the bot owner (isOwner=true) may cancel. Uses version-CAS to prevent races.
func (s *pgApprovalStore) Cancel(ctx context.Context, id, callerID uuid.UUID, isOwner bool) (*model.ApprovalRequest, error) {
	existing, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status != model.ApprovalStatusPending {
		return nil, model.ErrApprovalAlreadyFinal
	}
	if !isOwner && existing.RequesterID != callerID {
		return nil, model.ErrApprovalForbidden
	}

	var r model.ApprovalRequest
	err = s.db.QueryRow(ctx,
		`UPDATE bot_approval_requests
		 SET status = 'cancelled', version = version + 1
		 WHERE id = $1 AND version = $2 AND status = 'pending'
		 RETURNING id, bot_id, chat_id, requester_id, approval_type, subject, payload,
		           status, version, decided_by, decided_at, decision_note, created_at, updated_at`,
		id, existing.Version,
	).Scan(
		&r.ID, &r.BotID, &r.ChatID, &r.RequesterID, &r.ApprovalType, &r.Subject, &r.Payload,
		&r.Status, &r.Version, &r.DecidedBy, &r.DecidedAt, &r.DecisionNote, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrApprovalAlreadyFinal
		}
		return nil, fmt.Errorf("cancel approval request: %w", err)
	}
	return &r, nil
}
