// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/auth/internal/model"
	"github.com/mst-corp/orbit/services/auth/internal/store"
)

// ---------------------------------------------------------------------------
// Fake DirectoryClient
// ---------------------------------------------------------------------------

type fakeDirClient struct {
	subjects []string
	err      error
}

func (f *fakeDirClient) ListActiveSubjects(_ context.Context) ([]string, error) {
	return f.subjects, f.err
}

// ---------------------------------------------------------------------------
// Minimal in-memory stores for sync tests
// ---------------------------------------------------------------------------

// syncUserStore backs SyncOnce tests. Tracks deactivation calls.
type syncUserStore struct {
	oidcUsers   []store.OIDCActiveUser // returned by ListOIDCActiveUsers
	deactivated []uuid.UUID            // records every Deactivate call
}

func (s *syncUserStore) Create(_ context.Context, _ *model.User) error              { return nil }
func (s *syncUserStore) CreateIfNoAdmins(_ context.Context, _ *model.User) error    { return nil }
func (s *syncUserStore) GetByID(_ context.Context, _ uuid.UUID) (*model.User, error) { return nil, nil }
func (s *syncUserStore) GetByEmail(_ context.Context, _ string) (*model.User, error) { return nil, nil }
func (s *syncUserStore) GetNotificationPriorityMode(_ context.Context, _ uuid.UUID) (string, error) {
	return "smart", nil
}
func (s *syncUserStore) Update(_ context.Context, _ *model.User) error               { return nil }
func (s *syncUserStore) CountAdmins(_ context.Context) (int, error)                  { return 0, nil }
func (s *syncUserStore) UpdatePassword(_ context.Context, _ uuid.UUID, _ string) error { return nil }
func (s *syncUserStore) UpdateTOTP(_ context.Context, _ uuid.UUID, _ *string, _ bool) error {
	return nil
}
func (s *syncUserStore) EnableTOTPAndRevokeSessions(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *syncUserStore) UpdateNotificationPriorityMode(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *syncUserStore) GetByOIDCSubject(_ context.Context, _, _ string) (*model.User, error) {
	return nil, nil
}
func (s *syncUserStore) LinkOIDCSubject(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *syncUserStore) CreateOIDCUser(_ context.Context, u *model.User, _, _ string) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}
func (s *syncUserStore) Deactivate(_ context.Context, id uuid.UUID) error {
	s.deactivated = append(s.deactivated, id)
	return nil
}
func (s *syncUserStore) ListOIDCActiveUsers(_ context.Context, _ string) ([]store.OIDCActiveUser, error) {
	return s.oidcUsers, nil
}

// syncSessionStore backs SyncOnce tests. Tracks DeleteAllByUser calls.
type syncSessionStore struct {
	deletedUsers []uuid.UUID
}

func (s *syncSessionStore) Create(_ context.Context, sess *model.Session) error {
	if sess.ID == uuid.Nil {
		sess.ID = uuid.New()
	}
	return nil
}
func (s *syncSessionStore) GetByID(_ context.Context, _ uuid.UUID) (*model.Session, error) {
	return nil, nil
}
func (s *syncSessionStore) GetByTokenHash(_ context.Context, _ string) (*model.Session, error) {
	return nil, nil
}
func (s *syncSessionStore) ListByUser(_ context.Context, _ uuid.UUID) ([]model.Session, error) {
	return nil, nil
}
func (s *syncSessionStore) DeleteByID(_ context.Context, _, _ uuid.UUID) error  { return nil }
func (s *syncSessionStore) DeleteByTokenHash(_ context.Context, _ string) error { return nil }
func (s *syncSessionStore) DeleteAndReturnByTokenHash(_ context.Context, _ string) (*model.Session, error) {
	return nil, nil
}
func (s *syncSessionStore) DeleteAllByUser(_ context.Context, userID uuid.UUID) error {
	s.deletedUsers = append(s.deletedUsers, userID)
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newSyncWorker sets up a worker with in-memory stores and miniredis.
func newSyncWorker(t *testing.T, oidcUsers []store.OIDCActiveUser, dirSubjects []string, dirErr error) (
	*OIDCSyncWorker, *syncUserStore, *syncSessionStore, *redis.Client,
) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	users := &syncUserStore{oidcUsers: oidcUsers}
	sessions := &syncSessionStore{}
	invites := &fakeInviteStore{}

	svc := &AuthService{
		users:    users,
		sessions: sessions,
		invites:  invites,
		redis:    rdb,
		cfg: &Config{
			JWTSecret:  "test-jwt-secret-32-chars-minimum!!",
			AccessTTL:  15 * time.Minute,
			RefreshTTL: 24 * time.Hour,
		},
		logger: slog.Default(),
	}

	cfg := &OIDCSyncConfig{
		Enabled:     true,
		Interval:    time.Hour,
		ProviderKey: "google",
	}
	client := &fakeDirClient{subjects: dirSubjects, err: dirErr}
	worker := NewOIDCSyncWorker(cfg, svc, client, slog.Default())

	return worker, users, sessions, rdb
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestOIDCSync_DeactivatesUserMissingFromDirectory seeds two active OIDC
// users (subjects "a" and "b"). The directory returns only "a". Verifies that
// "b" is deactivated, its session deleted, and the per-user JWT blacklist key
// is set in Redis.
func TestOIDCSync_DeactivatesUserMissingFromDirectory(t *testing.T) {
	ctx := context.Background()

	idA := uuid.New()
	idB := uuid.New()

	oidcUsers := []store.OIDCActiveUser{
		{ID: idA, Subject: "a"},
		{ID: idB, Subject: "b"},
	}

	worker, users, sessions, rdb := newSyncWorker(t, oidcUsers, []string{"a"}, nil)

	n, err := worker.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("SyncOnce returned error: %v", err)
	}
	if n != 1 {
		t.Errorf("deactivated count: want 1, got %d", n)
	}

	// User B must be deactivated.
	if len(users.deactivated) != 1 || users.deactivated[0] != idB {
		t.Errorf("deactivated users: want [%s], got %v", idB, users.deactivated)
	}

	// Session for user B must be deleted.
	if len(sessions.deletedUsers) != 1 || sessions.deletedUsers[0] != idB {
		t.Errorf("deleted session users: want [%s], got %v", idB, sessions.deletedUsers)
	}

	// JWT blacklist key for user B must exist in Redis.
	blacklistKey := fmt.Sprintf("jwt_blacklist:user:%s", idB)
	val, redisErr := rdb.Get(ctx, blacklistKey).Result()
	if redisErr != nil {
		t.Fatalf("redis get blacklist key %q: %v", blacklistKey, redisErr)
	}
	if val != "1" {
		t.Errorf("blacklist key value: want '1', got %q", val)
	}

	// User A must NOT be affected.
	for _, id := range users.deactivated {
		if id == idA {
			t.Errorf("user A (still active in IdP) was incorrectly deactivated")
		}
	}
}

// TestOIDCSync_NoOpWhenAllPresent verifies that when all Orbit OIDC users are
// still in the directory, nothing is deactivated.
func TestOIDCSync_NoOpWhenAllPresent(t *testing.T) {
	ctx := context.Background()

	idA := uuid.New()
	idB := uuid.New()

	oidcUsers := []store.OIDCActiveUser{
		{ID: idA, Subject: "a"},
		{ID: idB, Subject: "b"},
	}

	worker, users, sessions, _ := newSyncWorker(t, oidcUsers, []string{"a", "b"}, nil)

	n, err := worker.SyncOnce(ctx)
	if err != nil {
		t.Fatalf("SyncOnce returned error: %v", err)
	}
	if n != 0 {
		t.Errorf("deactivated count: want 0, got %d", n)
	}
	if len(users.deactivated) != 0 {
		t.Errorf("no users should be deactivated, got %v", users.deactivated)
	}
	if len(sessions.deletedUsers) != 0 {
		t.Errorf("no sessions should be deleted, got %v", sessions.deletedUsers)
	}
}

// TestOIDCSync_DirectoryErrorIsConservative verifies that when the directory
// client returns an error, SyncOnce returns the error and deactivates nobody.
func TestOIDCSync_DirectoryErrorIsConservative(t *testing.T) {
	ctx := context.Background()

	idA := uuid.New()

	oidcUsers := []store.OIDCActiveUser{
		{ID: idA, Subject: "a"},
	}

	dirErr := errors.New("directory unavailable")
	worker, users, sessions, _ := newSyncWorker(t, oidcUsers, nil, dirErr)

	n, err := worker.SyncOnce(ctx)
	if err == nil {
		t.Fatal("expected error from SyncOnce, got nil")
	}
	if n != 0 {
		t.Errorf("deactivated count: want 0 on error, got %d", n)
	}
	if len(users.deactivated) != 0 {
		t.Errorf("conservative: no deactivations expected on dir error, got %v", users.deactivated)
	}
	if len(sessions.deletedUsers) != 0 {
		t.Errorf("conservative: no session deletes expected on dir error, got %v", sessions.deletedUsers)
	}
}

// TestOIDCSync_LoadConfig verifies that env vars round-trip correctly.
func TestOIDCSync_LoadConfig(t *testing.T) {
	// Enabled=true, custom interval.
	env := map[string]string{
		"OIDC_SYNC_ENABLED":  "true",
		"OIDC_SYNC_INTERVAL": "30m",
	}
	cfg := LoadOIDCSyncConfigFromEnv(func(k string) string { return env[k] })
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.Interval != 30*time.Minute {
		t.Errorf("Interval: want 30m, got %v", cfg.Interval)
	}

	// Disabled by default.
	cfg2 := LoadOIDCSyncConfigFromEnv(func(_ string) string { return "" })
	if cfg2.Enabled {
		t.Error("expected Enabled=false when env unset")
	}
	if cfg2.Interval != time.Hour {
		t.Errorf("default Interval: want 1h, got %v", cfg2.Interval)
	}

	// Invalid interval falls back to 1h.
	env3 := map[string]string{
		"OIDC_SYNC_ENABLED":  "TRUE",
		"OIDC_SYNC_INTERVAL": "not-a-duration",
	}
	cfg3 := LoadOIDCSyncConfigFromEnv(func(k string) string { return env3[k] })
	if !cfg3.Enabled {
		t.Error("expected Enabled=true for 'TRUE'")
	}
	if cfg3.Interval != time.Hour {
		t.Errorf("invalid duration should fall back to 1h, got %v", cfg3.Interval)
	}
}
