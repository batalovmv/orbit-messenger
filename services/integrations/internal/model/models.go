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
	ErrRouteNotFound      = errors.New("route not found")
	ErrDuplicateRoute     = errors.New("route already exists for this connector and chat")
	ErrInvalidSignature   = errors.New("invalid webhook signature")
	ErrDeliveryNotFound   = errors.New("delivery not found")
)

type CreateConnectorRequest struct {
	Name        string  `json:"name"`
	DisplayName string  `json:"display_name"`
	Type        string  `json:"type"`
	BotID       *string `json:"bot_id,omitempty"`
}

// ConnectorConfig is a typed view over Connector.Config (JSONB). All fields are
// optional with sensible defaults — the zero value represents the legacy
// "POST with X-Orbit-Signature header" behaviour, so old connectors keep
// working without any config rewrites.
//
// Presets set these fields at creation time from the frontend (see
// web/src/api/saturn/presets/integrations.ts) so each MST integration only
// needs to fill them once.
type ConnectorConfig struct {
	// PresetID identifies which frontend preset was used: "saturn_deploy",
	// "insightflow", "keitaro", "asa_analytics", "generic". Informational.
	PresetID string `json:"preset_id,omitempty"`

	// HttpMethod the provider uses. "POST" (default) or "GET" (Keitaro).
	HttpMethod string `json:"http_method,omitempty"`

	// SignatureLocation tells the webhook handler where to read the HMAC from.
	// "header" (default) or "query". Keitaro uses "query".
	SignatureLocation string `json:"signature_location,omitempty"`

	// SignatureParamName overrides the default header/query param name.
	// When SignatureLocation="header": defaults to "X-Orbit-Signature".
	// When SignatureLocation="query":  defaults to "sign".
	SignatureParamName string `json:"signature_param_name,omitempty"`

	// TimestampParamName overrides the default timestamp header/query param.
	// When SignatureLocation="header": defaults to "X-Orbit-Timestamp".
	// When SignatureLocation="query":  defaults to "ts".
	TimestampParamName string `json:"timestamp_param_name,omitempty"`
}

// Parsed returns a ConnectorConfig with defaults applied. Never returns an
// error — an empty/invalid JSONB silently yields the default config, because
// the handler should never refuse a webhook purely because the config blob is
// malformed (that's admin UI's job to prevent).
func (c *Connector) Parsed() ConnectorConfig {
	cfg := ConnectorConfig{}
	if len(c.Config) > 0 {
		_ = json.Unmarshal([]byte(c.Config), &cfg)
	}
	cfg.applyDefaults()
	return cfg
}

func (c *ConnectorConfig) applyDefaults() {
	if c.HttpMethod == "" {
		c.HttpMethod = "POST"
	}
	if c.SignatureLocation == "" {
		c.SignatureLocation = "header"
	}
	if c.SignatureParamName == "" {
		if c.SignatureLocation == "query" {
			c.SignatureParamName = "sign"
		} else {
			c.SignatureParamName = "X-Orbit-Signature"
		}
	}
	if c.TimestampParamName == "" {
		if c.SignatureLocation == "query" {
			c.TimestampParamName = "ts"
		} else {
			c.TimestampParamName = "X-Orbit-Timestamp"
		}
	}
}
