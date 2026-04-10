package handler

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/integrations/internal/model"
	"github.com/mst-corp/orbit/services/integrations/internal/service"
)

func newIntegrationHandlerTestApp(
	t *testing.T,
	connectorStore *mockConnectorStore,
	routeStore *mockRouteStore,
	deliveryStore *mockDeliveryStore,
) *fiber.App {
	t.Helper()

	if connectorStore == nil {
		connectorStore = &mockConnectorStore{}
	}
	if routeStore == nil {
		routeStore = &mockRouteStore{}
	}
	if deliveryStore == nil {
		deliveryStore = &mockDeliveryStore{}
	}

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
	})

	svc := service.NewIntegrationService(connectorStore, routeStore, deliveryStore, nil, slog.Default())
	h := NewConnectorHandler(svc, slog.Default()).WithRedis(rdb)

	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	h.Register(app)
	h.RegisterPublic(app)
	return app
}

func TestCreateConnector_Success(t *testing.T) {
	userID := uuid.New()
	now := time.Now().UTC()

	var created *string
	connectorStore := &mockConnectorStore{
		createFn: func(ctx context.Context, c *model.Connector) error {
			created = &c.Name
			c.ID = uuid.New()
			c.CreatedAt = now
			c.UpdatedAt = now
			return nil
		},
	}

	app := newIntegrationHandlerTestApp(t, connectorStore, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/connectors", map[string]any{
		"name":         "crm-hook",
		"display_name": "CRM Hook",
		"type":         "inbound_webhook",
	}, map[string]string{
		"X-User-ID":   userID.String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
	if created == nil || *created != "crm-hook" {
		t.Fatalf("expected connector create to be called, got %#v", created)
	}

	var body map[string]any
	decodeIntegrationJSON(t, resp.Body, &body)
	if _, ok := body["connector"].(map[string]any); !ok {
		t.Fatalf("expected connector object, got %#v", body["connector"])
	}
	if secret, ok := body["secret"].(string); !ok || strings.TrimSpace(secret) == "" {
		t.Fatalf("expected non-empty secret, got %#v", body["secret"])
	}
}

func TestCreateConnector_Unauthorized(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/connectors", map[string]any{
		"name":         "crm-hook",
		"display_name": "CRM Hook",
		"type":         "inbound_webhook",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestCreateConnector_ValidationError(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/connectors", map[string]any{
		"name":         "bad name",
		"display_name": "CRM Hook",
		"type":         "inbound_webhook",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestReceiveWebhook_Success(t *testing.T) {
	connectorID := uuid.New()
	secretHash := "stored-digest-for-test"
	payload := []byte(`{"event":"deal.updated","external_event_id":"evt-1"}`)

	connectorStore := &mockConnectorStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
			if id != connectorID {
				t.Fatalf("unexpected connector id: %s", id)
			}
			return &model.Connector{
				ID:        connectorID,
				Name:      "crm-hook",
				IsActive:  true,
				CreatedBy: uuid.New(),
			}, nil
		},
		getSecretHashFn: func(ctx context.Context, id uuid.UUID) (string, error) {
			return secretHash, nil
		},
	}
	routeStore := &mockRouteStore{
		findMatchingRoutesFn: func(ctx context.Context, connectorID uuid.UUID, eventType string) ([]model.Route, error) {
			return []model.Route{}, nil
		},
	}
	deliveryStore := &mockDeliveryStore{
		findByExternalIDFn: func(ctx context.Context, connectorID uuid.UUID, externalEventID string) (*model.Delivery, error) {
			return nil, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, connectorStore, routeStore, deliveryStore)
	resp := doIntegrationRawRequest(t, app, http.MethodPost, "/webhooks/in/"+connectorID.String(), payload, map[string]string{
		"Content-Type":       "application/json",
		"X-Orbit-Signature":  signIntegrationPayload(secretHash, payload),
		"X-Orbit-Timestamp":  time.Now().UTC().Format(time.RFC3339),
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestReceiveWebhook_InvalidSignature(t *testing.T) {
	connectorID := uuid.New()
	payload := []byte(`{"event":"deal.updated"}`)

	connectorStore := &mockConnectorStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
			return &model.Connector{
				ID:        connectorID,
				Name:      "crm-hook",
				IsActive:  true,
				CreatedBy: uuid.New(),
			}, nil
		},
		getSecretHashFn: func(ctx context.Context, id uuid.UUID) (string, error) {
			return "stored-digest-for-test", nil
		},
	}

	app := newIntegrationHandlerTestApp(t, connectorStore, &mockRouteStore{}, &mockDeliveryStore{})
	resp := doIntegrationRawRequest(t, app, http.MethodPost, "/webhooks/in/"+connectorID.String(), payload, map[string]string{
		"Content-Type":       "application/json",
		"X-Orbit-Signature":  "deadbeef",
		"X-Orbit-Timestamp":  time.Now().UTC().Format(time.RFC3339),
	})

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestReceiveWebhook_ConnectorNotFound(t *testing.T) {
	connectorID := uuid.New()

	connectorStore := &mockConnectorStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
			return nil, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, connectorStore, nil, nil)
	resp := doIntegrationRawRequest(t, app, http.MethodPost, "/webhooks/in/"+connectorID.String(), []byte(`{"event":"deal.updated"}`), map[string]string{
		"Content-Type": "application/json",
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestListDeliveries_Success(t *testing.T) {
	connectorID := uuid.New()
	deliveries := []model.Delivery{
		{ID: uuid.New(), ConnectorID: connectorID, EventType: "deal.updated", Status: "delivered", AttemptCount: 1, CreatedAt: time.Now().UTC()},
	}

	deliveryStore := &mockDeliveryStore{
		listByConnectorFilteredFn: func(ctx context.Context, id uuid.UUID, status *string, limit, offset int) ([]model.Delivery, int, error) {
			if id != connectorID {
				t.Fatalf("unexpected connector id: %s", id)
			}
			return deliveries, len(deliveries), nil
		},
	}

	app := newIntegrationHandlerTestApp(t, nil, nil, deliveryStore)
	resp := doIntegrationRequest(t, app, http.MethodGet, "/integrations/connectors/"+connectorID.String()+"/deliveries?status=delivered", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "compliance",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	decodeIntegrationJSON(t, resp.Body, &body)
	if len(body.Data) != 1 || body.Total != 1 {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func doIntegrationRequest(t *testing.T, app *fiber.App, method, path string, body any, headers map[string]string) *http.Response {
	t.Helper()

	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func doIntegrationRawRequest(t *testing.T, app *fiber.App, method, path string, body []byte, headers map[string]string) *http.Response {
	t.Helper()

	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func decodeIntegrationJSON(t *testing.T, body io.Reader, target any) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(target); err != nil {
		t.Fatalf("decode json: %v", err)
	}
}

func signIntegrationPayload(secretHash string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secretHash))
	mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}
