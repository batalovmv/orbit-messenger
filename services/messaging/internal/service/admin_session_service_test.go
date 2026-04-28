// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
)

// recordingPublisher captures NATS publishes for assertions. Implements the
// Publisher interface; nil-safe — tests only check the recorded slice.
type recordingPublisher struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	Subject  string
	Event    string
	Data     interface{}
	SenderID string
}

func (p *recordingPublisher) Publish(subject, event string, data interface{}, _ []string, senderID ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sid := ""
	if len(senderID) > 0 {
		sid = senderID[0]
	}
	p.events = append(p.events, recordedEvent{
		Subject: subject, Event: event, Data: data, SenderID: sid,
	})
}

func (p *recordingPublisher) snapshot() []recordedEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]recordedEvent, len(p.events))
	copy(out, p.events)
	return out
}

func newSessionAdminSvc(
	users *mockUserStore, audit *mockAuditStore, pub Publisher,
	httpFn func(*http.Request) (*http.Response, error),
) *SessionAdminService {
	return NewSessionAdminService(SessionAdminConfig{
		Users:          users,
		Audit:          audit,
		NATS:           pub,
		AuthURL:        "http://auth",
		InternalSecret: "secret",
		HTTPClient:     &mockHTTPClient{doFn: httpFn},
		Timeout:        2 * time.Second,
	})
}

// fakeAuthSession is the JSON shape returned by auth /internal endpoints.
type fakeAuthSession struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	IPAddress *string   `json:"ip_address,omitempty"`
	UserAgent *string   `json:"user_agent,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestSessionAdmin_List_RequiresSysManageUsers(t *testing.T) {
	svc := newSessionAdminSvc(&mockUserStore{}, &mockAuditStore{}, nil,
		func(*http.Request) (*http.Response, error) {
			t.Fatal("auth must not be called when perm check fails")
			return nil, nil
		})
	_, err := svc.ListUserSessions(context.Background(), ListUserSessionsParams{
		ActorID:      uuid.New(),
		ActorRole:    "member",
		TargetUserID: uuid.NewString(),
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("expected 403, got %v", err)
	}
}

// TestSessionAdmin_List_BlocksHigherRoleTarget asserts CanModifyUser hierarchy
// on the LIST path — admin trying to enumerate a superadmin's sessions gets
// 403 BEFORE auth is queried. Without this, an admin could read session
// metadata (IPs, user-agents) of higher-role users without being able to
// act on those sessions.
func TestSessionAdmin_List_BlocksHigherRoleTarget(t *testing.T) {
	adminID := uuid.New()
	superID := uuid.New()
	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			if id == superID {
				return &model.User{ID: superID, Role: "superadmin", IsActive: true}, nil
			}
			return nil, nil
		},
	}
	svc := newSessionAdminSvc(users, &mockAuditStore{}, nil,
		func(*http.Request) (*http.Response, error) {
			t.Fatal("auth must not be called when role hierarchy guard rejects")
			return nil, nil
		})

	_, err := svc.ListUserSessions(context.Background(), ListUserSessionsParams{
		ActorID:      adminID,
		ActorRole:    "admin",
		TargetUserID: superID.String(),
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("expected 403 for listing superadmin's sessions, got %v", err)
	}
}

// TestSessionAdmin_List_AllowsSelfInspection: an admin may always view their
// own sessions even though same-role CanModifyUser would normally fail. The
// self-bypass is the natural read path for "what's signed in as me right now".
func TestSessionAdmin_List_AllowsSelfInspection(t *testing.T) {
	adminID := uuid.New()
	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return &model.User{ID: id, Role: "admin", IsActive: true}, nil
		},
	}
	httpFn := func(*http.Request) (*http.Response, error) {
		return newJSONResp(200, `{"sessions":[]}`), nil
	}
	svc := newSessionAdminSvc(users, &mockAuditStore{}, nil, httpFn)

	_, err := svc.ListUserSessions(context.Background(), ListUserSessionsParams{
		ActorID:      adminID,
		ActorRole:    "admin",
		TargetUserID: adminID.String(),
	})
	if err != nil {
		t.Fatalf("self-inspection must succeed, got %v", err)
	}
}

func TestSessionAdmin_List_TargetNotFound(t *testing.T) {
	svc := newSessionAdminSvc(
		&mockUserStore{getByIDFn: func(_ context.Context, _ uuid.UUID) (*model.User, error) { return nil, nil }},
		&mockAuditStore{}, nil,
		func(*http.Request) (*http.Response, error) {
			t.Fatal("auth must not be called when target user not found")
			return nil, nil
		})
	_, err := svc.ListUserSessions(context.Background(), ListUserSessionsParams{
		ActorID:      uuid.New(),
		ActorRole:    "admin",
		TargetUserID: uuid.NewString(),
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
}

func TestSessionAdmin_List_SetsIsCurrentOnActorJTI(t *testing.T) {
	actorID := uuid.New()
	actorJTI := uuid.NewString()
	otherJTI := uuid.NewString()

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return &model.User{ID: id, Role: "member", IsActive: true}, nil
		},
	}

	httpFn := func(req *http.Request) (*http.Response, error) {
		// Admin is listing their own sessions. The actor's jti corresponds to
		// one of the rows.
		if !strings.Contains(req.URL.Path, fmt.Sprintf("/internal/users/%s/sessions", actorID)) {
			t.Fatalf("unexpected URL: %s", req.URL.Path)
		}
		body := fmt.Sprintf(`{"sessions":[%s,%s]}`,
			mustJSON(t, fakeAuthSession{ID: actorJTI, UserID: actorID.String(), CreatedAt: time.Now()}),
			mustJSON(t, fakeAuthSession{ID: otherJTI, UserID: actorID.String(), CreatedAt: time.Now()}),
		)
		return newJSONResp(200, body), nil
	}

	svc := newSessionAdminSvc(users, &mockAuditStore{}, nil, httpFn)
	out, err := svc.ListUserSessions(context.Background(), ListUserSessionsParams{
		ActorID:        actorID,
		ActorRole:      "admin",
		ActorSessionID: actorJTI,
		TargetUserID:   actorID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(out))
	}
	var foundCurrent, foundOther int
	for _, s := range out {
		if s.ID == actorJTI {
			if !s.IsCurrent {
				t.Fatalf("session %s must have is_current=true", s.ID)
			}
			foundCurrent++
		}
		if s.ID == otherJTI {
			if s.IsCurrent {
				t.Fatalf("session %s must have is_current=false", s.ID)
			}
			foundOther++
		}
	}
	if foundCurrent != 1 || foundOther != 1 {
		t.Fatalf("expected one current and one other; got current=%d other=%d", foundCurrent, foundOther)
	}
}

// TestSessionAdmin_List_DropsTokenHash guards against accidentally leaking
// session token_hash to the admin UI. Even if auth returns it (it does), the
// AdminSession DTO must not carry it.
func TestSessionAdmin_List_DropsTokenHash(t *testing.T) {
	actorID := uuid.New()
	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return &model.User{ID: id, Role: "member", IsActive: true}, nil
		},
	}
	httpFn := func(req *http.Request) (*http.Response, error) {
		// Auth response includes token_hash — verify our DTO drops it.
		body := `{"sessions":[{"id":"00000000-0000-0000-0000-000000000001","user_id":"` + actorID.String() + `","token_hash":"SHOULD_NOT_LEAK","created_at":"2026-04-28T00:00:00Z","expires_at":"2026-05-28T00:00:00Z"}]}`
		return newJSONResp(200, body), nil
	}
	svc := newSessionAdminSvc(users, &mockAuditStore{}, nil, httpFn)
	out, err := svc.ListUserSessions(context.Background(), ListUserSessionsParams{
		ActorID:      actorID,
		ActorRole:    "admin",
		TargetUserID: actorID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 session, got %d", len(out))
	}
	encoded, _ := json.Marshal(out[0])
	if strings.Contains(string(encoded), "SHOULD_NOT_LEAK") || strings.Contains(string(encoded), "token_hash") {
		t.Fatalf("AdminSession DTO leaked token_hash: %s", encoded)
	}
}

func TestSessionAdmin_Revoke_RequiresSysManageUsers(t *testing.T) {
	svc := newSessionAdminSvc(&mockUserStore{}, &mockAuditStore{}, nil,
		func(*http.Request) (*http.Response, error) {
			t.Fatal("auth must not be called when perm check fails")
			return nil, nil
		})
	err := svc.RevokeSession(context.Background(), RevokeSessionParams{
		ActorID:   uuid.New(),
		ActorRole: "member",
		SessionID: uuid.NewString(),
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("expected 403, got %v", err)
	}
}

// TestSessionAdmin_Revoke_BlocksOwnCurrentSession is the lockout guard: the
// admin's own JWT (jti == ActorSessionID) cannot be revoked from this UI.
// They must use logout. Verify-by-revert: removing the guard makes the call
// proceed to audit + delete and the test fails.
func TestSessionAdmin_Revoke_BlocksOwnCurrentSession(t *testing.T) {
	actorID := uuid.New()
	jti := uuid.NewString()

	httpFn := func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Fatalf("auth should only be GET'd to fetch the session before the guard fires; got %s", req.Method)
		}
		body := mustJSON(t, fakeAuthSession{ID: jti, UserID: actorID.String(), CreatedAt: time.Now()})
		return newJSONResp(200, body), nil
	}

	auditCalled := false
	audit := &mockAuditStore{logFn: func(_ context.Context, _ *model.AuditEntry) error {
		auditCalled = true
		return nil
	}}
	svc := newSessionAdminSvc(&mockUserStore{}, audit, &recordingPublisher{}, httpFn)

	err := svc.RevokeSession(context.Background(), RevokeSessionParams{
		ActorID:        actorID,
		ActorRole:      "superadmin",
		ActorSessionID: jti,
		SessionID:      jti,
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("expected 400 for own-current revoke, got %v", err)
	}
	if auditCalled {
		t.Fatal("own-current guard fires BEFORE audit — audit must not be called")
	}
}

// TestSessionAdmin_Revoke_BlocksHigherRoleTarget asserts CanModifyUser hierarchy:
// an admin cannot revoke a superadmin's session (would let admins kick the
// person above them out of the system).
func TestSessionAdmin_Revoke_BlocksHigherRoleTarget(t *testing.T) {
	adminID := uuid.New()
	superID := uuid.New()
	jti := uuid.NewString()

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			if id == superID {
				return &model.User{ID: superID, Role: "superadmin", IsActive: true}, nil
			}
			return nil, nil
		},
	}
	httpFn := func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodDelete {
			t.Fatal("DELETE must not run when role hierarchy guard rejects")
		}
		body := mustJSON(t, fakeAuthSession{ID: jti, UserID: superID.String(), CreatedAt: time.Now()})
		return newJSONResp(200, body), nil
	}

	auditCalled := false
	audit := &mockAuditStore{logFn: func(_ context.Context, _ *model.AuditEntry) error {
		auditCalled = true
		return nil
	}}
	svc := newSessionAdminSvc(users, audit, &recordingPublisher{}, httpFn)

	err := svc.RevokeSession(context.Background(), RevokeSessionParams{
		ActorID:        adminID,
		ActorRole:      "admin",
		ActorSessionID: uuid.NewString(),
		SessionID:      jti,
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Fatalf("expected 403 for revoking superadmin's session, got %v", err)
	}
	if auditCalled {
		t.Fatal("role hierarchy guard fires BEFORE audit")
	}
}

// TestSessionAdmin_Revoke_HappyPath asserts the full pipeline: GET session →
// audit FIRST → DELETE session → NATS publish. Order matters; we track call
// order so a regression that swaps audit-after-delete is caught.
func TestSessionAdmin_Revoke_HappyPath(t *testing.T) {
	adminID := uuid.New()
	targetID := uuid.New()
	sessionID := uuid.NewString()

	order := make([]string, 0, 3)
	var orderMu sync.Mutex
	addOrder := func(s string) {
		orderMu.Lock()
		defer orderMu.Unlock()
		order = append(order, s)
	}

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return &model.User{ID: id, Role: "member", IsActive: true}, nil
		},
	}

	httpFn := func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			addOrder("auth_get")
			body := mustJSON(t, fakeAuthSession{ID: sessionID, UserID: targetID.String(), CreatedAt: time.Now()})
			return newJSONResp(200, body), nil
		}
		if req.Method == http.MethodDelete {
			addOrder("auth_delete")
			return newJSONResp(200, `{"status":"revoked"}`), nil
		}
		t.Fatalf("unexpected method %s", req.Method)
		return nil, nil
	}

	audit := &mockAuditStore{logFn: func(_ context.Context, entry *model.AuditEntry) error {
		addOrder("audit")
		if entry.Action != model.AuditUserSessionRevoke {
			t.Fatalf("expected action %q, got %q", model.AuditUserSessionRevoke, entry.Action)
		}
		if entry.TargetType != "user" {
			t.Fatalf("expected target_type=user, got %q", entry.TargetType)
		}
		if entry.TargetID == nil || *entry.TargetID != targetID.String() {
			t.Fatalf("expected target_id=%s, got %v", targetID, entry.TargetID)
		}
		// details must include session_id
		var d map[string]interface{}
		if err := json.Unmarshal(entry.Details, &d); err != nil {
			t.Fatalf("unmarshal details: %v", err)
		}
		if d["session_id"] != sessionID {
			t.Fatalf("expected session_id in details, got %v", d["session_id"])
		}
		return nil
	}}

	pub := &recordingPublisher{}
	svc := newSessionAdminSvc(users, audit, pub, httpFn)

	err := svc.RevokeSession(context.Background(), RevokeSessionParams{
		ActorID:        adminID,
		ActorRole:      "superadmin",
		ActorSessionID: uuid.NewString(),
		SessionID:      sessionID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"auth_get", "audit", "auth_delete"}
	if !equalSlices(order, want) {
		t.Fatalf("order: want %v, got %v", want, order)
	}

	events := pub.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 NATS publish, got %d", len(events))
	}
	wantSubject := fmt.Sprintf("orbit.session.%s.revoked", sessionID)
	if events[0].Subject != wantSubject {
		t.Fatalf("expected subject %q, got %q", wantSubject, events[0].Subject)
	}
	if events[0].Event != "session_revoked" {
		t.Fatalf("expected event session_revoked, got %q", events[0].Event)
	}
}

// TestSessionAdmin_Revoke_AuditFailureBlocksMutation is the fail-closed gate:
// if audit_log write fails, the DELETE must not run. Verify-by-revert: moving
// audit AFTER delete makes this test fail because the delete happens
// regardless of the audit error.
func TestSessionAdmin_Revoke_AuditFailureBlocksMutation(t *testing.T) {
	adminID := uuid.New()
	targetID := uuid.New()
	sessionID := uuid.NewString()

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			return &model.User{ID: id, Role: "member", IsActive: true}, nil
		},
	}

	deleteCalled := false
	httpFn := func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet {
			body := mustJSON(t, fakeAuthSession{ID: sessionID, UserID: targetID.String(), CreatedAt: time.Now()})
			return newJSONResp(200, body), nil
		}
		if req.Method == http.MethodDelete {
			deleteCalled = true
			t.Fatal("DELETE must not run when audit write fails")
		}
		return nil, nil
	}

	audit := &mockAuditStore{logFn: func(_ context.Context, _ *model.AuditEntry) error {
		return errors.New("redis down or db slow")
	}}
	pub := &recordingPublisher{}
	svc := newSessionAdminSvc(users, audit, pub, httpFn)

	err := svc.RevokeSession(context.Background(), RevokeSessionParams{
		ActorID:        adminID,
		ActorRole:      "superadmin",
		ActorSessionID: uuid.NewString(),
		SessionID:      sessionID,
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 500 {
		t.Fatalf("expected 500 on audit failure, got %v", err)
	}
	if deleteCalled {
		t.Fatal("DELETE ran despite audit failure — fail-closed broken")
	}
	if len(pub.snapshot()) != 0 {
		t.Fatal("NATS published despite audit failure")
	}
}

// TestSessionAdmin_Revoke_AuthNotFound returns 404 to the caller and never
// touches audit/NATS. Stale session id from a re-rendered list, etc.
func TestSessionAdmin_Revoke_AuthNotFound(t *testing.T) {
	adminID := uuid.New()
	httpFn := func(req *http.Request) (*http.Response, error) {
		return newJSONResp(404, `{"error":"Session not found"}`), nil
	}
	auditCalled := false
	audit := &mockAuditStore{logFn: func(_ context.Context, _ *model.AuditEntry) error {
		auditCalled = true
		return nil
	}}
	pub := &recordingPublisher{}
	svc := newSessionAdminSvc(&mockUserStore{}, audit, pub, httpFn)

	err := svc.RevokeSession(context.Background(), RevokeSessionParams{
		ActorID:        adminID,
		ActorRole:      "superadmin",
		ActorSessionID: uuid.NewString(),
		SessionID:      uuid.NewString(),
	})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("expected 404, got %v", err)
	}
	if auditCalled {
		t.Fatal("audit must not be written for not-found session")
	}
	if len(pub.snapshot()) != 0 {
		t.Fatal("NATS must not publish for not-found session")
	}
}

// helper functions ----------------------------------------------------------

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// jsonReader is unused but keeps `io` import live if other helpers are added.
var _ io.Reader = strings.NewReader("")
