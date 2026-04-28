// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

type mockHTTPClient struct {
	doFn func(*http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.doFn(req)
}

func newPushAdminSvc(users *mockUserStore, audit *mockAuditStore, httpFn func(*http.Request) (*http.Response, error)) *PushAdminService {
	return NewPushAdminService(PushAdminConfig{
		Users:          users,
		Audit:          audit,
		GatewayURL:     "http://gateway",
		InternalSecret: "secret",
		HTTPClient:     &mockHTTPClient{doFn: httpFn},
		Timeout:        2 * time.Second,
	})
}

func newJSONResp(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// TestPushAdmin_RequiresSysManageSettings rejects 'member' role with 403 even
// before any DB lookup happens — perm check is the first gate.
func TestPushAdmin_RequiresSysManageSettings(t *testing.T) {
	svc := newPushAdminSvc(&mockUserStore{}, &mockAuditStore{}, func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP client must not be called when perm check fails")
		return nil, nil
	})
	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "member",
		UserID:    uuid.NewString(),
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("expected 403, got %v", err)
	}
}

func TestPushAdmin_RejectsBothIdentifiers(t *testing.T) {
	svc := newPushAdminSvc(&mockUserStore{}, &mockAuditStore{}, nil)
	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		UserID:    uuid.NewString(),
		Email:     "a@b.com",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected 400 for both identifiers, got %v", err)
	}
}

func TestPushAdmin_RejectsNeitherIdentifier(t *testing.T) {
	svc := newPushAdminSvc(&mockUserStore{}, &mockAuditStore{}, nil)
	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected 400 for missing identifier, got %v", err)
	}
}

func TestPushAdmin_NotFound(t *testing.T) {
	svc := newPushAdminSvc(
		&mockUserStore{getByEmailFn: func(_ context.Context, _ string) (*model.User, error) {
			return nil, nil // store contract: "no row" returns (nil, nil)
		}},
		&mockAuditStore{},
		func(*http.Request) (*http.Response, error) {
			t.Fatal("gateway must not be called when user not found")
			return nil, nil
		})
	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		Email:     "missing@example.com",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}

// TestPushAdmin_DeactivatedUser blocks the test push for a deactivated user
// — the admin should reactivate first; sending pushes to a deactivated user
// reveals their device fleet to a possibly-stale push subscription set.
func TestPushAdmin_DeactivatedUser(t *testing.T) {
	target := &model.User{
		ID: uuid.New(), Role: "member", IsActive: false,
		Email: "deactivated@example.com",
	}
	svc := newPushAdminSvc(
		&mockUserStore{getByEmailFn: func(_ context.Context, _ string) (*model.User, error) {
			return target, nil
		}},
		&mockAuditStore{},
		func(*http.Request) (*http.Response, error) {
			t.Fatal("gateway must not be called for deactivated user")
			return nil, nil
		})

	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		Email:     "deactivated@example.com",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected 400 for deactivated user, got %v", err)
	}
}

// TestPushAdmin_AuditFailureBlocksDispatch — fail-closed: if audit_log write
// fails, we must NOT push. Otherwise an admin could test-push without trail.
func TestPushAdmin_AuditFailureBlocksDispatch(t *testing.T) {
	target := &model.User{
		ID: uuid.New(), IsActive: true, Email: "ok@example.com",
	}
	gatewayCalled := false
	svc := newPushAdminSvc(
		&mockUserStore{getByEmailFn: func(_ context.Context, _ string) (*model.User, error) {
			return target, nil
		}},
		&mockAuditStore{logFn: func(_ context.Context, _ *model.AuditEntry) error {
			return errors.New("audit pg dead")
		}},
		func(*http.Request) (*http.Response, error) {
			gatewayCalled = true
			return newJSONResp(200, `{}`), nil
		})

	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		Email:     "ok@example.com",
	})
	if err == nil {
		t.Fatal("expected error when audit fails")
	}
	if gatewayCalled {
		t.Fatal("gateway must not be called when audit fails")
	}
}

// TestPushAdmin_GatewayTimeoutMapsTo504 covers the deadline path. The
// upstream call should NOT bubble a generic 500 — admin UI distinguishes.
func TestPushAdmin_GatewayTimeoutMapsTo504(t *testing.T) {
	target := &model.User{ID: uuid.New(), IsActive: true, Email: "u@e.com"}
	svc := newPushAdminSvc(
		&mockUserStore{getByEmailFn: func(_ context.Context, _ string) (*model.User, error) {
			return target, nil
		}},
		&mockAuditStore{},
		func(*http.Request) (*http.Response, error) {
			return nil, context.DeadlineExceeded
		})

	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		Email:     "u@e.com",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 504 {
		t.Fatalf("expected 504, got %v", err)
	}
}

// TestPushAdmin_GatewayNon200MapsTo502 — generic gateway-side failure (5xx,
// 4xx, malformed) becomes 502. Body bytes are NOT echoed to caller.
func TestPushAdmin_GatewayNon200MapsTo502(t *testing.T) {
	target := &model.User{ID: uuid.New(), IsActive: true, Email: "u@e.com"}
	svc := newPushAdminSvc(
		&mockUserStore{getByEmailFn: func(_ context.Context, _ string) (*model.User, error) {
			return target, nil
		}},
		&mockAuditStore{},
		func(*http.Request) (*http.Response, error) {
			// Body intentionally contains an internal-only token —
			// the test will verify it does NOT leak through the error.
			return newJSONResp(500, `{"internal":"INTERNAL_TOKEN_LEAK_ME"}`), nil
		})

	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		Email:     "u@e.com",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 502 {
		t.Fatalf("expected 502, got %v", err)
	}
	if strings.Contains(err.Error(), "INTERNAL_TOKEN_LEAK_ME") {
		t.Fatalf("error must not echo gateway body: %q", err.Error())
	}
}

// TestPushAdmin_HappyPath covers the full audit-then-dispatch flow plus the
// outgoing wire format: messaging must POST {user_id, title, body} with the
// internal token header, and surface the gateway report back to the caller
// with email + display_name annotations.
func TestPushAdmin_HappyPath(t *testing.T) {
	targetID := uuid.New()
	target := &model.User{
		ID: targetID, IsActive: true,
		Email: "happy@example.com", DisplayName: "Happy User",
	}

	var auditEntry *model.AuditEntry
	auditCalledBeforeGateway := true
	gatewayHit := false

	audit := &mockAuditStore{logFn: func(_ context.Context, e *model.AuditEntry) error {
		auditEntry = e
		if gatewayHit {
			auditCalledBeforeGateway = false
		}
		return nil
	}}

	httpFn := func(req *http.Request) (*http.Response, error) {
		gatewayHit = true
		// Verify wire format: POST, JSON body, X-Internal-Token, correct path.
		if req.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", req.Method)
		}
		if req.URL.Path != "/internal/push/dispatch-test" {
			t.Errorf("unexpected path: %s", req.URL.Path)
		}
		if got := req.Header.Get("X-Internal-Token"); got != "secret" {
			t.Errorf("X-Internal-Token = %q, want %q", got, "secret")
		}
		var body map[string]string
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &body)
		if body["user_id"] != targetID.String() {
			t.Errorf("body user_id = %q, want %q", body["user_id"], targetID.String())
		}
		if body["title"] != "Test from admin" {
			t.Errorf("body title = %q", body["title"])
		}

		respBody := bytes.NewBufferString(`{"user_id":"` + targetID.String() + `","device_count":1,"sent":1,"failed":0,"stale":0,"devices":[{"device_id":"dev-1","endpoint_host":"fcm.googleapis.com","status":"ok"}]}`)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(respBody), Header: make(http.Header)}, nil
	}

	svc := newPushAdminSvc(
		&mockUserStore{getByEmailFn: func(_ context.Context, _ string) (*model.User, error) {
			return target, nil
		}}, audit, httpFn)

	report, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		Email:     "happy@example.com",
		Title:     "Test from admin",
		Body:      "test body",
	})
	if err != nil {
		t.Fatalf("SendTestPush: %v", err)
	}

	// Audit was written before gateway call.
	if !auditCalledBeforeGateway {
		t.Error("audit must be written BEFORE gateway dispatch (fail-closed)")
	}
	if auditEntry == nil || auditEntry.Action != model.AuditPushTestSent {
		t.Errorf("audit entry not recorded correctly: %+v", auditEntry)
	}

	// Report annotations.
	if report.UserID != targetID.String() {
		t.Errorf("report.UserID = %q, want %q", report.UserID, targetID.String())
	}
	if report.Email != "happy@example.com" {
		t.Errorf("report.Email = %q", report.Email)
	}
	if report.DisplayName != "Happy User" {
		t.Errorf("report.DisplayName = %q", report.DisplayName)
	}
	if report.Sent != 1 || report.DeviceCount != 1 {
		t.Errorf("counts: %+v", report)
	}
}

// TestPushAdmin_HappyByUUID covers the UUID-resolution branch of resolveTarget.
// The Email path is exercised by every other test; without this one the UUID
// branch (uuid.Parse + GetByID) is uncovered.
func TestPushAdmin_HappyByUUID(t *testing.T) {
	targetID := uuid.New()
	target := &model.User{ID: targetID, IsActive: true, Email: "uuid@example.com"}

	httpFn := func(req *http.Request) (*http.Response, error) {
		body := bytes.NewBufferString(`{"user_id":"` + targetID.String() + `","device_count":0,"sent":0,"failed":0,"stale":0,"devices":[]}`)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(body), Header: make(http.Header)}, nil
	}

	svc := newPushAdminSvc(
		&mockUserStore{getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			if id != targetID {
				t.Errorf("unexpected GetByID id=%s", id)
			}
			return target, nil
		}},
		&mockAuditStore{}, httpFn)

	report, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		UserID:    targetID.String(),
	})
	if err != nil {
		t.Fatalf("SendTestPush: %v", err)
	}
	if report.UserID != targetID.String() {
		t.Fatalf("report.UserID = %q, want %q", report.UserID, targetID.String())
	}
}

// TestPushAdmin_InvalidUUID returns 400 for a malformed user_id rather than
// falling through to the email branch or to a 404. Guards against parser
// regressions.
func TestPushAdmin_InvalidUUID(t *testing.T) {
	svc := newPushAdminSvc(&mockUserStore{}, &mockAuditStore{}, func(*http.Request) (*http.Response, error) {
		t.Fatal("must not reach gateway with invalid uuid")
		return nil, nil
	})
	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		UserID:    "not-a-uuid",
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected 400 for invalid uuid, got %v", err)
	}
}

// TestPushAdmin_TitleBodyCaps rejects oversize inputs at the service layer
// (defense in depth — gateway also caps).
func TestPushAdmin_TitleBodyCaps(t *testing.T) {
	svc := newPushAdminSvc(&mockUserStore{}, &mockAuditStore{}, nil)
	huge := strings.Repeat("a", pushTestMaxTitleLen+1)
	_, err := svc.SendTestPush(context.Background(), SendTestPushParams{
		ActorID:   uuid.New(),
		ActorRole: "admin",
		UserID:    uuid.NewString(),
		Title:     huge,
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected 400 for oversized title, got %v", err)
	}
}
