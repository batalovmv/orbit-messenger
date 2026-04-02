package permissions

// Capability bits — what a user CAN do.
// Used for both admin capabilities and member defaults.
const (
	CanSendMessages   int64 = 1 << 0 // 1
	CanSendMedia      int64 = 1 << 1 // 2
	CanAddMembers     int64 = 1 << 2 // 4
	CanPinMessages    int64 = 1 << 3 // 8
	CanChangeInfo     int64 = 1 << 4 // 16
	CanDeleteMessages int64 = 1 << 5 // 32
	CanBanUsers       int64 = 1 << 6 // 64
	CanInviteViaLink  int64 = 1 << 7 // 128

	AllPermissions int64 = (1 << 8) - 1 // 255
)

// Role defaults.
const (
	DefaultGroupPermissions   = CanSendMessages | CanSendMedia | CanAddMembers | CanPinMessages // groups: no CanChangeInfo, CanDeleteMessages, CanBanUsers, CanInviteViaLink by default
	DefaultDirectPermissions  = CanSendMessages | CanSendMedia | CanPinMessages                 // direct chats: participants can send text/media and manage pins, but cannot add others
	DefaultChannelPermissions = int64(0)                                                        // channels: only admin/owner can post
	DefaultAdminPermissions   = AllPermissions                                                  // new admins get all capabilities
)

// Has returns true if bit is set in mask.
func Has(mask, bit int64) bool { return mask&bit != 0 }

// Set turns on bit in mask.
func Set(mask, bit int64) int64 { return mask | bit }

// Clear turns off bit in mask.
func Clear(mask, bit int64) int64 { return mask &^ bit }

// PermissionsUnset is the sentinel value meaning "no per-user override, use defaults".
// DB column default is -1. A value of 0 means "explicitly no permissions".
const PermissionsUnset int64 = -1

// EffectivePermissions resolves the final capability set for a member.
//   - Owner: all permissions
//   - Admin: their personal permissions (or default admin if unset / -1)
//   - Member: per-user override if set (not -1), else chat default_permissions
//   - Channel members: 0 (only admin/owner can act)
//   - Banned/readonly: 0
//
// The sentinel PermissionsUnset (-1) distinguishes "not customized" from "explicitly 0".
func EffectivePermissions(role, chatType string, memberPerms, defaultPerms int64) int64 {
	switch role {
	case "owner":
		return AllPermissions
	case "admin":
		if memberPerms != PermissionsUnset {
			return memberPerms
		}
		return DefaultAdminPermissions
	case "banned", "readonly":
		return 0
	default: // "member"
		if chatType == "channel" {
			return 0
		}
		if memberPerms != PermissionsUnset {
			return memberPerms
		}
		return defaultPerms
	}
}

// CanPerform checks if a user can perform a specific action.
func CanPerform(role, chatType string, memberPerms, defaultPerms, required int64) bool {
	return Has(EffectivePermissions(role, chatType, memberPerms, defaultPerms), required)
}

// IsAdminOrOwner returns true for owner or admin roles.
func IsAdminOrOwner(role string) bool {
	return role == "owner" || role == "admin"
}
