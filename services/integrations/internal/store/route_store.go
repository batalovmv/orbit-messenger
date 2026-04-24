// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
)

type RouteStore interface {
	Create(ctx context.Context, r *model.Route) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Route, error)
	ListByConnector(ctx context.Context, connectorID uuid.UUID) ([]model.Route, error)
	ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.Route, error)
	Update(ctx context.Context, r *model.Route) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindMatchingRoutes(ctx context.Context, connectorID uuid.UUID, eventType string) ([]model.Route, error)
}

type routeStore struct {
	pool *pgxpool.Pool
}

func NewRouteStore(pool *pgxpool.Pool) RouteStore {
	return &routeStore{pool: pool}
}

func (s *routeStore) Create(ctx context.Context, r *model.Route) error {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO integration_routes (
			connector_id, chat_id, event_filter, template, is_active
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`, r.ConnectorID, r.ChatID, r.EventFilter, r.Template, r.IsActive).
		Scan(&r.ID, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return model.ErrDuplicateRoute
		}
		return fmt.Errorf("create route: %w", err)
	}

	return nil
}

func (s *routeStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Route, error) {
	route := &model.Route{}
	err := s.pool.QueryRow(ctx, `
		SELECT id, connector_id, chat_id, event_filter, template, is_active, created_at, updated_at
		FROM integration_routes
		WHERE id = $1
	`, id).Scan(
		&route.ID,
		&route.ConnectorID,
		&route.ChatID,
		&route.EventFilter,
		&route.Template,
		&route.IsActive,
		&route.CreatedAt,
		&route.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get route by id: %w", err)
	}

	return route, nil
}

func (s *routeStore) ListByConnector(ctx context.Context, connectorID uuid.UUID) ([]model.Route, error) {
	return s.list(ctx, `connector_id = $1`, connectorID)
}

func (s *routeStore) ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.Route, error) {
	return s.list(ctx, `chat_id = $1`, chatID)
}

func (s *routeStore) Update(ctx context.Context, r *model.Route) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE integration_routes
		SET event_filter = $1,
		    template = $2,
		    is_active = $3,
		    updated_at = NOW()
		WHERE id = $4
	`, r.EventFilter, r.Template, r.IsActive, r.ID)
	if err != nil {
		return fmt.Errorf("update route: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrRouteNotFound
	}

	return nil
}

func (s *routeStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM integration_routes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete route: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return model.ErrRouteNotFound
	}

	return nil
}

func (s *routeStore) FindMatchingRoutes(ctx context.Context, connectorID uuid.UUID, eventType string) ([]model.Route, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, connector_id, chat_id, event_filter, template, is_active, created_at, updated_at
		FROM integration_routes
		WHERE connector_id = $1
		  AND is_active = true
		  AND (
			event_filter IS NULL
			OR event_filter = ''
			OR event_filter = $2
			OR $2 LIKE REPLACE(event_filter, '*', '%')
		  )
		ORDER BY created_at ASC
	`, connectorID, eventType)
	if err != nil {
		return nil, fmt.Errorf("find matching routes: %w", err)
	}
	defer rows.Close()

	return scanRoutes(rows)
}

func (s *routeStore) list(ctx context.Context, where string, arg uuid.UUID) ([]model.Route, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, connector_id, chat_id, event_filter, template, is_active, created_at, updated_at
		FROM integration_routes
		WHERE `+where+`
		ORDER BY created_at DESC
	`, arg)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	defer rows.Close()

	return scanRoutes(rows)
}

func scanRoutes(rows pgx.Rows) ([]model.Route, error) {
	routes := make([]model.Route, 0)
	for rows.Next() {
		var route model.Route
		if err := rows.Scan(
			&route.ID,
			&route.ConnectorID,
			&route.ChatID,
			&route.EventFilter,
			&route.Template,
			&route.IsActive,
			&route.CreatedAt,
			&route.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan route: %w", err)
		}
		routes = append(routes, route)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate routes: %w", err)
	}

	return routes, nil
}
