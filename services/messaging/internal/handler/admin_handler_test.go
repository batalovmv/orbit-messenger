// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package handler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/service"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type mockAdminUserStore struct {
	listAllPaginatedFn func(ctx context.Context, cursor string, limit int) ([]model.User, string, bool, error)
	getByIDFn          func(ctx context.Context, id uuid.UUID) (*model.User, error)
	getByEmailFn       func(ctx context.Context, email string) (*model.User, error)
}

func (m *mockAdminUserStore) GetByID(ctx context.Context, id uuid.UUID) (*model.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	return nil, nil
}
func (m *mockAdminUserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	if m.getByEmailFn != nil {
		return m.getByEmailFn(ctx, email)
	}
	return nil, nil
}
func (m *mockAdminUserStore) Update(ctx context.Context, u *model.User) error { return nil }
func (m *mockAdminUserStore) UpdateStatus(ctx context.Context, userID, status string, lastSeenAt *time.Time) error {
	return nil
}
func (m *mockAdminUserStore) Search(ctx context.Context, query string, limit int) ([]model.User, error) {
	return nil, nil
}
func (m *mockAdminUserStore) ListAll(ctx context.Context, limit int) ([]model.User, error) { return nil, nil }
func (m *mockAdminUserStore) ListAllPaginated(ctx context.Context, cursor string, limit int) ([]model.User, string, bool, error) {
	if m.listAllPaginatedFn != nil {
		return m.listAllPaginatedFn(ctx, cursor, limit)
	}
	return nil, "", false, nil
}
func (m *mockAdminUserStore) Deactivate(ctx context.Context, userID, actorID uuid.UUID) error { return nil }
func (m *mockAdminUserStore) Reactivate(ctx context.Context, userID uuid.UUID) error { return nil }
func (m *mockAdminUserStore) UpdateRole(ctx context.Context, userID uuid.UUID, newRole string) error { return nil }
func (m *mockAdminUserStore) CountByRole(ctx context.Context, role string) (int, error) { return 0, nil }

type mockAdminAuditStore struct {
	logFn    func(ctx context.Context, entry *model.AuditEntry) error
	streamFn func(ctx context.Context, filter store.AuditFilter, hardCap int, emit func(model.AuditEntry) error) (int, error)
}

func (m *mockAdminAuditStore) Log(ctx context.Context, entry *model.AuditEntry) error {
	if m.logFn != nil {
		return m.logFn(ctx, entry)
	}
	return nil
}

func (m *mockAdminAuditStore) List(ctx context.Context, filter store.AuditFilter) ([]model.AuditEntry, string, bool, error) {
	return nil, "", false, nil
}

func (m *mockAdminAuditStore) Stream(ctx context.Context, filter store.AuditFilter, hardCap int, emit func(model.AuditEntry) error) (int, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, filter, hardCap, emit)
	}
	return 0, nil
}

func newAdminApp(us store.UserStore, audit store.AuditStore, rdb *redis.Client) *fiber.App {
	app := fiber.New()
	h := NewAdminHandler(service.NewAdminService(us, &mockChatStore{}, &mockMessageStore{}, audit, service.NewNoopNATSPublisher(), rdb))
	h.Register(app)
	return app
}

func newRedisClientForHandlerMiniredis(mr *miniredis.Miniredis) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func sampleAdminUser(id uuid.UUID) model.User {
	return model.User{
		ID:          id,
		Email:       "admin@example.com",
		DisplayName: "Admin User",
		Role:        "member",
		Status:      "online",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func TestListAllUsers_HappyPath(t *testing.T) {
	actorID := uuid.New()
	userID := uuid.New()
	order := make([]string, 0, 2)

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockAdminUserStore{
		listAllPaginatedFn: func(_ context.Context, cursor string, limit int) ([]model.User, string, bool, error) {
			if cursor != "" {
				t.Fatalf("expected empty cursor, got %q", cursor)
			}
			if limit != 50 {
				t.Fatalf("expected default limit 50, got %d", limit)
			}
			order = append(order, "users")
			return []model.User{sampleAdminUser(userID)}, "next", false, nil
		},
	}
	audit := &mockAdminAuditStore{
		logFn: func(_ context.Context, entry *model.AuditEntry) error {
			order = append(order, "audit")
			if entry.Action != model.AuditUserListRead {
				t.Fatalf("unexpected audit action: %s", entry.Action)
			}
			if entry.TargetType != "system" {
				t.Fatalf("expected target_type=system, got %q", entry.TargetType)
			}
			return nil
		},
	}

	app := newAdminApp(users, audit, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/users", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if len(order) != 2 || order[0] != "users" || order[1] != "audit" {
		t.Fatalf("expected users lookup before audit write, got order %v", order)
	}
}

func TestListAllUsers_AuthFail(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	usersCalled := false
	auditCalled := false
	users := &mockAdminUserStore{
		listAllPaginatedFn: func(_ context.Context, _ string, _ int) ([]model.User, string, bool, error) {
			usersCalled = true
			return nil, "", false, nil
		},
	}
	audit := &mockAdminAuditStore{
		logFn: func(_ context.Context, _ *model.AuditEntry) error {
			auditCalled = true
			return nil
		},
	}

	app := newAdminApp(users, audit, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/users", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "member")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, raw)
	}
	if usersCalled {
		t.Fatal("expected user list store not to be called on auth failure")
	}
	if auditCalled {
		t.Fatal("expected audit store not to be called on auth failure")
	}
}

func TestListAllUsers_AuditFail(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	users := &mockAdminUserStore{
		listAllPaginatedFn: func(_ context.Context, _ string, _ int) ([]model.User, string, bool, error) {
			return []model.User{sampleAdminUser(uuid.New())}, "", false, nil
		},
	}
	audit := &mockAdminAuditStore{
		logFn: func(_ context.Context, _ *model.AuditEntry) error {
			return context.DeadlineExceeded
		},
	}

	app := newAdminApp(users, audit, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/users", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, raw)
	}
}

func TestExportChat_HappyPath(t *testing.T) {
	actorID := uuid.New()
	chatID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/chats/"+chatID.String()+"/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "compliance")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("expected ndjson content-type, got %q", got)
	}
}

func TestExportChat_AuthFail(t *testing.T) {
	actorID := uuid.New()
	chatID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/chats/"+chatID.String()+"/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "member")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, raw)
	}
}

func TestExportUser_HappyPath(t *testing.T) {
	actorID := uuid.New()
	targetUserID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/users/"+targetUserID.String()+"/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "compliance")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/x-ndjson" {
		t.Fatalf("expected ndjson content-type, got %q", got)
	}
}

func TestExportUser_AuthFail(t *testing.T) {
	actorID := uuid.New()
	targetUserID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/users/"+targetUserID.String()+"/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "member")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403, got %d: %s", resp.StatusCode, raw)
	}
}

// ---------------------------------------------------------------------------
// Audit log export (PR — admin toolkit polish)
// ---------------------------------------------------------------------------

// TestExportAuditLog_HappyPath — compliance can export. Asserts:
//   - 200 OK with text/csv content-type and an attachment Content-Disposition
//   - audit-first ordering: an `audit.export` row hits the audit store
//     BEFORE the streamFn callback fires
//   - rows emitted by streamFn show up as CSV lines after the header
func TestExportAuditLog_HappyPath(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	var (
		auditWritten bool
		auditAction  string
		streamCalled bool
	)
	audit := &mockAdminAuditStore{
		logFn: func(_ context.Context, e *model.AuditEntry) error {
			auditWritten = true
			auditAction = e.Action
			return nil
		},
		streamFn: func(_ context.Context, _ store.AuditFilter, _ int, emit func(model.AuditEntry) error) (int, error) {
			if !auditWritten {
				t.Fatalf("stream invoked before audit-first row was written")
			}
			streamCalled = true
			return 1, emit(model.AuditEntry{
				ID:         42,
				ActorID:    uuid.New(),
				Action:     "user.role_change",
				TargetType: "user",
				ActorName:  "Alice",
				CreatedAt:  time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
			})
		},
	}

	app := newAdminApp(&mockAdminUserStore{}, audit, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/audit-log/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "compliance")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Fatalf("expected text/csv, got %q", got)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd == "" {
		t.Fatalf("expected Content-Disposition header")
	}
	if !auditWritten {
		t.Fatalf("audit-first row was not written")
	}
	if auditAction != model.AuditAuditExport {
		t.Fatalf("expected audit action %q, got %q", model.AuditAuditExport, auditAction)
	}
	if !streamCalled {
		t.Fatalf("stream callback was not invoked")
	}
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte("id,created_at,actor_id,actor_name,action,target_type,target_id,ip_address,user_agent,details")) {
		t.Fatalf("missing CSV header in body: %s", body)
	}
	if !bytes.Contains(body, []byte("user.role_change")) {
		t.Fatalf("missing data row in body: %s", body)
	}
}

// TestExportAuditLog_AdminCannotExport — admin role has SysViewAuditLog but
// not SysExportData; the export endpoint must reject with 403 even though
// the in-app GET /audit-log works.
func TestExportAuditLog_AdminCannotExport(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	var streamCalled bool
	audit := &mockAdminAuditStore{
		streamFn: func(_ context.Context, _ store.AuditFilter, _ int, _ func(model.AuditEntry) error) (int, error) {
			streamCalled = true
			return 0, nil
		},
	}
	app := newAdminApp(&mockAdminUserStore{}, audit, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/audit-log/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "admin")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 for admin role, got %d: %s", resp.StatusCode, raw)
	}
	if streamCalled {
		t.Fatalf("stream must not be invoked when permission check fails")
	}
}

// TestExportAuditLog_RejectsUnknownAction — handler whitelists `action` and
// `target_type` against the in-code registries before reaching the store.
func TestExportAuditLog_RejectsUnknownAction(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/audit-log/export?action=fake.action", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "compliance")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400 for unknown action, got %d: %s", resp.StatusCode, raw)
	}
}

// TestExportAuditLog_FailsClosedOnAuditWriteError — if the audit-first write
// fails, the stream must NOT proceed AND the response must be a real 5xx
// (not a 200 OK CSV with a "#error" row, which is what a naive
// SetBodyStreamWriter implementation would surface).
func TestExportAuditLog_FailsClosedOnAuditWriteError(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	var streamCalled bool
	audit := &mockAdminAuditStore{
		logFn: func(_ context.Context, _ *model.AuditEntry) error {
			return fmt.Errorf("audit DB unavailable")
		},
		streamFn: func(_ context.Context, _ store.AuditFilter, _ int, _ func(model.AuditEntry) error) (int, error) {
			streamCalled = true
			return 0, nil
		},
	}
	app := newAdminApp(&mockAdminUserStore{}, audit, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/audit-log/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "compliance")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	// Audit-first runs synchronously before SetBodyStreamWriter, so the
	// failure surfaces as an HTTP 500 with a JSON error body — not a 200
	// streaming a "#error" CSV row.
	if resp.StatusCode != http.StatusInternalServerError {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, raw)
	}
	if got := resp.Header.Get("Content-Type"); got == "text/csv; charset=utf-8" {
		t.Fatalf("must not commit text/csv response when audit write fails")
	}
	if streamCalled {
		t.Fatalf("stream must not be invoked when audit-first write fails")
	}
}

// TestExportAuditLog_CSVInjectionDefanged — values like a user_agent that
// starts with '=' or '@' are spreadsheet-formula vectors when the export
// is opened in Excel/LibreOffice. Cells must be prefixed with a single
// quote on output.
func TestExportAuditLog_CSVInjectionDefanged(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	uaPayload := "=cmd|'/c calc'!A1"
	audit := &mockAdminAuditStore{
		streamFn: func(_ context.Context, _ store.AuditFilter, _ int, emit func(model.AuditEntry) error) (int, error) {
			ua := uaPayload
			return 1, emit(model.AuditEntry{
				ID:         1,
				ActorID:    uuid.New(),
				Action:     "user.list_read",
				TargetType: "system",
				CreatedAt:  time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
				UserAgent:  &ua,
			})
		},
	}
	app := newAdminApp(&mockAdminUserStore{}, audit, rdb)
	req, _ := http.NewRequest(http.MethodGet, "/admin/audit-log/export", bytes.NewBuffer(nil))
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "compliance")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	// The dangerous cell must be prefixed with ' (encoding/csv will quote
	// the cell because of the embedded comma; the leading ' is preserved
	// inside the quotes).
	if !bytes.Contains(body, []byte("'=cmd")) {
		t.Fatalf("CSV injection prefix missing for user_agent in body: %s", body)
	}
}

// ---------------------------------------------------------------------------
// Welcome flow (mig 069) — admin handlers
// ---------------------------------------------------------------------------

// TestSetChatDefaultStatus_AdminGate — non-admin (role=member) must get 403.
// The service rejects, but we also exercise the handler-level body parse +
// chat-id validation along the way.
func TestSetChatDefaultStatus_AdminGate(t *testing.T) {
	actorID := uuid.New()
	chatID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)

	body := bytes.NewBufferString(`{"is_default":true,"default_join_order":1}`)
	req, _ := http.NewRequest(http.MethodPut,
		"/admin/chats/"+chatID.String()+"/default-status", body)
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "member")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 for non-admin, got %d: %s", resp.StatusCode, raw)
	}
}

// TestBackfillDefaultMemberships_AdminGate — same shape as above, on the
// system-wide backfill endpoint.
func TestBackfillDefaultMemberships_AdminGate(t *testing.T) {
	actorID := uuid.New()

	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)
	req, _ := http.NewRequest(http.MethodPost, "/admin/default-chats/backfill", nil)
	req.Header.Set("X-User-ID", actorID.String())
	req.Header.Set("X-User-Role", "member")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 403 for non-admin, got %d: %s", resp.StatusCode, raw)
	}
}

// TestSetChatDefaultStatus_BadChatID — handler rejects malformed UUID with
// 400 before reaching the service.
func TestSetChatDefaultStatus_BadChatID(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := newRedisClientForHandlerMiniredis(mr)
	t.Cleanup(func() { _ = rdb.Close() })

	app := newAdminApp(&mockAdminUserStore{}, &mockAdminAuditStore{}, rdb)
	req, _ := http.NewRequest(http.MethodPut, "/admin/chats/not-a-uuid/default-status",
		bytes.NewBufferString(`{"is_default":true}`))
	req.Header.Set("X-User-ID", uuid.NewString())
	req.Header.Set("X-User-Role", "admin")
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad chat id, got %d", resp.StatusCode)
	}
}
