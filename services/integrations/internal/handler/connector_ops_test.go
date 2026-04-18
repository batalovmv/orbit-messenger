package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/integrations/internal/model"
	"github.com/mst-corp/orbit/services/integrations/internal/store"
)

// ── PreviewTemplate ──────────────────────────────────────────────────────

func TestPreviewTemplate_RendersWithoutConnector(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/templates/preview", map[string]any{
		"template":   "Hello {{.data.name}} from {{.event}}",
		"event_type": "deal.updated",
		"sample_payload": map[string]any{
			"data": map[string]any{"name": "Alice"},
		},
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Rendered string `json:"rendered"`
	}
	decodeIntegrationJSON(t, resp.Body, &body)
	if body.Rendered != "Hello Alice from deal.updated" {
		t.Fatalf("unexpected render: %q", body.Rendered)
	}
}

func TestPreviewTemplate_ResolvesConnectorFields(t *testing.T) {
	connectorID := uuid.New()
	connectorStore := &mockConnectorStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
			if id != connectorID {
				t.Fatalf("unexpected connector id: %s", id)
			}
			return &model.Connector{
				ID:          connectorID,
				Name:        "crm-hook",
				DisplayName: "CRM Hook",
			}, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, connectorStore, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/templates/preview", map[string]any{
		"connector_id": connectorID.String(),
		"template":     "from {{.connector.display_name}}",
		"sample_payload": map[string]any{
			"data": map[string]any{"name": "Alice"},
		},
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body struct {
		Rendered string `json:"rendered"`
	}
	decodeIntegrationJSON(t, resp.Body, &body)
	if body.Rendered != "from CRM Hook" {
		t.Fatalf("unexpected render: %q", body.Rendered)
	}
}

func TestPreviewTemplate_EmptyTemplate_BadRequest(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/templates/preview", map[string]any{
		"template": "   ",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPreviewTemplate_InvalidConnectorID_BadRequest(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/templates/preview", map[string]any{
		"connector_id": "not-a-uuid",
		"template":     "hi",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestPreviewTemplate_MemberForbidden(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/templates/preview", map[string]any{
		"template": "hi",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ── TestConnector ────────────────────────────────────────────────────────

func TestTestConnector_NoMatchingRoutes_BadRequest(t *testing.T) {
	connectorID := uuid.New()
	connectorStore := &mockConnectorStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
			return &model.Connector{ID: connectorID, Name: "crm-hook", CreatedBy: uuid.New()}, nil
		},
	}
	routeStore := &mockRouteStore{
		findMatchingRoutesFn: func(ctx context.Context, id uuid.UUID, eventType string) ([]model.Route, error) {
			return nil, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, connectorStore, routeStore, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/connectors/"+connectorID.String()+"/test", map[string]any{
		"event_type": "deal.updated",
		"payload":    map[string]any{"foo": "bar"},
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestTestConnector_ConnectorNotFound(t *testing.T) {
	connectorID := uuid.New()
	connectorStore := &mockConnectorStore{
		getByIDFn: func(ctx context.Context, id uuid.UUID) (*model.Connector, error) {
			return nil, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, connectorStore, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/connectors/"+connectorID.String()+"/test", map[string]any{
		"event_type": "deal.updated",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestTestConnector_MemberForbidden(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodPost, "/integrations/connectors/"+uuid.New().String()+"/test", map[string]any{
		"event_type": "x",
	}, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

// ── GetConnectorStats ────────────────────────────────────────────────────

func TestGetConnectorStats_Success(t *testing.T) {
	connectorID := uuid.New()
	lastAt := time.Now().UTC().Add(-5 * time.Minute)

	var capturedWindow time.Duration
	deliveryStore := &mockDeliveryStore{
		connectorStatsFn: func(ctx context.Context, id uuid.UUID, window time.Duration) (*store.ConnectorStatsRow, error) {
			if id != connectorID {
				t.Fatalf("unexpected connector id: %s", id)
			}
			capturedWindow = window
			return &store.ConnectorStatsRow{
				Total:          10,
				Delivered:      7,
				Failed:         2,
				Pending:        1,
				DeadLetter:     0,
				LastDeliveryAt: &lastAt,
			}, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, nil, nil, deliveryStore)
	resp := doIntegrationRequest(t, app, http.MethodGet, "/integrations/connectors/"+connectorID.String()+"/stats?window=1h", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedWindow != time.Hour {
		t.Fatalf("expected 1h window passed to store, got %v", capturedWindow)
	}

	var body struct {
		Total      int    `json:"total"`
		Delivered  int    `json:"delivered"`
		Failed     int    `json:"failed"`
		DeadLetter int    `json:"dead_letter"`
		Window     string `json:"window"`
	}
	decodeIntegrationJSON(t, resp.Body, &body)
	if body.Total != 10 || body.Delivered != 7 || body.Failed != 2 {
		t.Fatalf("unexpected stats body: %+v", body)
	}
	if body.Window == "" {
		t.Fatalf("expected window label, got empty")
	}
}

func TestGetConnectorStats_DefaultWindowIs24h(t *testing.T) {
	connectorID := uuid.New()

	var capturedWindow time.Duration
	deliveryStore := &mockDeliveryStore{
		connectorStatsFn: func(ctx context.Context, id uuid.UUID, window time.Duration) (*store.ConnectorStatsRow, error) {
			capturedWindow = window
			return &store.ConnectorStatsRow{}, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, nil, nil, deliveryStore)
	resp := doIntegrationRequest(t, app, http.MethodGet, "/integrations/connectors/"+connectorID.String()+"/stats", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedWindow != 24*time.Hour {
		t.Fatalf("expected default window 24h, got %v", capturedWindow)
	}
}

func TestGetConnectorStats_InvalidWindow_BadRequest(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodGet, "/integrations/connectors/"+uuid.New().String()+"/stats?window=not-a-duration", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetConnectorStats_WindowTooLarge_BadRequest(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	// 31 days is above the 30-day limit.
	resp := doIntegrationRequest(t, app, http.MethodGet, "/integrations/connectors/"+uuid.New().String()+"/stats?window=744h", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "admin",
	})

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestGetConnectorStats_ComplianceAllowed(t *testing.T) {
	connectorID := uuid.New()
	deliveryStore := &mockDeliveryStore{
		connectorStatsFn: func(ctx context.Context, id uuid.UUID, window time.Duration) (*store.ConnectorStatsRow, error) {
			return &store.ConnectorStatsRow{}, nil
		},
	}

	app := newIntegrationHandlerTestApp(t, nil, nil, deliveryStore)
	resp := doIntegrationRequest(t, app, http.MethodGet, "/integrations/connectors/"+connectorID.String()+"/stats", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "compliance",
	})

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for compliance role (has SysViewBotLogs), got %d", resp.StatusCode)
	}
}

func TestGetConnectorStats_MemberForbidden(t *testing.T) {
	app := newIntegrationHandlerTestApp(t, nil, nil, nil)
	resp := doIntegrationRequest(t, app, http.MethodGet, "/integrations/connectors/"+uuid.New().String()+"/stats", nil, map[string]string{
		"X-User-ID":   uuid.New().String(),
		"X-User-Role": "member",
	})

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}
