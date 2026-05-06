// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/auth/internal/model"
)

// ---------------------------------------------------------------------------
// Minimal fakes
// ---------------------------------------------------------------------------

type fakeUserStore struct {
	createFn func(ctx context.Context, u *model.User) error
}

func (f *fakeUserStore) Create(ctx context.Context, u *model.User) error {
	if f.createFn != nil {
		return f.createFn(ctx, u)
	}
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}
func (f *fakeUserStore) CreateIfNoAdmins(ctx context.Context, u *model.User) error { return nil }
func (f *fakeUserStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	return nil, nil
}
func (f *fakeUserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	return nil, nil
}
func (f *fakeUserStore) GetNotificationPriorityMode(ctx context.Context, userID uuid.UUID) (string, error) {
	return "smart", nil
}
func (f *fakeUserStore) Update(ctx context.Context, u *model.User) error               { return nil }
func (f *fakeUserStore) CountAdmins(ctx context.Context) (int, error)                  { return 0, nil }
func (f *fakeUserStore) UpdatePassword(ctx context.Context, id uuid.UUID, h string) error {
	return nil
}
func (f *fakeUserStore) UpdateTOTP(ctx context.Context, id uuid.UUID, s *string, en bool) error {
	return nil
}
func (f *fakeUserStore) EnableTOTPAndRevokeSessions(ctx context.Context, id uuid.UUID, s string) error {
	return nil
}
func (f *fakeUserStore) UpdateNotificationPriorityMode(ctx context.Context, id uuid.UUID, m string) error {
	return nil
}
func (f *fakeUserStore) GetByOIDCSubject(ctx context.Context, provider, subject string) (*model.User, error) {
	return nil, nil
}
func (f *fakeUserStore) LinkOIDCSubject(ctx context.Context, userID uuid.UUID, provider, subject string) error {
	return nil
}
func (f *fakeUserStore) CreateOIDCUser(ctx context.Context, u *model.User, provider, subject string) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

type fakeInviteStore struct {
	getByCodeFn func(ctx context.Context, code string) (*model.Invite, error)
}

func (f *fakeInviteStore) Create(ctx context.Context, inv *model.Invite) error { return nil }
func (f *fakeInviteStore) GetByCode(ctx context.Context, code string) (*model.Invite, error) {
	if f.getByCodeFn != nil {
		return f.getByCodeFn(ctx, code)
	}
	return nil, nil
}
func (f *fakeInviteStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Invite, error) {
	return nil, nil
}
func (f *fakeInviteStore) ListAll(ctx context.Context) ([]model.Invite, error) { return nil, nil }
func (f *fakeInviteStore) UseInvite(ctx context.Context, code string, userID uuid.UUID, email string) (string, error) {
	return "member", nil
}
func (f *fakeInviteStore) RollbackUsage(ctx context.Context, code string) error              { return nil }
func (f *fakeInviteStore) Revoke(ctx context.Context, id, createdBy uuid.UUID) error          { return nil }
func (f *fakeInviteStore) UpdateUsedBy(ctx context.Context, code string, userID uuid.UUID) error {
	return nil
}

type fakeSessionStore struct{}

func (f *fakeSessionStore) Create(ctx context.Context, s *model.Session) error { return nil }
func (f *fakeSessionStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) GetByTokenHash(ctx context.Context, h string) (*model.Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) DeleteByID(ctx context.Context, id, userID uuid.UUID) error  { return nil }
func (f *fakeSessionStore) DeleteByTokenHash(ctx context.Context, h string) error       { return nil }
func (f *fakeSessionStore) DeleteAndReturnByTokenHash(ctx context.Context, h string) (*model.Session, error) {
	return nil, nil
}
func (f *fakeSessionStore) DeleteAllByUser(ctx context.Context, userID uuid.UUID) error { return nil }
func (f *fakeSessionStore) ListByUser(ctx context.Context, userID uuid.UUID) ([]model.Session, error) {
	return nil, nil
}

// recordingHTTPClient captures every Do(req) for assertions and returns
// a configurable status / error.
type recordingHTTPClient struct {
	mu      sync.Mutex
	calls   []recordedCall
	respFn  func(call int) (*http.Response, error)
	timeout chan struct{} // when non-nil the first call blocks until closed
	count   int
}

type recordedCall struct {
	Method   string
	URL      string
	Header   http.Header
	GotToken string
}

func (c *recordingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	c.count++
	idx := c.count
	c.calls = append(c.calls, recordedCall{
		Method:   req.Method,
		URL:      req.URL.String(),
		Header:   req.Header.Clone(),
		GotToken: req.Header.Get("X-Internal-Token"),
	})
	c.mu.Unlock()
	if c.respFn != nil {
		return c.respFn(idx)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(nil))}, nil
}

func newRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func now() *time.Time {
	t := time.Now()
	return &t
}

func newTestService(t *testing.T, hc HTTPClient, msgURL, internalSecret string) (*AuthService, *fakeInviteStore, *fakeUserStore) {
	t.Helper()
	users := &fakeUserStore{}
	invites := &fakeInviteStore{
		getByCodeFn: func(_ context.Context, _ string) (*model.Invite, error) {
			return &model.Invite{
				ID:       uuid.New(),
				Code:     "OK-CODE",
				IsActive: true,
				MaxUses:  10,
				UseCount: 0,
				Role:     "member",
			}, nil
		},
	}
	cfg := &Config{
		JWTSecret:      "test-secret",
		AccessTTL:      15 * time.Minute,
		RefreshTTL:     720 * time.Hour,
		MessagingURL:   msgURL,
		InternalSecret: internalSecret,
	}
	svc := NewAuthService(users, &fakeSessionStore{}, invites, newRedis(t),
		cfg, slog.Default()).WithHTTPClient(hc)
	return svc, invites, users
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestRegister_CallsMessagingJoinDefaults_OnSuccess proves that a successful
// Register fans out to the messaging /internal/users/:id/join-default-chats
// endpoint with the expected URL, method, and X-Internal-Token header.
func TestRegister_CallsMessagingJoinDefaults_OnSuccess(t *testing.T) {
	hc := &recordingHTTPClient{}
	svc, _, _ := newTestService(t, hc, "http://messaging:8082", "shh")

	user, err := svc.Register(context.Background(), "OK-CODE", "user@orbit.local",
		"PasswordPasswordPassword!1", "Test User")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user == nil || user.ID == uuid.Nil {
		t.Fatal("Register returned nil user / zero ID")
	}

	if len(hc.calls) != 1 {
		t.Fatalf("expected exactly 1 messaging call, got %d", len(hc.calls))
	}
	call := hc.calls[0]
	if call.Method != http.MethodPost {
		t.Fatalf("expected POST, got %s", call.Method)
	}
	wantPath := "/internal/users/" + user.ID.String() + "/join-default-chats"
	if !strings.HasSuffix(call.URL, wantPath) {
		t.Fatalf("URL must end with %q, got %s", wantPath, call.URL)
	}
	if call.GotToken != "shh" {
		t.Fatalf("X-Internal-Token: want %q, got %q", "shh", call.GotToken)
	}
}

// TestRegister_TolerantOfMessagingFailure: on transport error the register
// must still succeed (best-effort side channel) and the client must observe
// exactly two attempts (initial + 1 retry).
func TestRegister_TolerantOfMessagingFailure(t *testing.T) {
	transportErr := errors.New("connection refused")
	hc := &recordingHTTPClient{
		respFn: func(_ int) (*http.Response, error) {
			return nil, transportErr
		},
	}
	svc, _, _ := newTestService(t, hc, "http://messaging:8082", "shh")

	user, err := svc.Register(context.Background(), "OK-CODE", "user2@orbit.local",
		"PasswordPasswordPassword!1", "Test User")
	if err != nil {
		t.Fatalf("Register must not fail when messaging is down: %v", err)
	}
	if user == nil {
		t.Fatal("Register returned nil user even though it should be tolerant")
	}
	if len(hc.calls) != 2 {
		t.Fatalf("expected 2 attempts (initial + retry), got %d", len(hc.calls))
	}
}

// TestRegister_NoMessagingCallWhenConfigDisabled — Welcome flow stays a
// no-op when MessagingURL or InternalSecret is empty (typical in unit tests
// or in deployments not yet on mig 069).
func TestRegister_NoMessagingCallWhenConfigDisabled(t *testing.T) {
	hc := &recordingHTTPClient{}
	svc, _, _ := newTestService(t, hc, "", "")

	if _, err := svc.Register(context.Background(), "OK-CODE", "user3@orbit.local",
		"PasswordPasswordPassword!1", "Test User"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if len(hc.calls) != 0 {
		t.Fatalf("expected 0 messaging calls (welcome flow disabled), got %d", len(hc.calls))
	}
}

// suppress unused imports under different go versions
var _ = io.NopCloser
var _ = bytes.NewReader
var _ = now
