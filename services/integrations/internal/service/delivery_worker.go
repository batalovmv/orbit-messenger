// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/services/integrations/internal/client"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
	"github.com/mst-corp/orbit/services/integrations/internal/store"
)

type DeliveryWorker struct {
	deliveries store.DeliveryStore
	msgClient  *client.MessagingClient
	logger     *slog.Logger
}

func NewDeliveryWorker(deliveries store.DeliveryStore, msgClient *client.MessagingClient, logger *slog.Logger) *DeliveryWorker {
	return &DeliveryWorker{
		deliveries: deliveries,
		msgClient:  msgClient,
		logger:     logger,
	}
}

func (w *DeliveryWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *DeliveryWorker) processBatch(ctx context.Context) {
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	deliveries, err := w.deliveries.GetPendingRetries(runCtx, 50)
	if err != nil {
		w.logger.Error("failed to load integration retries", "error", err)
		return
	}

	for _, delivery := range deliveries {
		w.retryDelivery(runCtx, delivery)
	}
}

func (w *DeliveryWorker) retryDelivery(ctx context.Context, delivery model.Delivery) {
	payload, err := decodeDeliveryMessagePayload(delivery.Payload)
	if err != nil {
		w.logger.Error("invalid integration delivery payload",
			"delivery_id", delivery.ID,
			"connector_id", delivery.ConnectorID,
			"attempt_count", delivery.AttemptCount,
			"error", err,
		)
		if markErr := w.deliveries.MarkDeadLetter(ctx, delivery.ID, fmt.Sprintf("invalid delivery payload: %v", err)); markErr != nil {
			w.logger.Error("failed to move invalid delivery to dead letter",
				"delivery_id", delivery.ID,
				"error", markErr,
			)
		}
		return
	}

	attemptNumber := delivery.AttemptCount + 1
	attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	message, err := w.sendDelivery(attemptCtx, delivery.OrbitMessageID, payload)
	if err != nil {
		w.logger.Error("integration delivery retry failed",
			"delivery_id", delivery.ID,
			"connector_id", delivery.ConnectorID,
			"attempt_count", attemptNumber,
			"error", err,
		)

		if attemptNumber >= delivery.MaxAttempts {
			if markErr := w.deliveries.MarkDeadLetter(ctx, delivery.ID, err.Error()); markErr != nil {
				w.logger.Error("failed to mark delivery dead letter",
					"delivery_id", delivery.ID,
					"connector_id", delivery.ConnectorID,
					"attempt_count", attemptNumber,
					"error", markErr,
				)
			}
			w.recordAttempt(ctx, delivery.ID, attemptNumber, deliveryStatusDeadLetter, err)
			return
		}

		retryAt := time.Now().UTC().Add(nextRetryDelay(attemptNumber))
		lastError := err.Error()
		if updateErr := w.deliveries.UpdateStatus(ctx, delivery.ID, deliveryStatusFailed, &lastError, &retryAt, nil); updateErr != nil {
			w.logger.Error("failed to update retry status",
				"delivery_id", delivery.ID,
				"connector_id", delivery.ConnectorID,
				"attempt_count", attemptNumber,
				"error", updateErr,
			)
		}
		w.recordAttempt(ctx, delivery.ID, attemptNumber, deliveryStatusFailed, err)
		return
	}

	if err := w.deliveries.UpdateStatus(ctx, delivery.ID, deliveryStatusDelivered, nil, nil, uuidPtr(message.ID)); err != nil {
		w.logger.Error("failed to mark retry as delivered",
			"delivery_id", delivery.ID,
			"connector_id", delivery.ConnectorID,
			"attempt_count", attemptNumber,
			"error", err,
		)
		return
	}

	w.recordAttempt(ctx, delivery.ID, attemptNumber, deliveryStatusDelivered, nil)

	w.logger.Info("integration delivery retry succeeded",
		"delivery_id", delivery.ID,
		"connector_id", delivery.ConnectorID,
		"attempt_count", attemptNumber,
	)
}

// recordAttempt mirrors IntegrationService.recordAttempt for the retry worker;
// kept local because the worker owns a DeliveryStore but not the full service.
func (w *DeliveryWorker) recordAttempt(ctx context.Context, deliveryID uuid.UUID, attemptNo int, status string, runErr error) {
	attempt := &model.DeliveryAttempt{
		DeliveryID: deliveryID,
		AttemptNo:  attemptNo,
		Status:     status,
	}
	if runErr != nil {
		errMsg := runErr.Error()
		attempt.Error = &errMsg
	}
	if err := w.deliveries.InsertAttempt(ctx, attempt); err != nil {
		w.logger.Error("failed to record retry attempt",
			"delivery_id", deliveryID,
			"attempt_no", attemptNo,
			"error", err,
		)
	}
}

func (w *DeliveryWorker) sendDelivery(ctx context.Context, orbitMessageID *uuid.UUID, payload *deliveryMessagePayload) (*client.MessageResponse, error) {
	if orbitMessageID != nil {
		return w.msgClient.EditMessage(ctx, payload.SenderID, *orbitMessageID, payload.Content, payload.ReplyMarkup)
	}

	return w.msgClient.SendMessage(ctx, payload.SenderID, payload.ChatID, payload.Content, payload.MessageType, payload.ReplyMarkup, nil)
}
