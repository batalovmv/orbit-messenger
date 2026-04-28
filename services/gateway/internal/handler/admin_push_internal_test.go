// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mst-corp/orbit/pkg/response"
	"github.com/mst-corp/orbit/services/gateway/internal/middleware"
	"github.com/mst-corp/orbit/services/gateway/internal/push"
)

type stubAdminDispatcher struct {
	enabled  bool
	report   *push.Report
	err      error
	gotUser  string
	delay    time.Duration
	gotBody  []byte
	stopFn   func()
}

func (s *stubAdminDispatcher) Enabled() bool { return s.enabled }
func (s *stubAdminDispatcher) SendToUserWithReport(ctx context.Context, userID string, payload []byte) (*push.Report, error) {
	s.gotUser = userID
	s.gotBody = payload
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	return s.report, nil
}

func newAdminPushApp(disp *stubAdminDispatcher, secret string, timeout time.Duration) *fiber.App {
	app := fiber.New(fiber.Config{ErrorHandler: response.FiberErrorHandler})
	g := app.Group("/internal", middleware.RequireInternalToken(secret))
	RegisterAdminPushInternalRoute(g, AdminPushInternalConfig{
		Dispatcher: disp,
		Timeout:    timeout,
	})
	return app
}

func TestAdminPushInternal_RequiresInternalToken(t *testing.T) {
	disp := &stubAdminDispatcher{enabled: true}
	app := newAdminPushApp(disp, "secret", 0)

	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test", strings.NewReader(`{"user_id":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
	if disp.gotUser != "" {
		t.Fatal("dispatcher must not be called without valid token")
	}
}

func TestAdminPushInternal_DispatcherDisabled(t *testing.T) {
	disp := &stubAdminDispatcher{enabled: false}
	app := newAdminPushApp(disp, "secret", 0)

	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test", strings.NewReader(`{"user_id":"u"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when dispatcher disabled, got %d", resp.StatusCode)
	}
}

func TestAdminPushInternal_BadJSONBody(t *testing.T) {
	disp := &stubAdminDispatcher{enabled: true}
	app := newAdminPushApp(disp, "secret", 0)

	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test", strings.NewReader(`{not json`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminPushInternal_RequiresUserID(t *testing.T) {
	disp := &stubAdminDispatcher{enabled: true}
	app := newAdminPushApp(disp, "secret", 0)

	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing user_id, got %d", resp.StatusCode)
	}
}

// TestAdminPushInternal_Happy verifies the gateway builds the SW push payload
// itself (messaging just supplies title/body) so the SW push schema stays in
// one place. Also verifies user_id flows through.
func TestAdminPushInternal_Happy(t *testing.T) {
	report := &push.Report{
		UserID: "11111111-1111-1111-1111-111111111111",
		Sent:   2, Failed: 0, Stale: 0, DeviceCount: 2,
	}
	disp := &stubAdminDispatcher{enabled: true, report: report}
	app := newAdminPushApp(disp, "secret", 0)

	body := `{"user_id":"` + report.UserID + `","title":"Heyo","body":"check"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if disp.gotUser != report.UserID {
		t.Fatalf("dispatcher got %q, want %q", disp.gotUser, report.UserID)
	}

	// Payload built by gateway must be valid JSON containing the title.
	var got map[string]interface{}
	if err := json.Unmarshal(disp.gotBody, &got); err != nil {
		t.Fatalf("gateway-built payload not JSON: %v", err)
	}
	if got["title"] != "Heyo" {
		t.Fatalf("expected title=Heyo, got %v", got["title"])
	}
}

// TestAdminPushInternal_Timeout maps gateway-side timeouts to 504 — admin UI
// shows "downstream unreachable" rather than a generic 500.
func TestAdminPushInternal_Timeout(t *testing.T) {
	disp := &stubAdminDispatcher{
		enabled: true,
		err:     context.DeadlineExceeded,
	}
	app := newAdminPushApp(disp, "secret", 50*time.Millisecond)

	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test",
		strings.NewReader(`{"user_id":"11111111-1111-1111-1111-111111111111"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", resp.StatusCode)
	}
}

// TestAdminPushInternal_DispatcherError maps generic dispatcher errors to 502.
func TestAdminPushInternal_DispatcherError(t *testing.T) {
	disp := &stubAdminDispatcher{
		enabled: true,
		err:     errors.New("messaging unreachable"),
	}
	app := newAdminPushApp(disp, "secret", 0)

	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test",
		strings.NewReader(`{"user_id":"11111111-1111-1111-1111-111111111111"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.StatusCode)
	}
}

// TestAdminPushInternal_PayloadCaps rejects oversized title/body to keep the
// SW notification UI sane and to bound audit_log details size upstream.
func TestAdminPushInternal_PayloadCaps(t *testing.T) {
	disp := &stubAdminDispatcher{enabled: true}
	app := newAdminPushApp(disp, "secret", 0)

	hugeTitle := strings.Repeat("a", adminPushMaxTitleLen+1)
	body := `{"user_id":"u","title":"` + hugeTitle + `"}`
	req := httptest.NewRequest(http.MethodPost, "/internal/push/dispatch-test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Token", "secret")
	resp, _ := app.Test(req, -1)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for oversized title, got %d", resp.StatusCode)
	}
}
