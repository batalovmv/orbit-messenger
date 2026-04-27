// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package featureflags is the canonical, in-code list of every flag the
// system understands. The DB row in `feature_flags` overrides values, but
// unknown DB rows are ignored when computing client-facing config and
// missing rows fall back to the defaults declared here.
//
// Why a registry: a single boolean column cannot encode WHO is allowed to
// see the flag (unauth client, logged-in client, admin only) nor the
// safety class (a feature flag that defaults OFF is rolled out, while a
// hardening flag that defaults ON is a kill-switch). The registry is the
// single source of truth for those policies.
package featureflags

// Exposure describes which audience may read a flag value.
type Exposure string

const (
	// ExposureUnauth — safe to expose on the public /system/config endpoint.
	ExposureUnauth Exposure = "unauth"
	// ExposureAuth — exposed to logged-in clients only.
	ExposureAuth Exposure = "auth"
	// ExposureAdmin — visible only to operators with SysManageSettings.
	ExposureAdmin Exposure = "admin"
	// ExposureServerOnly — never sent to any client; checked server-side only.
	ExposureServerOnly Exposure = "server_only"
)

// Class describes the safety semantics of a flag for failure scenarios
// (DB unavailable, no row, etc.). It is informational for now — the
// effective default is `Default`, but Class makes intent reviewable.
type Class string

const (
	// ClassRiskyFeature — new feature, default OFF; rolled out by toggling on.
	ClassRiskyFeature Class = "risky_feature"
	// ClassHardening — security/correctness improvement, default ON; toggling
	// off is a deliberate kill-switch.
	ClassHardening Class = "hardening"
	// ClassControl — operational control (e.g. maintenance_mode).
	ClassControl Class = "control"
)

// Definition is the in-code description of a single feature flag.
type Definition struct {
	Key         string
	Default     bool
	Description string
	Exposure    Exposure
	Class       Class
}

// Well-known flag keys. Adding a key here is the only correct way to
// introduce a new flag — random keys written to the DB will be ignored
// by the public/auth config endpoints (admin can still see them).
const (
	KeyE2EDirectMessages = "e2e_dm_enabled"
	KeyMaintenanceMode   = "maintenance_mode"
)

// Registry is the immutable list of known flags. Adding a new flag means
// adding a row here AND a migration that seeds the row in feature_flags.
var Registry = []Definition{
	{
		Key:         KeyE2EDirectMessages,
		Default:     false,
		Description: "Enable E2E encryption for new DM chats. Currently inert (Phase 7 Signal Protocol rolled back in mig 053).",
		Exposure:    ExposureAuth,
		Class:       ClassRiskyFeature,
	},
	{
		Key:         KeyMaintenanceMode,
		Default:     false,
		Description: "System-wide maintenance mode. When on, the web client shows a banner; if metadata.block_writes=true the gateway also blocks mutating requests for non-superadmin users.",
		Exposure:    ExposureUnauth,
		Class:       ClassControl,
	},
}

// byKey is a lookup index built once at package init.
var byKey map[string]Definition

func init() {
	byKey = make(map[string]Definition, len(Registry))
	for _, d := range Registry {
		byKey[d.Key] = d
	}
}

// Lookup returns the registry definition for a key, if known.
func Lookup(key string) (Definition, bool) {
	d, ok := byKey[key]
	return d, ok
}

// IsKnown reports whether the key appears in the in-code registry.
func IsKnown(key string) bool {
	_, ok := byKey[key]
	return ok
}

// VisibleTo returns true if a flag with this exposure should be revealed
// to the given audience. Audiences higher in the hierarchy can read all
// lower-exposure flags.
//
//	server_only  → nobody (returns false for any non-server caller)
//	admin        → admin only
//	auth         → admin, auth
//	unauth       → admin, auth, unauth
func VisibleTo(e Exposure, audience Exposure) bool {
	if e == ExposureServerOnly {
		return false
	}
	rank := map[Exposure]int{
		ExposureUnauth: 1,
		ExposureAuth:   2,
		ExposureAdmin:  3,
	}
	return rank[audience] >= rank[e]
}
