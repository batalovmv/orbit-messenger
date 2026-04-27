// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/featureflags"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

// flagsCacheTTL is how long a single process keeps cached values before
// re-reading from Postgres. Short enough that a flag toggle propagates in
// well under a minute; long enough that a tight read loop does not hammer
// the DB. See the GPT-5.5 design memo: cache is acceleration, DB is truth.
const flagsCacheTTL = 30 * time.Second

// MaintenanceState is the parsed view of feature_flags(maintenance_mode).
// Stored in DB as `metadata` JSONB on the flag row.
//
// MaintenanceState is the AUTH/admin shape — it includes operator metadata
// (`since`, `updated_by`). Use PublicMaintenance() for unauthenticated
// responses; that strips the operator UUID + timestamp so a passerby on
// the login screen cannot enumerate active admin accounts.
type MaintenanceState struct {
	Active      bool       `json:"active"`
	Message     string     `json:"message,omitempty"`
	BlockWrites bool       `json:"block_writes"`
	Since       *time.Time `json:"since,omitempty"`
	UpdatedBy   *string    `json:"updated_by,omitempty"`
}

// PublicMaintenanceState is the leak-safe projection of MaintenanceState
// returned from the unauthenticated /public/system/config endpoint. It
// deliberately omits `since` and `updated_by` so the public payload reveals
// nothing about which operator toggled the flag or when.
type PublicMaintenanceState struct {
	Active      bool   `json:"active"`
	Message     string `json:"message,omitempty"`
	BlockWrites bool   `json:"block_writes"`
}

// PublicFlag is the safe-for-client projection of a feature flag.
type PublicFlag struct {
	Key         string `json:"key"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

// AdminFlag is the full view (admin only).
type AdminFlag struct {
	Key         string                 `json:"key"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
	Exposure    string                 `json:"exposure"`
	Class       string                 `json:"class"`
	Known       bool                   `json:"known"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// FeatureFlagService fronts the feature_flags table with a process-local
// TTL cache and the audit-on-mutation pattern used by AdminService.
type FeatureFlagService struct {
	store store.FeatureFlagStore
	audit store.AuditStore

	mu        sync.RWMutex
	cache     map[string]store.FeatureFlag
	cachedAt  time.Time
	cacheLoad sync.Mutex // serialises DB reload, not reads
}

func NewFeatureFlagService(s store.FeatureFlagStore, audit store.AuditStore) *FeatureFlagService {
	return &FeatureFlagService{
		store: s,
		audit: audit,
		cache: map[string]store.FeatureFlag{},
	}
}

// snapshot returns a fresh-enough map of all flags, refreshing from the DB
// when the cache is older than flagsCacheTTL. Safe under concurrent calls.
func (s *FeatureFlagService) snapshot(ctx context.Context) (map[string]store.FeatureFlag, error) {
	s.mu.RLock()
	if time.Since(s.cachedAt) < flagsCacheTTL && len(s.cache) > 0 {
		out := make(map[string]store.FeatureFlag, len(s.cache))
		for k, v := range s.cache {
			out[k] = v
		}
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()

	s.cacheLoad.Lock()
	defer s.cacheLoad.Unlock()

	// Re-check after taking the load lock — another goroutine may have refreshed.
	s.mu.RLock()
	if time.Since(s.cachedAt) < flagsCacheTTL && len(s.cache) > 0 {
		out := make(map[string]store.FeatureFlag, len(s.cache))
		for k, v := range s.cache {
			out[k] = v
		}
		s.mu.RUnlock()
		return out, nil
	}
	s.mu.RUnlock()

	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	fresh := make(map[string]store.FeatureFlag, len(rows))
	for _, r := range rows {
		fresh[r.Key] = r
	}

	s.mu.Lock()
	s.cache = fresh
	s.cachedAt = time.Now()
	s.mu.Unlock()

	out := make(map[string]store.FeatureFlag, len(fresh))
	for k, v := range fresh {
		out[k] = v
	}
	return out, nil
}

// invalidate forces the next snapshot() call to re-read from the DB.
func (s *FeatureFlagService) invalidate() {
	s.mu.Lock()
	s.cachedAt = time.Time{}
	s.mu.Unlock()
}

// IsEnabled reports whether `key` is enabled. Falls back to the registry
// default (or false for unknown keys) on DB errors.
func (s *FeatureFlagService) IsEnabled(ctx context.Context, key string) bool {
	snap, err := s.snapshot(ctx)
	if err == nil {
		if r, ok := snap[key]; ok {
			return r.Enabled
		}
	}
	if d, ok := featureflags.Lookup(key); ok {
		return d.Default
	}
	return false
}

// PublicMaintenance returns the leak-safe maintenance projection for
// unauthenticated callers. Strips operator UUID and timestamp.
func (s *FeatureFlagService) PublicMaintenance(ctx context.Context) PublicMaintenanceState {
	full := s.Maintenance(ctx)
	return PublicMaintenanceState{
		Active:      full.Active,
		Message:     full.Message,
		BlockWrites: full.BlockWrites,
	}
}

// Maintenance returns the parsed maintenance state for client+gateway.
// Always safe to call; on DB error returns "off" — banner suppressed but
// the system stays available.
func (s *FeatureFlagService) Maintenance(ctx context.Context) MaintenanceState {
	snap, err := s.snapshot(ctx)
	if err != nil {
		return MaintenanceState{}
	}
	row, ok := snap[featureflags.KeyMaintenanceMode]
	if !ok {
		return MaintenanceState{}
	}
	var meta MaintenanceState
	if len(row.Metadata) > 0 {
		_ = json.Unmarshal(row.Metadata, &meta) // unknown fields are dropped
	}
	meta.Active = row.Enabled
	return meta
}

// VisibleFlags returns flag values filtered to those whose registry
// exposure is at least `audience`. Keys absent from the registry are
// dropped — they are admin-only by virtue of being unknown.
func (s *FeatureFlagService) VisibleFlags(ctx context.Context, audience featureflags.Exposure) []PublicFlag {
	snap, _ := s.snapshot(ctx)

	out := make([]PublicFlag, 0, len(featureflags.Registry))
	for _, def := range featureflags.Registry {
		if !featureflags.VisibleTo(def.Exposure, audience) {
			continue
		}
		enabled := def.Default
		desc := def.Description
		if row, ok := snap[def.Key]; ok {
			enabled = row.Enabled
			if row.Description != "" {
				desc = row.Description
			}
		}
		out = append(out, PublicFlag{Key: def.Key, Enabled: enabled, Description: desc})
	}
	return out
}

// ListAll returns every flag with its registry annotation, for admin UI.
// Requires SysManageSettings.
func (s *FeatureFlagService) ListAll(ctx context.Context, actorID uuid.UUID, actorRole, ip, ua string) ([]AdminFlag, error) {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageSettings) {
		return nil, apperror.Forbidden("Insufficient permissions")
	}

	rows, err := s.store.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list feature flags: %w", err)
	}

	// Audit the read so admin discovery is traceable. Best-effort: a failed
	// audit on a read does not block the response (matches AuditAuditView).
	_ = s.writeAudit(ctx, actorID, model.AuditFeatureFlagList, "feature_flag", nil, nil, ip, ua)

	byKey := map[string]store.FeatureFlag{}
	for _, r := range rows {
		byKey[r.Key] = r
	}
	seen := map[string]bool{}

	out := make([]AdminFlag, 0, len(rows)+len(featureflags.Registry))
	for _, def := range featureflags.Registry {
		seen[def.Key] = true
		af := AdminFlag{
			Key:         def.Key,
			Enabled:     def.Default,
			Description: def.Description,
			Exposure:    string(def.Exposure),
			Class:       string(def.Class),
			Known:       true,
			Metadata:    map[string]interface{}{},
		}
		if row, ok := byKey[def.Key]; ok {
			af.Enabled = row.Enabled
			if row.Description != "" {
				af.Description = row.Description
			}
			af.UpdatedAt = row.UpdatedAt
			_ = json.Unmarshal(row.Metadata, &af.Metadata)
		}
		out = append(out, af)
	}
	// Surface unknown DB rows so an operator can clean them up.
	for _, row := range rows {
		if seen[row.Key] {
			continue
		}
		af := AdminFlag{
			Key:         row.Key,
			Enabled:     row.Enabled,
			Description: row.Description,
			Exposure:    string(featureflags.ExposureServerOnly),
			Class:       "unknown",
			Known:       false,
			UpdatedAt:   row.UpdatedAt,
			Metadata:    map[string]interface{}{},
		}
		_ = json.Unmarshal(row.Metadata, &af.Metadata)
		out = append(out, af)
	}
	return out, nil
}

// Set toggles a flag and (optionally) replaces its metadata. Audit is
// written FIRST and is fail-closed: if the audit insert fails the toggle
// is rejected. Requires SysManageSettings.
//
// Only known flags may be written through this path — random keys are
// rejected to keep the registry the single source of truth.
func (s *FeatureFlagService) Set(ctx context.Context, actorID uuid.UUID, actorRole, key string, enabled bool, metadata map[string]interface{}, ip, ua string) (*AdminFlag, error) {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageSettings) {
		return nil, apperror.Forbidden("Insufficient permissions")
	}
	def, ok := featureflags.Lookup(key)
	if !ok {
		return nil, apperror.BadRequest("Unknown feature flag")
	}

	prev, err := s.store.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("read current flag: %w", err)
	}

	// Maintenance mode: enforce shape — message ≤ 500 chars, block_writes is
	// strictly bool. Other metadata fields are dropped.
	if key == featureflags.KeyMaintenanceMode {
		metadata = sanitizeMaintenanceMetadata(metadata, actorID)
	}
	metaBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, apperror.BadRequest("Invalid metadata payload")
	}

	prevEnabled := def.Default
	var prevMeta map[string]interface{}
	if prev != nil {
		prevEnabled = prev.Enabled
		_ = json.Unmarshal(prev.Metadata, &prevMeta)
	}

	auditDetails := map[string]interface{}{
		"key":         key,
		"prev":        prevEnabled,
		"next":        enabled,
		"prev_meta":   prevMeta,
		"next_meta":   metadata,
		"description": def.Description,
	}
	action := model.AuditFeatureFlagSet
	if key == featureflags.KeyMaintenanceMode {
		if enabled && !prevEnabled {
			action = model.AuditMaintenanceEnable
		} else if !enabled && prevEnabled {
			action = model.AuditMaintenanceDisable
		} else {
			action = model.AuditMaintenanceUpdate
		}
	}
	if err := s.writeAudit(ctx, actorID, action, "feature_flag", strPtr(key), auditDetails, ip, ua); err != nil {
		return nil, apperror.Internal("audit log write failed")
	}

	row, err := s.store.Upsert(ctx, key, enabled, def.Description, metaBytes)
	if err != nil {
		return nil, fmt.Errorf("upsert feature flag: %w", err)
	}
	s.invalidate()

	out := AdminFlag{
		Key:         row.Key,
		Enabled:     row.Enabled,
		Description: row.Description,
		Exposure:    string(def.Exposure),
		Class:       string(def.Class),
		Known:       true,
		UpdatedAt:   row.UpdatedAt,
		Metadata:    map[string]interface{}{},
	}
	_ = json.Unmarshal(row.Metadata, &out.Metadata)
	return &out, nil
}

// writeAudit mirrors AdminService.writeAudit. Duplicated rather than shared
// because the receivers differ; it is a thin wrapper.
func (s *FeatureFlagService) writeAudit(ctx context.Context, actorID uuid.UUID, action, targetType string, targetID *string, details map[string]interface{}, ip, ua string) error {
	entry := &model.AuditEntry{
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
	}
	if len(details) > 0 {
		entry.Details, _ = json.Marshal(details)
	}
	if ip != "" {
		entry.IPAddress = &ip
	}
	if ua != "" {
		entry.UserAgent = &ua
	}
	return s.audit.Log(ctx, entry)
}

const maintenanceMessageMax = 500

func sanitizeMaintenanceMetadata(in map[string]interface{}, actorID uuid.UUID) map[string]interface{} {
	out := map[string]interface{}{}

	if msg, ok := in["message"].(string); ok {
		msg = strings.TrimSpace(msg)
		if len(msg) > maintenanceMessageMax {
			msg = msg[:maintenanceMessageMax]
		}
		out["message"] = msg
	} else {
		out["message"] = ""
	}

	if bw, ok := in["block_writes"].(bool); ok {
		out["block_writes"] = bw
	} else {
		out["block_writes"] = false
	}

	out["since"] = time.Now().UTC().Format(time.RFC3339)
	out["updated_by"] = actorID.String()
	return out
}
