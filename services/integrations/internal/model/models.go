package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Connector struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	DisplayName string     `json:"display_name"`
	Type        string     `json:"type"`
	BotID       *uuid.UUID `json:"bot_id,omitempty"`
	Config      JSONB      `json:"config"`
	SecretHash  *string    `json:"-"`
	IsActive    bool       `json:"is_active"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type Route struct {
	ID          uuid.UUID `json:"id"`
	ConnectorID uuid.UUID `json:"connector_id"`
	ChatID      uuid.UUID `json:"chat_id"`
	EventFilter *string   `json:"event_filter,omitempty"`
	Template    *string   `json:"template,omitempty"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Delivery struct {
	ID              uuid.UUID  `json:"id"`
	ConnectorID     uuid.UUID  `json:"connector_id"`
	RouteID         *uuid.UUID `json:"route_id,omitempty"`
	ExternalEventID *string    `json:"external_event_id,omitempty"`
	EventType       string     `json:"event_type"`
	Payload         JSONB      `json:"payload"`
	Status          string     `json:"status"`
	OrbitMessageID  *uuid.UUID `json:"orbit_message_id,omitempty"`
	CorrelationKey  *string    `json:"correlation_key,omitempty"`
	AttemptCount    int        `json:"attempt_count"`
	MaxAttempts     int        `json:"max_attempts"`
	LastError       *string    `json:"last_error,omitempty"`
	NextRetryAt     *time.Time `json:"next_retry_at,omitempty"`
	DeliveredAt     *time.Time `json:"delivered_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type JSONB json.RawMessage

func (j *JSONB) Scan(src any) error {
	if src == nil {
		*j = JSONB([]byte("{}"))
		return nil
	}

	switch value := src.(type) {
	case []byte:
		*j = JSONB(append([]byte(nil), value...))
		return nil
	case string:
		*j = JSONB([]byte(value))
		return nil
	default:
		return fmt.Errorf("unsupported JSONB source type %T", src)
	}
}

func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return []byte("{}"), nil
	}
	if !json.Valid(j) {
		return nil, fmt.Errorf("invalid JSONB value")
	}
	return []byte(j), nil
}

var (
	ErrConnectorNotFound = errors.New("connector not found")
	ErrConnectorAlreadyExists = errors.New("connector already exists")
	ErrRouteNotFound     = errors.New("route not found")
	ErrDuplicateRoute    = errors.New("route already exists for this connector and chat")
	ErrInvalidSignature  = errors.New("invalid webhook signature")
)

type CreateConnectorRequest struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Type        string  `json:"type"`
	BotID       *string `json:"bot_id,omitempty"`
}
