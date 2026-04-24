// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type mockUserStore struct {
	getByIDFn      func(ctx context.Context, id uuid.UUID) (*model.User, error)
	deactivateFn   func(ctx context.Context, userID, actorID uuid.UUID) error
	reactivateFn   func(ctx context.Context, userID uuid.UUID) error
	updateRoleFn   func(ctx context.Context, userID uuid.UUID, newRole string) error
	countByRoleFn  func(ctx context.Context, role string) (int, error)
}

func (m *mockUserStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}

func (m *mockUserStore) Update(ctx context.Context, u *model.User) error { return nil }

func (m *mockUserStore) UpdateStatus(ctx context.Context, userID, status string, lastSeenAt *time.Time) error {
	return nil
}

func (m *mockUserStore) Search(ctx context.Context, query string, limit int) ([]model.User, error) {
	return nil, nil
}

func (m *mockUserStore) ListAll(ctx context.Context, limit int) ([]model.User, error) {
	return nil, nil
}

func (m *mockUserStore) ListAllPaginated(ctx context.Context, cursor string, limit int) ([]model.User, string, bool, error) {
	return nil, "", false, nil
}

func (m *mockUserStore) Deactivate(ctx context.Context, userID, actorID uuid.UUID) error {
	if m.deactivateFn != nil {
		return m.deactivateFn(ctx, userID, actorID)
	}
	return nil
}

func (m *mockUserStore) Reactivate(ctx context.Context, userID uuid.UUID) error {
	if m.reactivateFn != nil {
		return m.reactivateFn(ctx, userID)
	}
	return nil
}

func (m *mockUserStore) UpdateRole(ctx context.Context, userID uuid.UUID, newRole string) error {
	if m.updateRoleFn != nil {
		return m.updateRoleFn(ctx, userID, newRole)
	}
	return nil
}

func (m *mockUserStore) CountByRole(ctx context.Context, role string) (int, error) {
	if m.countByRoleFn != nil {
		return m.countByRoleFn(ctx, role)
	}
	return 0, nil
}

type mockAuditStore struct {
	logFn  func(ctx context.Context, entry *model.AuditEntry) error
	listFn func(ctx context.Context, filter store.AuditFilter) ([]model.AuditEntry, string, bool, error)
}

func (m *mockAuditStore) Log(ctx context.Context, entry *model.AuditEntry) error {
	if m.logFn != nil {
		return m.logFn(ctx, entry)
	}
	return nil
}

func (m *mockAuditStore) List(ctx context.Context, filter store.AuditFilter) ([]model.AuditEntry, string, bool, error) {
	if m.listFn != nil {
		return m.listFn(ctx, filter)
	}
	return nil, "", false, nil
}

func newRedisClientForMiniredis(mr *miniredis.Miniredis) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func mustUser(t *testing.T, role string) *model.User {
	t.Helper()
	return &model.User{ID: uuid.New(), Role: role, IsActive: true, DisplayName: "Test User", Email: "user@example.com", Status: "online", AccountType: "standard", CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

func TestDeactivateUser_WritesJWTBlacklist(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()
	targetID := uuid.New()
	order := make([]string, 0, 2)

	mr := newMiniredis(t)
	rdb := newRedisClientForMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			if id != targetID {
				t.Fatalf("GetByID called with unexpected id: %s", id)
			}
			return mustUser(t, "member"), nil
		},
		countByRoleFn: func(_ context.Context, role string) (int, error) {
			if role != "superadmin" {
				t.Fatalf("CountByRole called with unexpected role: %s", role)
			}
			return 2, nil
		},
		deactivateFn: func(_ context.Context, userID, actor uuid.UUID) error {
			order = append(order, "deactivate")
			if userID != targetID || actor != actorID {
				t.Fatalf("Deactivate called with unexpected ids: user=%s actor=%s", userID, actor)
			}
			return nil
		},
	}
	audit := &mockAuditStore{
		logFn: func(_ context.Context, entry *model.AuditEntry) error {
			order = append(order, "audit")
			if entry.Action != model.AuditUserDeactivate {
				t.Fatalf("unexpected audit action: %s", entry.Action)
			}
			return nil
		},
	}

	svc := &AdminService{users: users, chats: &mockChatStore{}, messages: &mockMessageStore{}, audit: audit, nats: NewNoopNATSPublisher(), redis: rdb}
	err := svc.DeactivateUser(ctx, actorID, targetID, "admin", "policy violation", "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("DeactivateUser: %v", err)
	}

	if len(order) < 2 || order[0] != "audit" || order[1] != "deactivate" {
		t.Fatalf("expected audit to be written before deactivate, got order %v", order)
	}

	key := fmt.Sprintf("jwt_blacklist:user:%s", targetID.String())
	if exists, err := rdb.Exists(ctx, key).Result(); err != nil {
		t.Fatalf("Redis Exists: %v", err)
	} else if exists != 1 {
		t.Fatalf("expected Redis blacklist key %q to exist", key)
	}
	ttl, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("Redis TTL: %v", err)
	}
	if ttl < 23*time.Hour {
		t.Fatalf("expected TTL >= 23h, got %s", ttl)
	}
	if got, err := rdb.Get(ctx, key).Result(); err != nil {
		t.Fatalf("Redis Get: %v", err)
	} else if got != "1" {
		t.Fatalf("expected Redis value 1, got %q", got)
	}
}

func TestDeactivateUser_RedisDown_ReturnsError(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()
	targetID := uuid.New()

	mr := newMiniredis(t)
	rdb := newRedisClientForMiniredis(mr)
	mr.Close()
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			if id != targetID {
				t.Fatalf("GetByID called with unexpected id: %s", id)
			}
			return mustUser(t, "member"), nil
		},
		countByRoleFn: func(_ context.Context, role string) (int, error) {
			return 2, nil
		},
	}
	audit := &mockAuditStore{logFn: func(_ context.Context, entry *model.AuditEntry) error { return nil }}

	svc := &AdminService{users: users, chats: &mockChatStore{}, messages: &mockMessageStore{}, audit: audit, nats: NewNoopNATSPublisher(), redis: rdb}
	err := svc.DeactivateUser(ctx, actorID, targetID, "admin", "", "", "")
	if err == nil {
		t.Fatal("expected error when Redis is down, got nil")
	}
}

func TestDeactivateUser_AuthFail_NotSuperadmin(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()
	targetID := uuid.New()

	mr := newMiniredis(t)
	rdb := newRedisClientForMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			t.Fatal("GetByID should not be called when actor lacks permissions")
			return nil, nil
		},
	}
	audit := &mockAuditStore{logFn: func(_ context.Context, entry *model.AuditEntry) error {
		t.Fatal("Audit should not be written when actor lacks permissions")
		return nil
	}}

	svc := &AdminService{users: users, chats: &mockChatStore{}, messages: &mockMessageStore{}, audit: audit, nats: NewNoopNATSPublisher(), redis: rdb}
	err := svc.DeactivateUser(ctx, actorID, targetID, "member", "", "", "")
	if err == nil {
		t.Fatal("expected forbidden error, got nil")
	}

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Status != 403 {
		t.Fatalf("expected status 403, got %d", appErr.Status)
	}

	key := fmt.Sprintf("jwt_blacklist:user:%s", targetID.String())
	if exists, err := rdb.Exists(ctx, key).Result(); err != nil {
		t.Fatalf("Redis Exists: %v", err)
	} else if exists == 1 {
		t.Fatalf("did not expect Redis blacklist key %q to be written", key)
	}
}

func TestDeactivateUser_SelfDeactivation(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()

	mr := newMiniredis(t)
	rdb := newRedisClientForMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			t.Fatal("GetByID should not be called for self-deactivation")
			return nil, nil
		},
	}
	audit := &mockAuditStore{logFn: func(_ context.Context, entry *model.AuditEntry) error {
		t.Fatal("Audit should not be written for self-deactivation")
		return nil
	}}

	svc := &AdminService{users: users, chats: &mockChatStore{}, messages: &mockMessageStore{}, audit: audit, nats: NewNoopNATSPublisher(), redis: rdb}
	err := svc.DeactivateUser(ctx, actorID, actorID, "admin", "", "", "")
	if err == nil {
		t.Fatal("expected bad request error, got nil")
	}

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Status != 400 {
		t.Fatalf("expected status 400, got %d", appErr.Status)
	}

	key := fmt.Sprintf("jwt_blacklist:user:%s", actorID.String())
	if exists, err := rdb.Exists(ctx, key).Result(); err != nil {
		t.Fatalf("Redis Exists: %v", err)
	} else if exists == 1 {
		t.Fatalf("did not expect Redis blacklist key %q to be written", key)
	}
}

func TestDeactivateUser_AdminCannotDeactivateSuperadmin(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()
	targetID := uuid.New()

	mr := newMiniredis(t)
	rdb := newRedisClientForMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			if id != targetID {
				t.Fatalf("GetByID called with unexpected id: %s", id)
			}
			return mustUser(t, "superadmin"), nil
		},
		countByRoleFn: func(_ context.Context, role string) (int, error) {
			t.Fatal("CountByRole should not be called when admin cannot modify superadmin")
			return 0, nil
		},
		deactivateFn: func(_ context.Context, userID, actor uuid.UUID) error {
			t.Fatal("Deactivate should not be called when admin cannot modify superadmin")
			return nil
		},
	}
	audit := &mockAuditStore{logFn: func(_ context.Context, entry *model.AuditEntry) error {
		t.Fatal("Audit should not be written when admin cannot modify superadmin")
		return nil
	}}

	svc := &AdminService{users: users, chats: &mockChatStore{}, messages: &mockMessageStore{}, audit: audit, nats: NewNoopNATSPublisher(), redis: rdb}
	err := svc.DeactivateUser(ctx, actorID, targetID, "admin", "", "", "")
	if err == nil {
		t.Fatal("expected forbidden error, got nil")
	}

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Status != 403 {
		t.Fatalf("expected status 403, got %d", appErr.Status)
	}

	key := fmt.Sprintf("jwt_blacklist:user:%s", targetID.String())
	if exists, err := rdb.Exists(ctx, key).Result(); err != nil {
		t.Fatalf("Redis Exists: %v", err)
	} else if exists == 1 {
		t.Fatalf("did not expect Redis blacklist key %q to be written", key)
	}
}

func TestDeactivateUser_MemberCannotDeactivate(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()
	targetID := uuid.New()

	mr := newMiniredis(t)
	rdb := newRedisClientForMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			t.Fatal("GetByID should not be called when member lacks permissions")
			return nil, nil
		},
	}
	audit := &mockAuditStore{logFn: func(_ context.Context, entry *model.AuditEntry) error {
		t.Fatal("Audit should not be written when member lacks permissions")
		return nil
	}}

	svc := &AdminService{users: users, chats: &mockChatStore{}, messages: &mockMessageStore{}, audit: audit, nats: NewNoopNATSPublisher(), redis: rdb}
	err := svc.DeactivateUser(ctx, actorID, targetID, "member", "", "", "")
	if err == nil {
		t.Fatal("expected forbidden error, got nil")
	}

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Status != 403 {
		t.Fatalf("expected status 403, got %d", appErr.Status)
	}

	key := fmt.Sprintf("jwt_blacklist:user:%s", targetID.String())
	if exists, err := rdb.Exists(ctx, key).Result(); err != nil {
		t.Fatalf("Redis Exists: %v", err)
	} else if exists == 1 {
		t.Fatalf("did not expect Redis blacklist key %q to be written", key)
	}
}

func TestDeactivateUser_LastSuperadmin(t *testing.T) {
	ctx := context.Background()
	actorID := uuid.New()
	targetID := uuid.New()

	mr := newMiniredis(t)
	rdb := newRedisClientForMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockUserStore{
		getByIDFn: func(_ context.Context, id uuid.UUID) (*model.User, error) {
			if id != targetID {
				t.Fatalf("GetByID called with unexpected id: %s", id)
			}
			return mustUser(t, "superadmin"), nil
		},
		countByRoleFn: func(_ context.Context, role string) (int, error) {
			if role != "superadmin" {
				t.Fatalf("CountByRole called with unexpected role: %s", role)
			}
			return 1, nil
		},
		deactivateFn: func(_ context.Context, userID, actor uuid.UUID) error {
			t.Fatal("Deactivate should not be called when target is the last superadmin")
			return nil
		},
	}
	audit := &mockAuditStore{logFn: func(_ context.Context, entry *model.AuditEntry) error {
		t.Fatal("Audit should not be written when target is the last superadmin")
		return nil
	}}

	svc := &AdminService{users: users, chats: &mockChatStore{}, messages: &mockMessageStore{}, audit: audit, nats: NewNoopNATSPublisher(), redis: rdb}
	err := svc.DeactivateUser(ctx, actorID, targetID, "superadmin", "", "", "")
	if err == nil {
		t.Fatal("expected bad request error, got nil")
	}

	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected AppError, got %T: %v", err, err)
	}
	if appErr.Status != 400 {
		t.Fatalf("expected status 400, got %d", appErr.Status)
	}

	key := fmt.Sprintf("jwt_blacklist:user:%s", targetID.String())
	if exists, err := rdb.Exists(ctx, key).Result(); err != nil {
		t.Fatalf("Redis Exists: %v", err)
	} else if exists == 1 {
		t.Fatalf("did not expect Redis blacklist key %q to be written", key)
	}
}
