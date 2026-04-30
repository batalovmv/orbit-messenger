// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
)

// adminPushHTTPClient stubs the gateway round-trip so handler tests stay
// hermetic. The PushAdminService talks to gateway via http.Client; we inject
// our own to capture the request and return canned reports.
type adminPushHTTPClient struct {
	doFn func(*http.Request) (*http.Response, error)
}

func (c *adminPushHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.doFn(req)
}

func newAdminPushApp(users *mockAdminUserStore, audit *mockAdminAuditStore, gatewayFn func(*http.Request) (*http.Response, error)) *fiber.App {
	app := fiber.New()
	svc := service.NewPushAdminService(service.PushAdminConfig{
		Users:          users,
		Audit:          audit,
		GatewayURL:     "http://gateway",
		InternalSecret: "secret",
		HTTPClient:     &adminPushHTTPClient{doFn: gatewayFn},
	})
	h := NewAdminPushHandler(svc)
	h.Register(app)
	return app
}

func gatewayHappy(targetID string) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		body := bytes.NewBufferString(`{"user_id":"` + targetID + `","device_count":1,"sent":1,"failed":0,"stale":0,"devices":[{"device_id":"` + uuid.NewString() + `","endpoint_host":"fcm.googleapis.com","status":"ok"}]}`)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(body), Header: make(http.Header)}, nil
	}
}

func TestAdminPush_RequiresSuperadminRole(t *testing.T) {
	users := &mockAdminUserStore{}
	audit := &mockAdminAuditStore{}
	app := newAdminPushApp(users, audit, func(*http.Request) (*http.Response, error) {
		t.Fatal("must not call gateway when role check fails")
		return nil, nil
	})

	body := `{"user_id":"` + uuid.NewString() + `"}`
	req, _ := http.NewRequest(http.MethodPost, "/admin/push/test", strings.NewReader(body))
	req.Header.Set("X-User-ID", uuid.NewString())
	req.Header.Set("X-User-Role", "member") // not superadmin
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestAdminPush_HappyEndToEnd(t *testing.T) {
	targetID := uuid.New()
	target := &model.User{ID: targetID, IsActive: true, Email: "t@e.com"}

	users := &mockAdminUserStore{}
	users.getByEmailFn = func(_ context.Context, _ string) (*model.User, error) {
		return target, nil
	}

	auditEntries := 0
	audit := &mockAdminAuditStore{
		logFn: func(_ context.Context, e *model.AuditEntry) error {
			auditEntries++
			if e.Action != model.AuditPushTestSent {
				t.Errorf("expected audit action %q, got %q", model.AuditPushTestSent, e.Action)
			}
			return nil
		},
	}

	app := newAdminPushApp(users, audit, gatewayHappy(targetID.String()))

	reqBody := `{"email":"t@e.com","title":"hi","body":"yo"}`
	req, _ := http.NewRequest(http.MethodPost, "/admin/push/test", strings.NewReader(reqBody))
	req.Header.Set("X-User-ID", uuid.NewString())
	req.Header.Set("X-User-Role", "superadmin")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d body=%s", resp.StatusCode, raw)
	}

	var report service.PushTestReport
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if report.UserID != targetID.String() || report.Sent != 1 || report.Email != "t@e.com" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if auditEntries != 1 {
		t.Fatalf("expected exactly 1 audit row, got %d", auditEntries)
	}
}

// TestAdminPush_BadJSON guards the parser path — admin tooling should get a
// 400, not the JSON parser error bubble up.
func TestAdminPush_BadJSON(t *testing.T) {
	app := newAdminPushApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, nil)

	req, _ := http.NewRequest(http.MethodPost, "/admin/push/test", strings.NewReader(`{nope`))
	req.Header.Set("X-User-ID", uuid.NewString())
	req.Header.Set("X-User-Role", "admin")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
