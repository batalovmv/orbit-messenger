package permissions

// System-level permission bits — what a user CAN do at the system (org-wide) level.
// These are separate from chat-level permissions (permissions.go).
const (
	SysManageUsers    int64 = 1 << 0  // 1    — create/deactivate/reactivate users
	SysManageInvites  int64 = 1 << 1  // 2    — create/revoke invite codes
	SysManageChats    int64 = 1 << 2  // 4    — delete/archive any chat
	SysViewAllChats   int64 = 1 << 3  // 8    — see list of all chats
	SysReadAllContent int64 = 1 << 4  // 16   — read messages in any chat
	SysManageSettings int64 = 1 << 5  // 32   — system-wide settings
	SysManageContent  int64 = 1 << 6  // 64   — sticker packs, etc.
	SysViewAuditLog   int64 = 1 << 7  // 128  — view audit trail
	SysExportData     int64 = 1 << 8  // 256  — export chat/user data
	SysAssignRoles    int64 = 1 << 9  // 512  — assign system roles
	SysManageSecurity int64 = 1 << 10 // 1024 — reset 2FA, revoke sessions

	AllSysPermissions int64 = (1 << 11) - 1 // 2047
)

// rolePermissions maps each system role to its permission bitmask.
var rolePermissions = map[string]int64{
	"superadmin": AllSysPermissions,
	"compliance": SysViewAllChats | SysReadAllContent | SysViewAuditLog | SysExportData,
	"admin":      SysManageUsers | SysManageInvites | SysManageContent,
	"member":     0,
}

// roleRanks defines the hierarchy. Higher rank = more privileged.
var roleRanks = map[string]int{
	"superadmin": 4,
	"compliance": 3,
	"admin":      2,
	"member":     1,
}

// ValidSystemRoles is the set of valid system-level roles.
var ValidSystemRoles = map[string]bool{
	"superadmin": true,
	"compliance": true,
	"admin":      true,
	"member":     true,
}

// SystemRoleRank returns the hierarchy rank of a system role (higher = more privileged).
// Returns 0 for unknown roles.
func SystemRoleRank(role string) int {
	return roleRanks[role]
}

// SystemRolePermissions returns the permission bitmask for a system role.
func SystemRolePermissions(role string) int64 {
	return rolePermissions[role]
}

// HasSysPermission checks if a system role has a specific system permission.
func HasSysPermission(role string, perm int64) bool {
	return Has(SystemRolePermissions(role), perm)
}

// IsPrivilegedRole returns true if the role has any elevated system permissions.
func IsPrivilegedRole(role string) bool {
	return SystemRolePermissions(role) != 0
}

// CanAssignRole checks if an actor with actorRole can assign newRole to a target.
//
// Rules:
//   - superadmin can assign any role
//   - admin can only assign "member"
//   - compliance and member cannot assign roles
func CanAssignRole(actorRole, newRole string) bool {
	if !ValidSystemRoles[newRole] {
		return false
	}

	switch actorRole {
	case "superadmin":
		return true // superadmin can assign any role
	case "admin":
		return newRole == "member" // admin can only assign member
	default:
		return false // compliance and member cannot assign roles
	}
}

// CanModifyUser checks if an actor can modify a target user's role/status.
// Only roles with SysManageUsers can modify users at all.
// An actor can only modify users with a strictly lower rank.
// Exception: superadmin can modify other superadmins (last-superadmin guard is elsewhere).
func CanModifyUser(actorRole, targetRole string) bool {
	if !HasSysPermission(actorRole, SysManageUsers) {
		return false
	}

	actorRank := SystemRoleRank(actorRole)
	targetRank := SystemRoleRank(targetRole)

	if actorRank == 0 || targetRank == 0 {
		return false
	}

	// superadmin can modify anyone (including other superadmins)
	if actorRole == "superadmin" {
		return true
	}

	// others can only modify strictly lower ranks
	return actorRank > targetRank
}
