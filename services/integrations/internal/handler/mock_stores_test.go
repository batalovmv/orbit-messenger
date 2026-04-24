// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
	"github.com/mst-corp/orbit/services/integrations/internal/store"
)

type mockConnectorStore struct {
	createFn        func(ctx context.Context, c *model.Connector) error
	getByIDFn       func(ctx context.Context, id uuid.UUID) (*model.Connector, error)
	getByNameFn     func(ctx context.Context, name string) (*model.Connector, error)
	listFn          func(ctx context.Context, limit, offset int) ([]model.Connector, int, error)
	updateFn        func(ctx context.Context, c *model.Connector) error
	deleteFn        func(ctx context.Context, id uuid.UUID) error
	getSecretHashFn func(ctx context.Context, id uuid.UUID) (string, error)
	setSecretHashFn func(ctx context.Context, id uuid.UUID, hash string) error
	getBotUserIDFn  func(ctx context.Context, botID uuid.UUID) (uuid.UUID, error)
}

func (m *mockConnectorStore) Create(ctx context.Context, c *model.Connector) error {
	if m.createFn != nil {
		return m.createFn(ctx, c)
	}
	c.ID = uuid.New()
	c.CreatedAt = time.Now().UTC()
	c.UpdatedAt = c.CreatedAt
	return nil
}

func (m *mockConnectorStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockConnectorStore) GetByName(ctx context.Context, name string) (*model.Connector, error) {
	if m.getByNameFn != nil {
		return m.getByNameFn(ctx, name)
	}
	return nil, nil
}

func (m *mockConnectorStore) List(ctx context.Context, limit, offset int) ([]model.Connector, int, error) {
	if m.listFn != nil {
		return m.listFn(ctx, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockConnectorStore) Update(ctx context.Context, c *model.Connector) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, c)
	}
	return nil
}

func (m *mockConnectorStore) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockConnectorStore) GetSecretHash(ctx context.Context, id uuid.UUID) (string, error) {
	if m.getSecretHashFn != nil {
		return m.getSecretHashFn(ctx, id)
	}
	return "", nil
}

func (m *mockConnectorStore) SetSecretHash(ctx context.Context, id uuid.UUID, hash string) error {
	if m.setSecretHashFn != nil {
		return m.setSecretHashFn(ctx, id, hash)
	}
	return nil
}

func (m *mockConnectorStore) GetBotUserID(ctx context.Context, botID uuid.UUID) (uuid.UUID, error) {
	if m.getBotUserIDFn != nil {
		return m.getBotUserIDFn(ctx, botID)
	}
	return uuid.Nil, nil
}

type mockRouteStore struct {
	createFn             func(ctx context.Context, r *model.Route) error
	getByIDFn            func(ctx context.Context, id uuid.UUID) (*model.Route, error)
	listByConnectorFn    func(ctx context.Context, connectorID uuid.UUID) ([]model.Route, error)
	listByChatFn         func(ctx context.Context, chatID uuid.UUID) ([]model.Route, error)
	updateFn             func(ctx context.Context, r *model.Route) error
	deleteFn             func(ctx context.Context, id uuid.UUID) error
	findMatchingRoutesFn func(ctx context.Context, connectorID uuid.UUID, eventType string) ([]model.Route, error)
}

func (m *mockRouteStore) Create(ctx context.Context, r *model.Route) error {
	if m.createFn != nil {
		return m.createFn(ctx, r)
	}
	r.ID = uuid.New()
	r.CreatedAt = time.Now().UTC()
	r.UpdatedAt = r.CreatedAt
	return nil
}

func (m *mockRouteStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Route, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockRouteStore) ListByConnector(ctx context.Context, connectorID uuid.UUID) ([]model.Route, error) {
	if m.listByConnectorFn != nil {
		return m.listByConnectorFn(ctx, connectorID)
	}
	return nil, nil
}

func (m *mockRouteStore) ListByChat(ctx context.Context, chatID uuid.UUID) ([]model.Route, error) {
	if m.listByChatFn != nil {
		return m.listByChatFn(ctx, chatID)
	}
	return nil, nil
}

func (m *mockRouteStore) Update(ctx context.Context, r *model.Route) error {
	if m.updateFn != nil {
		return m.updateFn(ctx, r)
	}
	return nil
}

func (m *mockRouteStore) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, id)
	}
	return nil
}

func (m *mockRouteStore) FindMatchingRoutes(ctx context.Context, connectorID uuid.UUID, eventType string) ([]model.Route, error) {
	if m.findMatchingRoutesFn != nil {
		return m.findMatchingRoutesFn(ctx, connectorID, eventType)
	}
	return nil, nil
}

type mockDeliveryStore struct {
	createFn                   func(ctx context.Context, d *model.Delivery) error
	getByIDFn                  func(ctx context.Context, id uuid.UUID) (*model.Delivery, error)
	listByConnectorFn          func(ctx context.Context, connectorID uuid.UUID, limit, offset int) ([]model.Delivery, int, error)
	listByConnectorFilteredFn  func(ctx context.Context, connectorID uuid.UUID, status *string, limit, offset int) ([]model.Delivery, int, error)
	updateStatusFn             func(ctx context.Context, id uuid.UUID, status string, lastError *string, nextRetryAt *time.Time, orbitMessageID *uuid.UUID) error
	getPendingRetriesFn        func(ctx context.Context, limit int) ([]model.Delivery, error)
	findByCorrelationFn        func(ctx context.Context, connectorID uuid.UUID, correlationKey string) (*model.Delivery, error)
	findByExternalIDFn         func(ctx context.Context, connectorID uuid.UUID, externalEventID string) (*model.Delivery, error)
	markDeadLetterFn           func(ctx context.Context, id uuid.UUID, lastError string) error
	connectorStatsFn           func(ctx context.Context, connectorID uuid.UUID, window time.Duration) (*store.ConnectorStatsRow, error)
}

func (m *mockDeliveryStore) Create(ctx context.Context, d *model.Delivery) error {
	if m.createFn != nil {
		return m.createFn(ctx, d)
	}
	d.ID = uuid.New()
	d.CreatedAt = time.Now().UTC()
	return nil
}

func (m *mockDeliveryStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Delivery, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockDeliveryStore) ListByConnector(ctx context.Context, connectorID uuid.UUID, limit, offset int) ([]model.Delivery, int, error) {
	if m.listByConnectorFn != nil {
		return m.listByConnectorFn(ctx, connectorID, limit, offset)
	}
	if m.listByConnectorFilteredFn != nil {
		return m.listByConnectorFilteredFn(ctx, connectorID, nil, limit, offset)
	}
	return nil, 0, nil
}

func (m *mockDeliveryStore) ListByConnectorFiltered(ctx context.Context, connectorID uuid.UUID, status *string, limit, offset int) ([]model.Delivery, int, error) {
	if m.listByConnectorFilteredFn != nil {
		return m.listByConnectorFilteredFn(ctx, connectorID, status, limit, offset)
	}
	return m.ListByConnector(ctx, connectorID, limit, offset)
}

func (m *mockDeliveryStore) UpdateStatus(ctx context.Context, id uuid.UUID, status string, lastError *string, nextRetryAt *time.Time, orbitMessageID *uuid.UUID) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(ctx, id, status, lastError, nextRetryAt, orbitMessageID)
	}
	return nil
}

func (m *mockDeliveryStore) GetPendingRetries(ctx context.Context, limit int) ([]model.Delivery, error) {
	if m.getPendingRetriesFn != nil {
		return m.getPendingRetriesFn(ctx, limit)
	}
	return nil, nil
}

func (m *mockDeliveryStore) FindByCorrelation(ctx context.Context, connectorID uuid.UUID, correlationKey string) (*model.Delivery, error) {
	if m.findByCorrelationFn != nil {
		return m.findByCorrelationFn(ctx, connectorID, correlationKey)
	}
	return nil, nil
}

func (m *mockDeliveryStore) FindByExternalID(ctx context.Context, connectorID uuid.UUID, externalEventID string) (*model.Delivery, error) {
	if m.findByExternalIDFn != nil {
		return m.findByExternalIDFn(ctx, connectorID, externalEventID)
	}
	return nil, nil
}

func (m *mockDeliveryStore) MarkDeadLetter(ctx context.Context, id uuid.UUID, lastError string) error {
	if m.markDeadLetterFn != nil {
		return m.markDeadLetterFn(ctx, id, lastError)
	}
	return nil
}

func (m *mockDeliveryStore) InsertAttempt(ctx context.Context, attempt *model.DeliveryAttempt) error {
	return nil
}

func (m *mockDeliveryStore) ListAttempts(ctx context.Context, deliveryID uuid.UUID) ([]model.DeliveryAttempt, error) {
	return nil, nil
}

func (m *mockDeliveryStore) ConnectorStats(ctx context.Context, connectorID uuid.UUID, window time.Duration) (*store.ConnectorStatsRow, error) {
	if m.connectorStatsFn != nil {
		return m.connectorStatsFn(ctx, connectorID, window)
	}
	return &store.ConnectorStatsRow{}, nil
}
