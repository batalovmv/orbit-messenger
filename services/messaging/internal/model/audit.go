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
)
