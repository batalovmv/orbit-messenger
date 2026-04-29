// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/featureflags"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// mockFlagStore is an in-memory FeatureFlagStore. Tests can pre-seed and
// inspect the rows directly.
type mockFlagStore struct {
	mu      sync.Mutex
	rows    map[string]store.FeatureFlag
	listErr error
}

func newMockFlagStore() *mockFlagStore {
	return &mockFlagStore{rows: map[string]store.FeatureFlag{}}
}

func (m *mockFlagStore) List(_ context.Context) ([]store.FeatureFlag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	out := make([]store.FeatureFlag, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r)
	}
	return out, nil
}

func (m *mockFlagStore) Get(_ context.Context, key string) (*store.FeatureFlag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[key]; ok {
		return &r, nil
	}
	return nil, nil
}

func (m *mockFlagStore) Upsert(_ context.Context, key string, enabled bool, description string, metadata json.RawMessage) (*store.FeatureFlag, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[key]
	r.Key = key
	r.Enabled = enabled
	if description != "" {
		r.Description = description
	}
	if metadata != nil {
		r.Metadata = metadata
	}
	m.rows[key] = r
	return &r, nil
}

func TestFeatureFlagService_Set_RequiresPermission(t *testing.T) {
	svc := NewFeatureFlagService(newMockFlagStore(), &mockAuditStore{})
	_, err := svc.Set(context.Background(), uuid.New(), "member", featureflags.KeyMaintenanceMode, true, nil, "ip", "ua")
	if err == nil {
		t.Fatalf("expected forbidden, got nil")
	}
}

func TestFeatureFlagService_Set_RejectsUnknownKey(t *testing.T) {
	svc := NewFeatureFlagService(newMockFlagStore(), &mockAuditStore{})
	_, err := svc.Set(context.Background(), uuid.New(), "superadmin", "totally_made_up_key", true, nil, "ip", "ua")
	if err == nil {
		t.Fatalf("expected bad_request for unknown key, got nil")
	}
}

func TestFeatureFlagService_Set_AuditFirst_FailClosed(t *testing.T) {
	store := newMockFlagStore()
	audit := &mockAuditStore{logFn: func(_ context.Context, _ *model.AuditEntry) error {
		return errors.New("audit insert blew up")
	}}
	svc := NewFeatureFlagService(store, audit)

	_, err := svc.Set(context.Background(), uuid.New(), "superadmin", featureflags.KeyMaintenanceMode, true, map[string]interface{}{"message": "x"}, "ip", "ua")
	if err == nil {
		t.Fatalf("expected error when audit fails")
	}
	if len(store.rows) != 0 {
		t.Fatalf("expected store to remain unchanged on audit failure, got %d rows", len(store.rows))
	}
}

func TestFeatureFlagService_Set_MaintenanceMetadataSanitised(t *testing.T) {
	flagStore := newMockFlagStore()
	auditCalls := 0
	audit := &mockAuditStore{logFn: func(_ context.Context, e *model.AuditEntry) error {
		auditCalls++
		if e.Action != model.AuditMaintenanceEnable {
			t.Fatalf("expected maintenance.enable, got %s", e.Action)
		}
		return nil
	}}
	svc := NewFeatureFlagService(flagStore, audit)

	flag, err := svc.Set(context.Background(), uuid.New(), "superadmin", featureflags.KeyMaintenanceMode, true, map[string]interface{}{
		"message":      "плановое обновление",
		"block_writes": true,
		"injected":     "must be dropped",
	}, "ip", "ua")
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !flag.Enabled || flag.Metadata["block_writes"] != true {
		t.Fatalf("metadata not preserved: %+v", flag.Metadata)
	}
	if _, ok := flag.Metadata["injected"]; ok {
		t.Fatalf("unexpected injected key kept in metadata: %+v", flag.Metadata)
	}
	if auditCalls != 1 {
		t.Fatalf("expected exactly 1 audit call, got %d", auditCalls)
	}
}

// TestFeatureFlagService_ListAll_PropagatesDangerous locks in the contract
// that featureflags.Definition.Dangerous flows through to the AdminFlag
// JSON. The frontend uses this bit to decide whether to show a confirm
// modal on toggle-on; if the registry says dangerous and the API drops it,
// the modal silently disappears.
func TestFeatureFlagService_ListAll_PropagatesDangerous(t *testing.T) {
	svc := NewFeatureFlagService(newMockFlagStore(), &mockAuditStore{})
	flags, err := svc.ListAll(context.Background(), uuid.New(), "superadmin", "ip", "ua")
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	byKey := map[string]AdminFlag{}
	for _, f := range flags {
		byKey[f.Key] = f
	}
	// e2e_dm_enabled is registered as Dangerous: true.
	if !byKey[featureflags.KeyE2EDirectMessages].Dangerous {
		t.Errorf("expected %s to be marked dangerous", featureflags.KeyE2EDirectMessages)
	}
	// maintenance_mode is NOT dangerous (separate Class=control).
	if byKey[featureflags.KeyMaintenanceMode].Dangerous {
		t.Errorf("maintenance_mode must not be marked dangerous")
	}
}

func TestFeatureFlagService_VisibleFlags_FiltersByExposure(t *testing.T) {
	flagStore := newMockFlagStore()
	flagStore.rows[featureflags.KeyMaintenanceMode] = store.FeatureFlag{
		Key: featureflags.KeyMaintenanceMode, Enabled: true,
		Metadata: json.RawMessage(`{"message":"x","block_writes":false}`),
	}
	flagStore.rows[featureflags.KeyE2EDirectMessages] = store.FeatureFlag{
		Key: featureflags.KeyE2EDirectMessages, Enabled: true,
	}
	svc := NewFeatureFlagService(flagStore, &mockAuditStore{})

	unauth := svc.VisibleFlags(context.Background(), featureflags.ExposureUnauth)
	auth := svc.VisibleFlags(context.Background(), featureflags.ExposureAuth)

	hasKey := func(set []PublicFlag, key string) bool {
		for _, f := range set {
			if f.Key == key {
				return true
			}
		}
		return false
	}

	if !hasKey(unauth, featureflags.KeyMaintenanceMode) {
		t.Errorf("unauth should see maintenance_mode")
	}
	if hasKey(unauth, featureflags.KeyE2EDirectMessages) {
		t.Errorf("unauth must NOT see e2e_dm_enabled (auth-exposed)")
	}
	if !hasKey(auth, featureflags.KeyMaintenanceMode) || !hasKey(auth, featureflags.KeyE2EDirectMessages) {
		t.Errorf("auth audience should see both flags, got %+v", auth)
	}
}

// TestFeatureFlagService_Maintenance_ScheduledWindow exercises the scheduled
// mode: an enabled flag is only effectively active when `now` falls inside
// the optional start_at / end_at window. The window is read from JSONB
// metadata at request time — no background sweeper, no migration.
func TestFeatureFlagService_Maintenance_ScheduledWindow(t *testing.T) {
	flagStore := newMockFlagStore()
	flagStore.rows[featureflags.KeyMaintenanceMode] = store.FeatureFlag{
		Key: featureflags.KeyMaintenanceMode, Enabled: true,
		Metadata: json.RawMessage(
			`{"message":"plan","block_writes":true,` +
				`"start_at":"2026-05-01T08:00:00Z",` +
				`"end_at":"2026-05-01T10:00:00Z"}`),
	}
	svc := NewFeatureFlagService(flagStore, &mockAuditStore{})
	ctx := context.Background()

	// Before the window: enabled=true but Active must be false — banner is
	// suppressed until start_at.
	before := svc.maintenanceAt(ctx, mustParseRFC3339(t, "2026-05-01T07:59:00Z"))
	if before.Active {
		t.Fatalf("expected inactive before start_at, got %+v", before)
	}

	// Inside the window: Active.
	during := svc.maintenanceAt(ctx, mustParseRFC3339(t, "2026-05-01T08:30:00Z"))
	if !during.Active {
		t.Fatalf("expected active inside window, got %+v", during)
	}

	// After end_at: window expired, banner clears even though enabled=true.
	after := svc.maintenanceAt(ctx, mustParseRFC3339(t, "2026-05-01T10:01:00Z"))
	if after.Active {
		t.Fatalf("expected inactive after end_at, got %+v", after)
	}

	// Open-ended (no end_at): always active once start_at passes.
	flagStore.rows[featureflags.KeyMaintenanceMode] = store.FeatureFlag{
		Key: featureflags.KeyMaintenanceMode, Enabled: true,
		Metadata: json.RawMessage(`{"message":"open","block_writes":false,"start_at":"2026-05-01T08:00:00Z"}`),
	}
	svc.invalidate()
	openAfter := svc.maintenanceAt(ctx, mustParseRFC3339(t, "2026-12-31T23:59:00Z"))
	if !openAfter.Active {
		t.Fatalf("expected active for open-ended window past start, got %+v", openAfter)
	}
}

func TestFeatureFlagService_Maintenance_SnapshotShape(t *testing.T) {
	flagStore := newMockFlagStore()
	flagStore.rows[featureflags.KeyMaintenanceMode] = store.FeatureFlag{
		Key: featureflags.KeyMaintenanceMode, Enabled: true,
		Metadata: json.RawMessage(`{"message":"hello","block_writes":true}`),
	}
	svc := NewFeatureFlagService(flagStore, &mockAuditStore{})
	st := svc.Maintenance(context.Background())
	if !st.Active || !st.BlockWrites || st.Message != "hello" {
		t.Fatalf("unexpected maintenance state: %+v", st)
	}
}

// TestFeatureFlagService_PublicMaintenance_StripsOperatorMetadata locks in
// the GPT-5.5 review finding: the unauthenticated endpoint must NOT leak
// `since` or `updated_by` (admin UUID + toggle timestamp), since it would
// let an unauth visitor enumerate operator accounts and toggle history.
func TestFeatureFlagService_PublicMaintenance_StripsOperatorMetadata(t *testing.T) {
	flagStore := newMockFlagStore()
	flagStore.rows[featureflags.KeyMaintenanceMode] = store.FeatureFlag{
		Key: featureflags.KeyMaintenanceMode, Enabled: true,
		// Window deliberately covers the entire test-suite lifetime
		// (1970-2099) so this stays a public-leak assertion, not a window
		// behaviour assertion.
		Metadata: json.RawMessage(
			`{"message":"тех. работы","block_writes":true,` +
				`"since":"2026-04-27T12:00:00Z",` +
				`"updated_by":"4ac0c4d2-1111-2222-3333-444455556666",` +
				`"start_at":"1970-01-01T00:00:00Z",` +
				`"end_at":"2099-12-31T00:00:00Z"}`),
	}
	svc := NewFeatureFlagService(flagStore, &mockAuditStore{})

	full := svc.Maintenance(context.Background())
	if full.UpdatedBy == nil || full.Since == nil {
		t.Fatalf("admin Maintenance must include since/updated_by, got %+v", full)
	}

	pub := svc.PublicMaintenance(context.Background())
	// Marshal to JSON and assert the leak-safe shape — easier to read than
	// reflect-based field checks and matches the wire format clients see.
	// The scheduled-mode timestamps (`start_at` / `end_at`) ARE operator
	// metadata too — leaking them tells a passerby "maintenance starts at X"
	// before the operator has decided to communicate that. Only the
	// effective `Active` bit, computed at read time, is public.
	raw, _ := json.Marshal(pub)
	for _, banned := range []string{`"since"`, `"updated_by"`, `"start_at"`, `"end_at"`} {
		if bytes.Contains(raw, []byte(banned)) {
			t.Errorf("public maintenance payload leaks %s: %s", banned, raw)
		}
	}
	if !pub.Active || !pub.BlockWrites || pub.Message != "тех. работы" {
		t.Fatalf("public maintenance lost legitimate fields: %+v", pub)
	}
}

// TestSanitizeMaintenanceMetadata_ScheduledWindow exercises the parser:
// valid RFC3339 + datetime-local round-trips, invalid strings drop, and
// end < start drops the whole window so we never write a degenerate state.
func TestSanitizeMaintenanceMetadata_ScheduledWindow(t *testing.T) {
	actor := uuid.MustParse("11111111-2222-3333-4444-555566667777")

	t.Run("valid RFC3339 window preserved", func(t *testing.T) {
		out := sanitizeMaintenanceMetadata(map[string]interface{}{
			"message":      "x",
			"block_writes": false,
			"start_at":     "2026-05-01T08:00:00Z",
			"end_at":       "2026-05-01T10:00:00Z",
		}, actor)
		if out["start_at"] != "2026-05-01T08:00:00Z" {
			t.Errorf("expected RFC3339 start_at preserved, got %v", out["start_at"])
		}
		if out["end_at"] != "2026-05-01T10:00:00Z" {
			t.Errorf("expected RFC3339 end_at preserved, got %v", out["end_at"])
		}
	})

	t.Run("datetime-local form normalised to RFC3339", func(t *testing.T) {
		out := sanitizeMaintenanceMetadata(map[string]interface{}{
			"start_at": "2026-05-01T08:00",
		}, actor)
		if out["start_at"] != "2026-05-01T08:00:00Z" {
			t.Errorf("datetime-local should normalise to RFC3339, got %v", out["start_at"])
		}
	})

	t.Run("garbage time dropped", func(t *testing.T) {
		out := sanitizeMaintenanceMetadata(map[string]interface{}{
			"start_at": "tomorrow",
			"end_at":   "next tuesday",
		}, actor)
		if _, ok := out["start_at"]; ok {
			t.Errorf("garbage start_at should be dropped: %v", out)
		}
		if _, ok := out["end_at"]; ok {
			t.Errorf("garbage end_at should be dropped: %v", out)
		}
	})

	t.Run("end before start drops both", func(t *testing.T) {
		out := sanitizeMaintenanceMetadata(map[string]interface{}{
			"start_at": "2026-05-01T10:00:00Z",
			"end_at":   "2026-05-01T08:00:00Z",
		}, actor)
		if _, ok := out["start_at"]; ok {
			t.Errorf("inverted window: start_at must be dropped, got %v", out)
		}
		if _, ok := out["end_at"]; ok {
			t.Errorf("inverted window: end_at must be dropped, got %v", out)
		}
	})

	t.Run("equal start and end drops both (zero-length window)", func(t *testing.T) {
		out := sanitizeMaintenanceMetadata(map[string]interface{}{
			"start_at": "2026-05-01T10:00:00Z",
			"end_at":   "2026-05-01T10:00:00Z",
		}, actor)
		if _, ok := out["start_at"]; ok {
			t.Errorf("equal start==end: start_at must be dropped, got %v", out)
		}
		if _, ok := out["end_at"]; ok {
			t.Errorf("equal start==end: end_at must be dropped, got %v", out)
		}
	})
}

func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse RFC3339 %q: %v", s, err)
	}
	return tt
}

func TestFeatureFlagService_ListAll_RequiresPermission(t *testing.T) {
	svc := NewFeatureFlagService(newMockFlagStore(), &mockAuditStore{})
	_, err := svc.ListAll(context.Background(), uuid.New(), "member", "ip", "ua")
	if err == nil {
		t.Fatalf("member must be forbidden")
	}
	_, err = svc.ListAll(context.Background(), uuid.New(), "superadmin", "ip", "ua")
	if err != nil {
		t.Fatalf("superadmin must succeed: %v", err)
	}
}
