package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AuditEntry represents a single audit log record.
// The audit_log table is append-only (protected by DB triggers).
type AuditEntry struct {
	ID         int64           `json:"id"`
	ActorID    uuid.UUID       `json:"actor_id"`
	Action     string          `json:"action"`
	TargetType string          `json:"target_type"`
	TargetID   *string         `json:"target_id,omitempty"`
	Details    json.RawMessage `json:"details,omitempty"`
	IPAddress  *string         `json:"ip_address,omitempty"`
	UserAgent  *string         `json:"user_agent,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`

	// Joined fields (not stored, populated by queries with JOIN)
	ActorName string `json:"actor_name,omitempty"`
}

// Audit action constants.
const (
	AuditChatPrivilegedRead = "chat.privileged_read"
	AuditUserDeactivate     = "user.deactivate"
	AuditUserReactivate     = "user.reactivate"
	AuditUserRoleChange     = "user.role_change"
	AuditUserSessionsRevoke = "user.sessions_revoked"
	AuditInviteCreate       = "invite.create"
	AuditInviteRevoke       = "invite.revoke"
	AuditAuditView          = "audit.view"
	AuditUserListRead       = "user.list_read"
	AuditDataExport         = "data.export"

	// Feature flag / maintenance mode (added in mig 066).
	AuditFeatureFlagList   = "feature_flag.list"
	AuditFeatureFlagSet    = "feature_flag.set"
	AuditMaintenanceEnable = "maintenance.enable"
	AuditMaintenanceUpdate = "maintenance.update"
	AuditMaintenanceDisable = "maintenance.disable"

	// Welcome flow (mig 069). Two distinct events: an admin flipping the
	// per-chat flag, and an admin running a manual cross-user backfill.
	AuditChatDefaultStatusSet = "chat.default_status_set"
	AuditDefaultChatsBackfill = "default_chats.backfill"

	// Day 5 admin troubleshooting toolkit.
	// AuditPushTestSent records the *intent*; the per-device delivery report
	// is NOT persisted (provider error bodies are noisy + sometimes echo
	// endpoint URLs we don't want in audit storage). Summary counts are
	// included in details: {target_user_id, device_count, sent, failed, stale}.
	AuditPushTestSent = "push.test_sent"

	// AuditAuditExport records a CSV export of the audit log itself —
	// compliance-friendly meta-event so an export operation cannot be
	// performed without leaving its own row in the table. Written BEFORE
	// the stream begins, fail-closed; details capture the active filter so
	// the scope of the export is reconstructable post-hoc.
	AuditAuditExport = "audit.export"
)

// AuditActions returns the canonical, ordered list of known action codes
// for use as a UI dropdown whitelist. Adding a new action constant above
// must be reflected here so the admin filter UI can offer it. The order
// is stable for predictable rendering.
func AuditActions() []string {
	return []string{
		AuditChatPrivilegedRead,
		AuditUserDeactivate,
		AuditUserReactivate,
		AuditUserRoleChange,
		AuditUserSessionsRevoke,
		AuditInviteCreate,
		AuditInviteRevoke,
		AuditAuditView,
		AuditAuditExport,
		AuditUserListRead,
		AuditDataExport,
		AuditFeatureFlagList,
		AuditFeatureFlagSet,
		AuditMaintenanceEnable,
		AuditMaintenanceUpdate,
		AuditMaintenanceDisable,
		AuditChatDefaultStatusSet,
		AuditDefaultChatsBackfill,
		AuditPushTestSent,
	}
}

// AuditTargetTypes returns the canonical list of target_type values written
// by services/* — kept in sync by hand (no compiler enforcement). Used by
// the admin UI dropdown so operators don't have to remember exact strings.
// Source-of-truth grep: `TargetType: ` literals across services/messaging/.
func AuditTargetTypes() []string {
	return []string{
		"system",
		"user",
		"chat",
		"message",
		"feature_flag",
	}
}
