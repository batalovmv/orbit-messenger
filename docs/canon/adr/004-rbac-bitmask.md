# ADR 004 — RBAC: 4 System Roles + Chat-Level Bitmask

**Status:** ACCEPTED.
**⚠ If the bitmask values below change, update tests AND this ADR in the same PR.**

## Context

The original TZ described a vaguer hierarchy ("admin / user", with
`superadmin` and `compliance` mentioned only in passing). Real corporate
operation needs cleaner separation between *who can read everything for
audit* and *who can manage the org but not snoop on chats*.

Two orthogonal axes also exist:
- **System-level** authority (deactivate users, view audit log, manage bots).
- **Chat-level** authority (delete messages, change title, ban members).

Modeling them as one role list mixes concerns and makes per-chat overrides
("alice is `member` system-wide but `admin` of #engineering") impossible.

## Decision

Two independent permission systems, both bitmask-based, in
`pkg/permissions/`:

**System roles** (`pkg/permissions/system.go`, 4 roles, hierarchy by rank):

| Role | Rank | Bitmask | Notes |
|------|------|---------|-------|
| `superadmin` | 4 | `AllSysPermissions` = `16383` (all 14 bits) | Full org control |
| `compliance` | 3 | `SysViewAllChats \| SysReadAllContent \| SysViewAuditLog \| SysExportData \| SysViewBotLogs` | Read-only audit access |
| `admin`      | 2 | `SysManageUsers \| SysManageInvites \| SysManageContent \| SysManageBots \| SysManageIntegrations \| SysViewBotLogs` | Org admin, no message read |
| `member`     | 1 | `0` | Default user |

System bits (bits 0-13): `SysManageUsers`, `SysManageInvites`,
`SysManageChats`, `SysViewAllChats`, `SysReadAllContent`, `SysManageSettings`,
`SysManageContent`, `SysViewAuditLog`, `SysExportData`, `SysAssignRoles`,
`SysManageSecurity`, `SysManageBots`, `SysManageIntegrations`,
`SysViewBotLogs`.

**Chat-level permissions** (`pkg/permissions/permissions.go`): per-row
`chat_members.permissions BIGINT` bitmask, sentinel `-1` = "inherit chat
default", `0` = "explicitly nothing". Defaults live in
`chats.default_permissions` (15 = `DefaultGroupPermissions`, see migration
020).

## Consequences

- Global `compliance` role exists and works (contrary to the old
  `divergences.md` line — that note is stale and should be removed when
  this ADR lands).
- Role assignment rules (`CanAssignRole`, `CanModifyUser`): superadmin can
  assign anything; admin can assign only `member`; compliance and member
  cannot assign roles. Last-superadmin guard lives elsewhere
  (handler layer) — do not duplicate it in `pkg/permissions`.
- Migration `036_rbac_system` introduced the four roles and the append-only
  `audit_log` (UPDATE/DELETE blocked by `prevent_audit_mutation` trigger).
- Bitmask values are **load-bearing**: persisted in `chat_members.permissions`
  and computed by `SystemRolePermissions()`. Changing a bit's position
  silently rewrites every existing user's effective permissions. If a bit
  must change, write a migration that re-encodes existing rows and bump
  the test fixtures in `pkg/permissions/system_test.go` and
  `permissions_test.go`.
