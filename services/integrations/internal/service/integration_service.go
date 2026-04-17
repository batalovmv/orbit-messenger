package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mst-corp/orbit/pkg/apperror"
	orchidCrypto "github.com/mst-corp/orbit/pkg/crypto"
	"github.com/mst-corp/orbit/services/integrations/internal/client"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
	"github.com/mst-corp/orbit/services/integrations/internal/store"
)

const (
	deliveryStatusPending    = "pending"
	deliveryStatusDelivered  = "delivered"
	deliveryStatusFailed     = "failed"
	deliveryStatusDeadLetter = "dead_letter"
	defaultMessageType       = "text"
	defaultMaxAttempts       = 5
)

var templatePlaceholderRegex = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}`)

type IntegrationService struct {
	connectors    store.ConnectorStore
	routes        store.RouteStore
	deliveries    store.DeliveryStore
	msgClient     *client.MessagingClient
	encryptionKey []byte
	logger        *slog.Logger
}

type UpdateConnectorInput struct {
	Name        *string
	DisplayName *string
	Type        *string
	BotID       *uuid.UUID
	Config      *model.JSONB
	IsActive    *bool
}

type deliveryMessagePayload struct {
	ChatID      uuid.UUID       `json:"chat_id"`
	SenderID    uuid.UUID       `json:"sender_id"`
	Content     string          `json:"content"`
	MessageType string          `json:"message_type"`
	ReplyMarkup json.RawMessage `json:"reply_markup,omitempty"`
	Source      json.RawMessage `json:"source_payload"`
}

type deliveryFilterer interface {
	ListByConnectorFiltered(ctx context.Context, connectorID uuid.UUID, status *string, limit, offset int) ([]model.Delivery, int, error)
}

func NewIntegrationService(
	connectors store.ConnectorStore,
	routes store.RouteStore,
	deliveries store.DeliveryStore,
	msgClient *client.MessagingClient,
	encryptionKey []byte,
	logger *slog.Logger,
) *IntegrationService {
	return &IntegrationService{
		connectors:    connectors,
		routes:        routes,
		deliveries:    deliveries,
		msgClient:     msgClient,
		encryptionKey: encryptionKey,
		logger:        logger,
	}
}

func (s *IntegrationService) CreateConnector(ctx context.Context, createdBy uuid.UUID, req model.CreateConnectorRequest) (*model.Connector, string, error) {
	rawSecret, err := generateWebhookSecret()
	if err != nil {
		return nil, "", fmt.Errorf("generate webhook secret: %w", err)
	}
	encrypted, err := orchidCrypto.Encrypt(rawSecret, s.encryptionKey)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt webhook secret: %w", err)
	}

	connector := &model.Connector{
		Name:        strings.TrimSpace(req.Name),
		DisplayName: strings.TrimSpace(req.DisplayName),
		Type:        strings.TrimSpace(req.Type),
		Config:      model.JSONB([]byte("{}")),
		SecretHash:  &encrypted,
		IsActive:    true,
		CreatedBy:   createdBy,
	}
	if req.BotID != nil && strings.TrimSpace(*req.BotID) != "" {
		botID, err := uuid.Parse(strings.TrimSpace(*req.BotID))
		if err != nil {
			return nil, "", apperror.BadRequest("Invalid bot_id")
		}
		connector.BotID = &botID
	}

	if err := s.connectors.Create(ctx, connector); err != nil {
		if errors.Is(err, model.ErrConnectorAlreadyExists) {
			return nil, "", apperror.Conflict("Connector already exists")
		}
		return nil, "", fmt.Errorf("create connector: %w", err)
	}

	connector.SecretHash = nil
	return connector, rawSecret, nil
}

func (s *IntegrationService) GetConnector(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
	connector, err := s.connectors.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get connector: %w", err)
	}
	if connector == nil {
		return nil, apperror.NotFound("Connector not found")
	}

	return connector, nil
}

func (s *IntegrationService) ListConnectors(ctx context.Context, limit, offset int) ([]model.Connector, int, error) {
	connectors, total, err := s.connectors.List(ctx, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list connectors: %w", err)
	}

	return connectors, total, nil
}

func (s *IntegrationService) UpdateConnector(ctx context.Context, id uuid.UUID, updates UpdateConnectorInput) (*model.Connector, error) {
	connector, err := s.connectors.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get connector for update: %w", err)
	}
	if connector == nil {
		return nil, apperror.NotFound("Connector not found")
	}

	if updates.Name != nil {
		connector.Name = strings.TrimSpace(*updates.Name)
	}
	if updates.DisplayName != nil {
		connector.DisplayName = strings.TrimSpace(*updates.DisplayName)
	}
	if updates.Type != nil {
		connector.Type = strings.TrimSpace(*updates.Type)
	}
	if updates.BotID != nil {
		connector.BotID = updates.BotID
	}
	if updates.Config != nil {
		connector.Config = *updates.Config
	}
	if updates.IsActive != nil {
		connector.IsActive = *updates.IsActive
	}

	if err := s.connectors.Update(ctx, connector); err != nil {
		if errors.Is(err, model.ErrConnectorNotFound) {
			return nil, apperror.NotFound("Connector not found")
		}
		if errors.Is(err, model.ErrConnectorAlreadyExists) {
			return nil, apperror.Conflict("Connector already exists")
		}
		return nil, fmt.Errorf("update connector: %w", err)
	}

	updated, err := s.connectors.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get updated connector: %w", err)
	}
	if updated == nil {
		return nil, apperror.NotFound("Connector not found")
	}

	return updated, nil
}

func (s *IntegrationService) DeleteConnector(ctx context.Context, id uuid.UUID) error {
	if err := s.connectors.Delete(ctx, id); err != nil {
		if errors.Is(err, model.ErrConnectorNotFound) {
			return apperror.NotFound("Connector not found")
		}
		return fmt.Errorf("delete connector: %w", err)
	}

	return nil
}

func (s *IntegrationService) RotateSecret(ctx context.Context, id uuid.UUID) (string, error) {
	connector, err := s.connectors.GetByID(ctx, id)
	if err != nil {
		return "", fmt.Errorf("get connector for rotate secret: %w", err)
	}
	if connector == nil {
		return "", apperror.NotFound("Connector not found")
	}

	rawSecret, err := generateWebhookSecret()
	if err != nil {
		return "", fmt.Errorf("generate connector secret: %w", err)
	}
	encrypted, err := orchidCrypto.Encrypt(rawSecret, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("encrypt connector secret: %w", err)
	}
	if err := s.connectors.SetSecretHash(ctx, id, encrypted); err != nil {
		if errors.Is(err, model.ErrConnectorNotFound) {
			return "", apperror.NotFound("Connector not found")
		}
		return "", fmt.Errorf("set connector secret hash: %w", err)
	}

	return rawSecret, nil
}

func (s *IntegrationService) CreateRoute(ctx context.Context, route *model.Route) (*model.Route, error) {
	if err := s.routes.Create(ctx, route); err != nil {
		if errors.Is(err, model.ErrDuplicateRoute) {
			return nil, apperror.Conflict("Route already exists for this chat")
		}
		return nil, fmt.Errorf("create route: %w", err)
	}

	return route, nil
}

func (s *IntegrationService) ListRoutes(ctx context.Context, connectorID uuid.UUID) ([]model.Route, error) {
	routes, err := s.routes.ListByConnector(ctx, connectorID)
	if err != nil {
		return nil, fmt.Errorf("list routes: %w", err)
	}
	return routes, nil
}

func (s *IntegrationService) DeleteRoute(ctx context.Context, id uuid.UUID) error {
	if err := s.routes.Delete(ctx, id); err != nil {
		if errors.Is(err, model.ErrRouteNotFound) {
			return apperror.NotFound("Route not found")
		}
		return fmt.Errorf("delete route: %w", err)
	}

	return nil
}

// UpdateRouteInput holds the patchable route fields. nil means "leave as-is",
// empty string for EventFilter/Template means "clear".
type UpdateRouteInput struct {
	EventFilter *string
	Template    *string
	IsActive    *bool
}

func (s *IntegrationService) UpdateRoute(ctx context.Context, id uuid.UUID, input UpdateRouteInput) (*model.Route, error) {
	route, err := s.routes.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get route for update: %w", err)
	}
	if route == nil {
		return nil, apperror.NotFound("Route not found")
	}

	if input.EventFilter != nil {
		route.EventFilter = normalizePtr(*input.EventFilter)
	}
	if input.Template != nil {
		route.Template = normalizePtr(*input.Template)
	}
	if input.IsActive != nil {
		route.IsActive = *input.IsActive
	}

	if err := s.routes.Update(ctx, route); err != nil {
		if errors.Is(err, model.ErrRouteNotFound) {
			return nil, apperror.NotFound("Route not found")
		}
		return nil, fmt.Errorf("update route: %w", err)
	}

	return route, nil
}

// ConnectorStats is a 24h (or custom window) summary of delivery outcomes.
type ConnectorStats struct {
	Window         string     `json:"window"`
	Total          int        `json:"total"`
	Delivered      int        `json:"delivered"`
	Failed         int        `json:"failed"`
	Pending        int        `json:"pending"`
	DeadLetter     int        `json:"dead_letter"`
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
}

// TestConnectorResult is returned from POST /integrations/connectors/:id/test.
type TestConnectorResult struct {
	DeliveryIDs []uuid.UUID `json:"delivery_ids"`
	RouteCount  int         `json:"route_count"`
	EventType   string      `json:"event_type"`
}

// TestConnector fires a synthetic payload through the normal route/template
// pipeline, skipping signature verification and idempotency dedup. Returns
// the created delivery IDs so the admin UI can link directly to them.
func (s *IntegrationService) TestConnector(ctx context.Context, connectorID uuid.UUID, eventType string, payload map[string]any) (*TestConnectorResult, error) {
	if strings.TrimSpace(eventType) == "" {
		eventType = "test.event"
	}

	connector, err := s.connectors.GetByID(ctx, connectorID)
	if err != nil {
		return nil, fmt.Errorf("get connector for test: %w", err)
	}
	if connector == nil {
		return nil, apperror.NotFound("Connector not found")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, apperror.BadRequest("payload must be JSON-serialisable")
	}

	routes, err := s.routes.FindMatchingRoutes(ctx, connectorID, eventType)
	if err != nil {
		return nil, fmt.Errorf("find routes for test: %w", err)
	}
	if len(routes) == 0 {
		return nil, apperror.BadRequest("no active routes match this event_type; add a route before testing")
	}

	senderID := connector.CreatedBy
	if connector.BotID != nil {
		if botUserID, err := s.connectors.GetBotUserID(ctx, *connector.BotID); err == nil {
			senderID = botUserID
		}
	}

	result := &TestConnectorResult{RouteCount: len(routes), EventType: eventType}
	for _, route := range routes {
		msgPayload, err := buildDeliveryMessagePayload(connector, route, eventType, payloadBytes, senderID)
		if err != nil {
			return nil, fmt.Errorf("build test delivery payload: %w", err)
		}

		nextRetryAt := time.Now().UTC()
		testType := "test." + eventType
		delivery := &model.Delivery{
			ConnectorID:  connectorID,
			RouteID:      uuidPtr(route.ID),
			EventType:    testType,
			Payload:      msgPayload,
			Status:       deliveryStatusPending,
			AttemptCount: 0,
			MaxAttempts:  defaultMaxAttempts,
			NextRetryAt:  &nextRetryAt,
		}
		if err := s.deliveries.Create(ctx, delivery); err != nil {
			return nil, fmt.Errorf("create test delivery: %w", err)
		}

		message, err := s.dispatchDelivery(ctx, delivery)
		if err != nil {
			lastError := err.Error()
			retryAt := time.Now().UTC().Add(nextRetryDelay(1))
			if updateErr := s.deliveries.UpdateStatus(ctx, delivery.ID, deliveryStatusFailed, &lastError, &retryAt, nil); updateErr != nil {
				s.logger.Error("failed to mark test delivery as failed",
					"delivery_id", delivery.ID, "error", updateErr)
			}
			s.recordAttempt(ctx, delivery.ID, 1, deliveryStatusFailed, nil, "", err)
		} else {
			if err := s.deliveries.UpdateStatus(ctx, delivery.ID, deliveryStatusDelivered, nil, nil, &message.ID); err != nil {
				s.logger.Error("failed to mark test delivery as delivered",
					"delivery_id", delivery.ID, "error", err)
			}
			s.recordAttempt(ctx, delivery.ID, 1, deliveryStatusDelivered, nil, "", nil)
		}
		result.DeliveryIDs = append(result.DeliveryIDs, delivery.ID)
	}

	return result, nil
}

func (s *IntegrationService) GetConnectorStats(ctx context.Context, connectorID uuid.UUID, window time.Duration) (*ConnectorStats, error) {
	if window <= 0 {
		window = 24 * time.Hour
	}
	row, err := s.deliveries.ConnectorStats(ctx, connectorID, window)
	if err != nil {
		return nil, fmt.Errorf("connector stats: %w", err)
	}
	stats := &ConnectorStats{
		Window:         window.String(),
		Total:          row.Total,
		Delivered:      row.Delivered,
		Failed:         row.Failed,
		Pending:        row.Pending,
		DeadLetter:     row.DeadLetter,
		LastDeliveryAt: row.LastDeliveryAt,
	}
	return stats, nil
}

func (s *IntegrationService) ListDeliveries(ctx context.Context, connectorID uuid.UUID, status *string, limit, offset int) ([]model.Delivery, int, error) {
	if filterer, ok := s.deliveries.(deliveryFilterer); ok {
		deliveries, total, err := filterer.ListByConnectorFiltered(ctx, connectorID, status, limit, offset)
		if err != nil {
			return nil, 0, fmt.Errorf("list deliveries: %w", err)
		}
		return deliveries, total, nil
	}

	deliveries, total, err := s.deliveries.ListByConnector(ctx, connectorID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list deliveries: %w", err)
	}
	if status == nil {
		return deliveries, total, nil
	}

	filtered := make([]model.Delivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		if delivery.Status == *status {
			filtered = append(filtered, delivery)
		}
	}

	return filtered, len(filtered), nil
}

func (s *IntegrationService) GetDelivery(ctx context.Context, id uuid.UUID) (*model.Delivery, error) {
	delivery, err := s.deliveries.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get delivery: %w", err)
	}
	if delivery == nil {
		return nil, apperror.NotFound("Delivery not found")
	}

	attempts, err := s.deliveries.ListAttempts(ctx, id)
	if err != nil {
		s.logger.Error("failed to list delivery attempts", "delivery_id", id, "error", err)
	} else {
		delivery.Attempts = attempts
	}

	return delivery, nil
}

func (s *IntegrationService) RetryDelivery(ctx context.Context, id uuid.UUID) error {
	delivery, err := s.deliveries.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get delivery for retry: %w", err)
	}
	if delivery == nil {
		return apperror.NotFound("Delivery not found")
	}

	now := time.Now().UTC()
	if err := s.deliveries.UpdateStatus(ctx, id, deliveryStatusPending, nil, &now, nil); err != nil {
		return fmt.Errorf("reset delivery status: %w", err)
	}

	return nil
}

func (s *IntegrationService) VerifySignature(ctx context.Context, connectorID uuid.UUID, payload []byte, signature string, timestamp string) error {
	secretEnc, err := s.connectors.GetSecretHash(ctx, connectorID)
	if err != nil {
		if errors.Is(err, model.ErrConnectorNotFound) {
			return apperror.NotFound("Connector not found")
		}
		return fmt.Errorf("get connector secret: %w", err)
	}
	if strings.TrimSpace(secretEnc) == "" {
		return nil
	}

	// When connector has a secret, signature AND timestamp are both required
	normalizedSignature := normalizeSignature(signature)
	if normalizedSignature == "" {
		return apperror.Unauthorized("Missing webhook signature")
	}

	// Decrypt to get the raw secret for HMAC verification
	rawSecret, err := orchidCrypto.Decrypt(secretEnc, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("decrypt connector secret: %w", err)
	}

	// Include timestamp in HMAC to prevent replay attacks
	expected := signPayload(rawSecret, payload, timestamp)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(normalizedSignature)) != 1 {
		return apperror.Unauthorized("Invalid webhook signature")
	}

	return nil
}

func (s *IntegrationService) ProcessInboundWebhook(
	ctx context.Context,
	connectorID uuid.UUID,
	eventType string,
	payload model.JSONB,
	signature string,
	timestamp string,
	correlationKey string,
	externalEventID string,
) error {
	if strings.TrimSpace(eventType) == "" {
		return apperror.BadRequest("event_type is required")
	}

	payloadBytes := []byte(payload)
	if !json.Valid(payloadBytes) {
		return apperror.BadRequest("Invalid webhook payload")
	}

	connector, err := s.connectors.GetByID(ctx, connectorID)
	if err != nil {
		return fmt.Errorf("get connector for inbound webhook: %w", err)
	}
	if connector == nil {
		return apperror.NotFound("Connector not found")
	}
	if !connector.IsActive {
		return apperror.Forbidden("Connector is deactivated")
	}

	if err := s.VerifySignature(ctx, connectorID, payloadBytes, signature, timestamp); err != nil {
		return err
	}

	if externalEventID = strings.TrimSpace(externalEventID); externalEventID != "" {
		existing, err := s.deliveries.FindByExternalID(ctx, connectorID, externalEventID)
		if err != nil {
			return fmt.Errorf("check delivery idempotency: %w", err)
		}
		if existing != nil {
			s.logger.Info("duplicate integration event skipped",
				"connector_id", connectorID,
				"external_event_id", externalEventID,
			)
			return nil
		}
	}

	routes, err := s.routes.FindMatchingRoutes(ctx, connectorID, strings.TrimSpace(eventType))
	if err != nil {
		return fmt.Errorf("find matching routes: %w", err)
	}
	if len(routes) == 0 {
		s.logger.Info("no integration routes matched",
			"connector_id", connectorID,
			"event_type", eventType,
		)
		return nil
	}

	// Use bot's user ID as sender if a bot is linked, otherwise fall back to connector creator.
	senderID := connector.CreatedBy
	if connector.BotID != nil {
		botUserID, err := s.connectors.GetBotUserID(ctx, *connector.BotID)
		if err != nil {
			s.logger.Warn("failed to resolve bot user_id, falling back to connector creator",
				"bot_id", *connector.BotID, "error", err)
		} else {
			senderID = botUserID
		}
	}
	for _, route := range routes {
		msgPayload, err := buildDeliveryMessagePayload(connector, route, eventType, payloadBytes, senderID)
		if err != nil {
			return fmt.Errorf("build delivery payload: %w", err)
		}

		var existingMessageID *uuid.UUID
		if correlation := strings.TrimSpace(correlationKey); correlation != "" {
			existing, err := s.deliveries.FindByCorrelation(ctx, connectorID, correlation)
			if err != nil {
				return fmt.Errorf("find delivery by correlation: %w", err)
			}
			if existing != nil && existing.OrbitMessageID != nil && existing.RouteID != nil && *existing.RouteID == route.ID {
				existingMessageID = existing.OrbitMessageID
			}
		}

		nextRetryAt := time.Now().UTC()
		delivery := &model.Delivery{
			ConnectorID:     connectorID,
			RouteID:         uuidPtr(route.ID),
			ExternalEventID: stringPtr(externalEventID),
			EventType:       strings.TrimSpace(eventType),
			Payload:         msgPayload,
			Status:          deliveryStatusPending,
			OrbitMessageID:  existingMessageID,
			CorrelationKey:  stringPtr(correlationKey),
			AttemptCount:    0,
			MaxAttempts:     defaultMaxAttempts,
			NextRetryAt:     &nextRetryAt,
		}

		if err := s.deliveries.Create(ctx, delivery); err != nil {
			return fmt.Errorf("create delivery record: %w", err)
		}

		message, err := s.dispatchDelivery(ctx, delivery)
		if err != nil {
			lastError := err.Error()
			retryAt := time.Now().UTC().Add(nextRetryDelay(delivery.AttemptCount + 1))
			updateErr := s.deliveries.UpdateStatus(ctx, delivery.ID, deliveryStatusFailed, &lastError, &retryAt, nil)
			if updateErr != nil {
				s.logger.Error("failed to mark delivery as failed",
					"delivery_id", delivery.ID,
					"connector_id", connectorID,
					"error", updateErr,
				)
			}
			s.recordAttempt(ctx, delivery.ID, 1, deliveryStatusFailed, nil, "", err)
			s.logger.Error("integration delivery failed",
				"delivery_id", delivery.ID,
				"connector_id", connectorID,
				"route_id", route.ID,
				"error", err,
			)
			continue
		}

		if err := s.deliveries.UpdateStatus(ctx, delivery.ID, deliveryStatusDelivered, nil, nil, &message.ID); err != nil {
			return fmt.Errorf("mark delivery as delivered: %w", err)
		}
		s.recordAttempt(ctx, delivery.ID, 1, deliveryStatusDelivered, nil, "", nil)
	}

	return nil
}

func (s *IntegrationService) dispatchDelivery(ctx context.Context, delivery *model.Delivery) (*client.MessageResponse, error) {
	payload, err := decodeDeliveryMessagePayload(delivery.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode delivery payload: %w", err)
	}

	if delivery.OrbitMessageID != nil {
		return s.msgClient.EditMessage(ctx, payload.SenderID, *delivery.OrbitMessageID, payload.Content, payload.ReplyMarkup)
	}

	return s.msgClient.SendMessage(ctx, payload.SenderID, payload.ChatID, payload.Content, payload.MessageType, payload.ReplyMarkup, nil)
}

// recordAttempt writes a row into integration_delivery_attempts for the given
// attempt. Logs and swallows errors — attempt history is diagnostic, it must
// never break the main delivery path.
func (s *IntegrationService) recordAttempt(
	ctx context.Context,
	deliveryID uuid.UUID,
	attemptNo int,
	status string,
	responseStatus *int,
	responseBody string,
	runErr error,
) {
	attempt := &model.DeliveryAttempt{
		DeliveryID:     deliveryID,
		AttemptNo:      attemptNo,
		Status:         status,
		ResponseStatus: responseStatus,
	}
	if snippet := truncate(responseBody, 1024); snippet != "" {
		attempt.ResponseBodySnippet = &snippet
	}
	if runErr != nil {
		errMsg := runErr.Error()
		attempt.Error = &errMsg
	}
	if err := s.deliveries.InsertAttempt(ctx, attempt); err != nil {
		s.logger.Error("failed to record delivery attempt",
			"delivery_id", deliveryID,
			"attempt_no", attemptNo,
			"error", err,
		)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// normalizePtr trims input and returns nil when the result is empty, mirroring
// the nullable-string contract used throughout the integrations service.
func normalizePtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func buildDeliveryMessagePayload(
	connector *model.Connector,
	route model.Route,
	eventType string,
	sourcePayload []byte,
	senderID uuid.UUID,
) (model.JSONB, error) {
	content, err := renderDeliveryMessage(connector, route, eventType, sourcePayload)
	if err != nil {
		return nil, err
	}

	payload := deliveryMessagePayload{
		ChatID:      route.ChatID,
		SenderID:    senderID,
		Content:     content,
		MessageType: defaultMessageType,
		Source:      json.RawMessage(append([]byte(nil), sourcePayload...)),
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal delivery payload: %w", err)
	}

	return model.JSONB(raw), nil
}

func decodeDeliveryMessagePayload(raw model.JSONB) (*deliveryMessagePayload, error) {
	var payload deliveryMessagePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal delivery payload: %w", err)
	}
	if payload.MessageType == "" {
		payload.MessageType = defaultMessageType
	}
	return &payload, nil
}

func renderDeliveryMessage(connector *model.Connector, route model.Route, eventType string, sourcePayload []byte) (string, error) {
	template := ""
	if route.Template != nil {
		template = strings.TrimSpace(*route.Template)
	}

	if template == "" {
		return defaultDeliveryMessage(connector, eventType, sourcePayload)
	}

	values, err := buildTemplateValues(connector, eventType, sourcePayload)
	if err != nil {
		return "", fmt.Errorf("build template values: %w", err)
	}

	rendered := templatePlaceholderRegex.ReplaceAllStringFunc(template, func(token string) string {
		matches := templatePlaceholderRegex.FindStringSubmatch(token)
		if len(matches) != 2 {
			return token
		}
		key := matches[1]
		// Strip leading dot — templates use {{.data.message}} but keys are "data.message"
		key = strings.TrimPrefix(key, ".")
		if value, ok := values[key]; ok {
			return value
		}
		return ""
	})

	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return defaultDeliveryMessage(connector, eventType, sourcePayload)
	}

	return rendered, nil
}

func buildTemplateValues(connector *model.Connector, eventType string, sourcePayload []byte) (map[string]string, error) {
	values := map[string]string{
		"event":                  strings.TrimSpace(eventType),
		"event_type":             strings.TrimSpace(eventType),
		"connector.name":         connector.Name,
		"connector.display_name": connector.DisplayName,
		"payload":                compactJSON(sourcePayload),
	}

	var decoded any
	if err := json.Unmarshal(sourcePayload, &decoded); err != nil {
		return nil, fmt.Errorf("unmarshal source payload: %w", err)
	}
	flattenTemplateValues(values, "", decoded)

	return values, nil
}

func flattenTemplateValues(values map[string]string, prefix string, current any) {
	switch value := current.(type) {
	case map[string]any:
		for key, item := range value {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flattenTemplateValues(values, next, item)
		}
	case []any:
		encoded, err := json.Marshal(value)
		if err == nil && prefix != "" {
			values[prefix] = string(encoded)
		}
	case nil:
		if prefix != "" {
			values[prefix] = ""
		}
	default:
		if prefix != "" {
			values[prefix] = fmt.Sprint(value)
		}
	}
}

func defaultDeliveryMessage(connector *model.Connector, eventType string, sourcePayload []byte) (string, error) {
	var formatted bytes.Buffer
	if err := json.Indent(&formatted, sourcePayload, "", "  "); err != nil {
		return "", fmt.Errorf("format source payload: %w", err)
	}

	title := connector.DisplayName
	if strings.TrimSpace(title) == "" {
		title = connector.Name
	}

	return fmt.Sprintf("[%s] %s\n%s", title, strings.TrimSpace(eventType), formatted.String()), nil
}

func compactJSON(raw []byte) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

func generateWebhookSecret() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(random), nil
}

func signPayload(rawSecret string, payload []byte, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(rawSecret))
	if timestamp != "" {
		mac.Write([]byte(timestamp))
		mac.Write([]byte("."))
	}
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeSignature(signature string) string {
	value := strings.ToLower(strings.TrimSpace(signature))
	value = strings.TrimPrefix(value, "sha256=")
	return value
}

func nextRetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 30 * time.Second
	case 2:
		return 2 * time.Minute
	case 3:
		return 10 * time.Minute
	case 4:
		return time.Hour
	default:
		return 6 * time.Hour
	}
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func uuidPtr(value uuid.UUID) *uuid.UUID {
	return &value
}
