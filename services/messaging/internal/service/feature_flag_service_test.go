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
		Metadata: json.RawMessage(
			`{"message":"тех. работы","block_writes":true,` +
				`"since":"2026-04-27T12:00:00Z",` +
				`"updated_by":"4ac0c4d2-1111-2222-3333-444455556666"}`),
	}
	svc := NewFeatureFlagService(flagStore, &mockAuditStore{})

	full := svc.Maintenance(context.Background())
	if full.UpdatedBy == nil || full.Since == nil {
		t.Fatalf("admin Maintenance must include since/updated_by, got %+v", full)
	}

	pub := svc.PublicMaintenance(context.Background())
	// Marshal to JSON and assert the leak-safe shape — easier to read than
	// reflect-based field checks and matches the wire format clients see.
	raw, _ := json.Marshal(pub)
	for _, banned := range []string{`"since"`, `"updated_by"`} {
		if bytes.Contains(raw, []byte(banned)) {
			t.Errorf("public maintenance payload leaks %s: %s", banned, raw)
		}
	}
	if !pub.Active || !pub.BlockWrites || pub.Message != "тех. работы" {
		t.Fatalf("public maintenance lost legitimate fields: %+v", pub)
	}
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
