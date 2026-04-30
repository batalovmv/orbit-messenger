package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/permissions"
	"github.com/mst-corp/orbit/services/messaging/internal/model"
	"github.com/mst-corp/orbit/services/messaging/internal/store"
)

type AdminService struct {
	users    store.UserStore
	chats    store.ChatStore
	messages store.MessageStore
	audit    store.AuditStore
	nats     Publisher
	redis    *redis.Client
}

func NewAdminService(users store.UserStore, chats store.ChatStore, messages store.MessageStore, audit store.AuditStore, nats Publisher, rdb *redis.Client) *AdminService {
	return &AdminService{users: users, chats: chats, messages: messages, audit: audit, nats: nats, redis: rdb}
}

// ListAllChats returns all chats (for privileged users with SysViewAllChats).
func (s *AdminService) ListAllChats(ctx context.Context, actorID uuid.UUID, actorRole, cursor string, limit int, ip, ua string) ([]model.Chat, string, bool, error) {
	if !permissions.HasSysPermission(actorRole, permissions.SysViewAllChats) {
		return nil, "", false, apperror.Forbidden("Insufficient permissions")
	}

	chats, nextCursor, hasMore, err := s.chats.ListAllPaginated(ctx, cursor, limit)
	if err != nil {
		return nil, "", false, fmt.Errorf("list all chats: %w", err)
	}

	if err := s.writeAudit(ctx, actorID, model.AuditChatPrivilegedRead, "system", nil,
		map[string]interface{}{"count": len(chats)}, ip, ua); err != nil {
		return nil, "", false, apperror.Internal("audit log write failed")
	}

	return chats, nextCursor, hasMore, nil
}

// ListAllUsers returns all users (for privileged users with SysManageUsers).
func (s *AdminService) ListAllUsers(ctx context.Context, actorID uuid.UUID, actorRole, cursor string, limit int, q string, ip, ua string) ([]model.User, string, bool, error) {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageUsers) {
		return nil, "", false, apperror.Forbidden("Insufficient permissions")
	}

	q = strings.TrimSpace(q)

	var (
		users      []model.User
		nextCursor string
		hasMore    bool
		err        error
	)
	if q != "" {
		users, err = s.users.Search(ctx, q, limit)
	} else {
		users, nextCursor, hasMore, err = s.users.ListAllPaginated(ctx, cursor, limit)
	}
	if err != nil {
		return nil, "", false, fmt.Errorf("list all users: %w", err)
	}

	if err := s.writeAudit(ctx, actorID, model.AuditUserListRead, "system", nil,
		map[string]interface{}{"count": len(users)}, ip, ua); err != nil {
		return nil, "", false, apperror.Internal("audit log write failed")
	}

	return users, nextCursor, hasMore, nil
}

// DeactivateUser sets is_active=false, blacklists sessions, publishes NATS event.
func (s *AdminService) DeactivateUser(ctx context.Context, actorID, targetID uuid.UUID, actorRole, reason, ip, ua string) error {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageUsers) {
		return apperror.Forbidden("Insufficient permissions")
	}

	if actorID == targetID {
		return apperror.BadRequest("Cannot deactivate yourself")
	}

	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		return apperror.NotFound("User not found")
	}

	if !permissions.CanModifyUser(actorRole, target.Role) {
		return apperror.Forbidden("Cannot deactivate a user with equal or higher role")
	}

	// Guard: cannot deactivate the last superadmin
	if target.Role == "superadmin" {
		count, err := s.users.CountByRole(ctx, "superadmin")
		if err != nil {
			return fmt.Errorf("count superadmins: %w", err)
		}
		if count <= 1 {
			return apperror.BadRequest("Cannot deactivate the last superadmin")
		}
	}

	// Audit FIRST — fail-closed
	details := map[string]interface{}{}
	if reason != "" {
		details["reason"] = reason
	}
	if err := s.writeAudit(ctx, actorID, model.AuditUserDeactivate, "user", strPtr(targetID.String()), details, ip, ua); err != nil {
		return apperror.Internal("audit log write failed")
	}

	if err := s.users.Deactivate(ctx, targetID, actorID); err != nil {
		return fmt.Errorf("deactivate user: %w", err)
	}

	// Publish NATS event to force-disconnect WebSocket
	s.nats.Publish(
		fmt.Sprintf("orbit.user.%s.deactivated", targetID),
		"user_deactivated",
		map[string]string{"user_id": targetID.String()},
		nil, actorID.String(),
	)

	// Invalidate all active JWTs for this user — fail-closed
	blacklistKey := fmt.Sprintf("jwt_blacklist:user:%s", targetID.String())
	if err := s.redis.Set(ctx, blacklistKey, "1", 24*time.Hour).Err(); err != nil {
		slog.Error("failed to write JWT user blacklist", "error", err, "user_id", targetID)
		return fmt.Errorf("jwt invalidation failed: %w", err)
	}

	return nil
}

// ReactivateUser sets is_active=true.
func (s *AdminService) ReactivateUser(ctx context.Context, actorID, targetID uuid.UUID, actorRole, ip, ua string) error {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageUsers) {
		return apperror.Forbidden("Insufficient permissions")
	}

	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		return apperror.NotFound("User not found")
	}

	// Audit FIRST — fail-closed
	if err := s.writeAudit(ctx, actorID, model.AuditUserReactivate, "user", strPtr(targetID.String()), nil, ip, ua); err != nil {
		return apperror.Internal("audit log write failed")
	}

	if err := s.users.Reactivate(ctx, targetID); err != nil {
		return fmt.Errorf("reactivate user: %w", err)
	}

	return nil
}

// ChangeUserRole changes a user's system role.
func (s *AdminService) ChangeUserRole(ctx context.Context, actorID, targetID uuid.UUID, actorRole, newRole, ip, ua string) error {
	if !permissions.HasSysPermission(actorRole, permissions.SysAssignRoles) {
		return apperror.Forbidden("Insufficient permissions")
	}

	if !permissions.ValidSystemRoles[newRole] {
		return apperror.BadRequest("Invalid role")
	}

	if !permissions.CanAssignRole(actorRole, newRole) {
		return apperror.Forbidden("Cannot assign this role")
	}

	target, err := s.users.GetByID(ctx, targetID)
	if err != nil {
		return fmt.Errorf("get target user: %w", err)
	}
	if target == nil {
		return apperror.NotFound("User not found")
	}

	if target.Role == newRole {
		return apperror.BadRequest("User already has this role")
	}

	if !permissions.CanModifyUser(actorRole, target.Role) {
		return apperror.Forbidden("Cannot modify a user with equal or higher role")
	}

	// Guard: cannot demote the last superadmin
	if target.Role == "superadmin" {
		count, err := s.users.CountByRole(ctx, "superadmin")
		if err != nil {
			return fmt.Errorf("count superadmins: %w", err)
		}
		if count <= 1 {
			return apperror.BadRequest("Cannot demote the last superadmin")
		}
	}

	// Audit FIRST — fail-closed
	if err := s.writeAudit(ctx, actorID, model.AuditUserRoleChange, "user", strPtr(targetID.String()),
		map[string]interface{}{"old_role": target.Role, "new_role": newRole}, ip, ua); err != nil {
		return apperror.Internal("audit log write failed")
	}

	if err := s.users.UpdateRole(ctx, targetID, newRole); err != nil {
		return fmt.Errorf("update role: %w", err)
	}

	slog.Info("user role changed",
		"actor_id", actorID, "target_id", targetID,
		"old_role", target.Role, "new_role", newRole)

	return nil
}

// GetAuditLog returns audit log entries (for privileged users with SysViewAuditLog).
//
// RBAC policy for the actor_id filter:
//   - superadmin and compliance (the auditor role) can filter by any actor.
//   - admin and lower roles cannot pivot through actor_id to inspect actions
//     of a strictly more privileged user — that would leak escalation hints
//     (e.g. an admin discovering what a superadmin or compliance actually does).
//     They can still see those actions in unfiltered listings; the gate just
//     prevents targeted enumeration.
func (s *AdminService) GetAuditLog(ctx context.Context, actorID uuid.UUID, actorRole string, filter store.AuditFilter, ip, ua string) ([]model.AuditEntry, string, bool, error) {
	if !permissions.HasSysPermission(actorRole, permissions.SysViewAuditLog) {
		return nil, "", false, apperror.Forbidden("Insufficient permissions")
	}

	if filter.ActorID != nil && actorRole != "superadmin" && actorRole != "compliance" {
		target, err := s.users.GetByID(ctx, *filter.ActorID)
		if err != nil {
			return nil, "", false, fmt.Errorf("resolve audit actor filter: %w", err)
		}
		if target != nil && permissions.SystemRoleRank(target.Role) > permissions.SystemRoleRank(actorRole) {
			return nil, "", false, apperror.Forbidden("Cannot filter audit log by a more privileged actor")
		}
	}

	// Log that someone viewed the audit log
	s.writeAudit(ctx, actorID, model.AuditAuditView, "system", nil, nil, ip, ua) //nolint: not fail-closed for viewing

	return s.audit.List(ctx, filter)
}

// AuditExportHardCap caps a single audit-export response at 200,000 rows.
// Two reasons for this number:
//
//  1. Memory budget at the gateway. Gateway proxy.doProxy() buffers the full
//     upstream response with c.Response().SetBody() — at ~500 bytes/row that
//     is ~100MB, which is on the edge of what we want a 1GB Saturn instance
//     to allocate transiently. Higher caps need a streaming proxy path.
//  2. Compliance scope. At a 150-user pilot, 200k rows is roughly three
//     months of audit traffic; if the operator needs more they can scope by
//     date range and run two passes, or use the WAL/PITR-era pg_dump runbook.
const AuditExportHardCap = 200_000

// PreflightAuditExport runs every check that MUST happen before the HTTP
// status code is committed: SysViewAuditLog + SysExportData gates, the
// actor-role pivot guard, and the audit-first row write (fail-closed).
//
// Split out from streaming because fasthttp's SetBodyStreamWriter callback
// runs in a separate goroutine after the handler returns; by then the
// status header is already on the wire and an error can only manifest as
// a "#error" CSV row, which is indistinguishable from a 200-with-empty
// body to a script. All gates that must produce a real 4xx/5xx live here.
//
// Authorization is intentionally stricter than GetAuditLog: requires both
// SysViewAuditLog AND SysExportData. In the role table that means
// 'compliance' and 'superadmin' can export — 'admin' can only read in-app.
// Separation of duties: the operator who makes admin changes is not the
// one who pulls the audit trail off-platform.
func (s *AdminService) PreflightAuditExport(ctx context.Context, actorID uuid.UUID, actorRole string, filter store.AuditFilter, ip, ua string) error {
	if !permissions.HasSysPermission(actorRole, permissions.SysViewAuditLog) {
		return apperror.Forbidden("Insufficient permissions")
	}
	if !permissions.HasSysPermission(actorRole, permissions.SysExportData) {
		return apperror.Forbidden("Audit export requires SysExportData")
	}

	if filter.ActorID != nil && actorRole != "superadmin" && actorRole != "compliance" {
		target, err := s.users.GetByID(ctx, *filter.ActorID)
		if err != nil {
			return fmt.Errorf("resolve audit actor filter: %w", err)
		}
		if target != nil && permissions.SystemRoleRank(target.Role) > permissions.SystemRoleRank(actorRole) {
			return apperror.Forbidden("Cannot filter audit log by a more privileged actor")
		}
	}

	// Audit-first: snapshot the active filter scope so the export operation
	// is reconstructable post-hoc. Fail-closed.
	details := map[string]interface{}{"hard_cap": AuditExportHardCap}
	if filter.ActorID != nil {
		details["actor_id"] = filter.ActorID.String()
	}
	if filter.Action != nil {
		details["action"] = *filter.Action
	}
	if filter.TargetType != nil {
		details["target_type"] = *filter.TargetType
	}
	if filter.TargetID != nil {
		details["target_id"] = *filter.TargetID
	}
	if filter.Since != nil {
		details["since"] = filter.Since.Format(time.RFC3339)
	}
	if filter.Until != nil {
		details["until"] = filter.Until.Format(time.RFC3339)
	}
	if filter.Q != "" {
		details["q"] = filter.Q
	}
	if err := s.writeAudit(ctx, actorID, model.AuditAuditExport, "system", nil, details, ip, ua); err != nil {
		return apperror.Internal("audit log write failed")
	}
	return nil
}

// StreamAuditExport emits audit rows via the callback. Caller MUST have
// invoked PreflightAuditExport first — this method does no auth/audit work,
// just the row stream. Capped at AuditExportHardCap.
func (s *AdminService) StreamAuditExport(ctx context.Context, filter store.AuditFilter, emit func(model.AuditEntry) error) (int, error) {
	return s.audit.Stream(ctx, filter, AuditExportHardCap, emit)
}

// writeAudit is a helper that logs an audit entry.
func (s *AdminService) writeAudit(ctx context.Context, actorID uuid.UUID, action, targetType string, targetID *string, details map[string]interface{}, ip, ua string) error {
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

	if err := s.audit.Log(ctx, entry); err != nil {
		slog.Error("audit log write failed", "error", err, "action", action, "actor_id", actorID)
		return err
	}
	return nil
}

func strPtr(s string) *string { return &s }

// ExportChatMessages streams all messages for a chat as NDJSON to the writer.
// Gated by SysExportData. Audit written FIRST (fail-closed).
func (s *AdminService) ExportChatMessages(ctx context.Context, actorID uuid.UUID, actorRole, chatID string, ip, ua string, writeRow func([]byte) error) error {
	if !permissions.HasSysPermission(actorRole, permissions.SysExportData) {
		return apperror.Forbidden("Insufficient permissions")
	}
	chatUUID, err := uuid.Parse(chatID)
	if err != nil {
		return apperror.BadRequest("Invalid chat ID")
	}
	if err := s.writeAudit(ctx, actorID, model.AuditDataExport, "chat", strPtr(chatID),
		map[string]interface{}{"format": "ndjson"}, ip, ua); err != nil {
		return apperror.Internal("audit log write failed")
	}
	return s.messages.ExportByChatID(ctx, chatUUID, writeRow)
}

// ExportUserData streams all chats for a user as NDJSON.
// Gated by SysExportData. Audit written FIRST (fail-closed).
func (s *AdminService) ExportUserData(ctx context.Context, actorID uuid.UUID, actorRole, targetUserID string, ip, ua string, writeRow func([]byte) error) error {
	if !permissions.HasSysPermission(actorRole, permissions.SysExportData) {
		return apperror.Forbidden("Insufficient permissions")
	}
	targetUUID, err := uuid.Parse(targetUserID)
	if err != nil {
		return apperror.BadRequest("Invalid user ID")
	}
	if err := s.writeAudit(ctx, actorID, model.AuditDataExport, "user", strPtr(targetUserID),
		map[string]interface{}{"format": "ndjson"}, ip, ua); err != nil {
		return apperror.Internal("audit log write failed")
	}
	return s.chats.ExportByUserID(ctx, targetUUID, writeRow)
}

// SetChatDefaultStatus flips the welcome-flow flag on a chat. Gated by
// SysManageSettings — both 'admin' and 'superadmin' have this. Audit row
// is written first so a partial settings change always shows up in the
// audit feed.
//
// joinOrder is preserved on the row regardless of isDefault so an admin can
// re-arrange the order without losing their slot when temporarily flipping
// the flag off; the partial index only includes rows where the flag is true,
// so non-default rows do not pay a planner cost for the join_order value.
func (s *AdminService) SetChatDefaultStatus(ctx context.Context, actorID uuid.UUID, actorRole string, chatID uuid.UUID, isDefault bool, joinOrder int, ip, ua string) error {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageSettings) {
		return apperror.Forbidden("Insufficient permissions")
	}
	if joinOrder < 0 || joinOrder > 1_000_000 {
		return apperror.BadRequest("default_join_order must be in [0, 1000000]")
	}

	if err := s.writeAudit(ctx, actorID, model.AuditChatDefaultStatusSet, "chat", strPtr(chatID.String()),
		map[string]interface{}{
			"is_default":         isDefault,
			"default_join_order": joinOrder,
		}, ip, ua); err != nil {
		return apperror.Internal("audit log write failed")
	}

	if err := s.chats.SetChatDefaultStatus(ctx, chatID, isDefault, joinOrder); err != nil {
		return fmt.Errorf("set chat default status: %w", err)
	}
	return nil
}

func (s *AdminService) PreviewDefaultMemberships(ctx context.Context, actorID uuid.UUID, actorRole, ip, ua string) (*store.DefaultMembershipPreview, error) {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageSettings) {
		return nil, apperror.Forbidden("Insufficient permissions")
	}
	preview, err := s.chats.PreviewDefaultMemberships(ctx)
	if err != nil {
		return nil, fmt.Errorf("preview default memberships: %w", err)
	}
	return preview, nil
}

// BackfillDefaultMemberships joins every existing user to every chat marked
// is_default_for_new_users=true. Manual operator action only — never wired
// to the flag-flip itself. Gated by SysManageSettings.
//
// For each freshly-inserted membership we publish a chat_member_added event
// so already-connected web clients reconcile without a manual refresh. The
// actor on the NATS event is the registering admin (the operator running
// the backfill), to match the audit row.
func (s *AdminService) BackfillDefaultMemberships(ctx context.Context, actorID uuid.UUID, actorRole, ip, ua string) (int, error) {
	if !permissions.HasSysPermission(actorRole, permissions.SysManageSettings) {
		return 0, apperror.Forbidden("Insufficient permissions")
	}

	if err := s.writeAudit(ctx, actorID, model.AuditDefaultChatsBackfill, "system", nil, nil, ip, ua); err != nil {
		return 0, apperror.Internal("audit log write failed")
	}

	inserts, err := s.chats.BackfillDefaultMemberships(ctx)
	if err != nil {
		return 0, fmt.Errorf("backfill default memberships: %w", err)
	}

	// Group by chat so we only fetch member IDs once per chat.
	byChat := make(map[uuid.UUID][]uuid.UUID, len(inserts))
	for _, ins := range inserts {
		byChat[ins.ChatID] = append(byChat[ins.ChatID], ins.UserID)
	}

	for chatID, userIDs := range byChat {
		allMemberIDs, mErr := s.chats.GetMemberIDs(ctx, chatID)
		if mErr != nil {
			slog.WarnContext(ctx, "default-chats backfill: NATS audience lookup failed",
				"chat_id", chatID, "error", mErr)
		}
		for _, userID := range userIDs {
			s.nats.Publish(
				fmt.Sprintf("orbit.chat.%s.member.added", chatID),
				"chat_member_added",
				map[string]string{
					"chat_id": chatID.String(),
					"user_id": userID.String(),
				},
				allMemberIDs,
				actorID.String(),
			)
		}
	}

	return len(inserts), nil
}
