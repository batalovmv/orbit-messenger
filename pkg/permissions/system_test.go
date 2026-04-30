package permissions

import "testing"

// ---------------------------------------------------------------------------
// SystemRoleRank
// ---------------------------------------------------------------------------

func TestSystemRoleRank(t *testing.T) {
	cases := []struct {
		role string
		want int
	}{
		{"superadmin", 4},
		{"compliance", 3},
		{"admin", 2},
		{"member", 1},
		{"unknown", 0},
		{"", 0},
	}
	for _, tc := range cases {
		got := SystemRoleRank(tc.role)
		if got != tc.want {
			t.Errorf("SystemRoleRank(%q) = %d, want %d", tc.role, got, tc.want)
		}
	}
}

func TestSystemRoleRank_Ordering(t *testing.T) {
	if SystemRoleRank("superadmin") <= SystemRoleRank("compliance") {
		t.Fatal("superadmin should outrank compliance")
	}
	if SystemRoleRank("compliance") <= SystemRoleRank("admin") {
		t.Fatal("compliance should outrank admin")
	}
	if SystemRoleRank("admin") <= SystemRoleRank("member") {
		t.Fatal("admin should outrank member")
	}
}

// ---------------------------------------------------------------------------
// SystemRolePermissions
// ---------------------------------------------------------------------------

func TestSystemRolePermissions_Superadmin(t *testing.T) {
	got := SystemRolePermissions("superadmin")
	if got != AllSysPermissions {
		t.Fatalf("superadmin should get AllSysPermissions (%d), got %d", AllSysPermissions, got)
	}
}

func TestSystemRolePermissions_Compliance(t *testing.T) {
	got := SystemRolePermissions("compliance")
	expected := SysViewAllChats | SysReadAllContent | SysViewAuditLog | SysExportData | SysViewBotLogs
	if got != expected {
		t.Fatalf("compliance should get %d, got %d", expected, got)
	}
}

func TestSystemRolePermissions_Admin(t *testing.T) {
	got := SystemRolePermissions("admin")
	expected := SysManageUsers | SysManageInvites | SysManageContent | SysManageBots | SysManageIntegrations | SysViewBotLogs | SysManageSettings | SysViewAuditLog
	if got != expected {
		t.Fatalf("admin should get %d, got %d", expected, got)
	}
}

func TestSystemRolePermissions_Member(t *testing.T) {
	got := SystemRolePermissions("member")
	if got != 0 {
		t.Fatalf("member should get 0, got %d", got)
	}
}

func TestSystemRolePermissions_Unknown(t *testing.T) {
	got := SystemRolePermissions("hacker")
	if got != 0 {
		t.Fatalf("unknown role should get 0, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// HasSysPermission — all roles × all permission bits
// ---------------------------------------------------------------------------

func TestHasSysPermission_SuperadminHasAll(t *testing.T) {
	bits := []int64{
		SysManageUsers, SysManageInvites, SysManageChats, SysViewAllChats,
		SysReadAllContent, SysManageSettings, SysManageContent, SysViewAuditLog,
		SysExportData, SysAssignRoles, SysManageSecurity,
	}
	for _, bit := range bits {
		if !HasSysPermission("superadmin", bit) {
			t.Errorf("superadmin should have permission bit %d", bit)
		}
	}
}

func TestHasSysPermission_ComplianceReadOnly(t *testing.T) {
	// Should have
	should := []int64{SysViewAllChats, SysReadAllContent, SysViewAuditLog, SysExportData, SysViewBotLogs}
	for _, bit := range should {
		if !HasSysPermission("compliance", bit) {
			t.Errorf("compliance should have permission bit %d", bit)
		}
	}
	// Should NOT have
	shouldNot := []int64{SysManageUsers, SysManageInvites, SysManageChats, SysManageSettings, SysManageContent, SysAssignRoles, SysManageSecurity}
	for _, bit := range shouldNot {
		if HasSysPermission("compliance", bit) {
			t.Errorf("compliance should NOT have permission bit %d", bit)
		}
	}
}

func TestHasSysPermission_AdminManageOnly(t *testing.T) {
	should := []int64{SysManageUsers, SysManageInvites, SysManageContent, SysManageBots, SysManageIntegrations, SysViewBotLogs, SysManageSettings, SysViewAuditLog}
	for _, bit := range should {
		if !HasSysPermission("admin", bit) {
			t.Errorf("admin should have permission bit %d", bit)
		}
	}
	shouldNot := []int64{SysViewAllChats, SysReadAllContent, SysManageChats, SysExportData, SysAssignRoles, SysManageSecurity}
	for _, bit := range shouldNot {
		if HasSysPermission("admin", bit) {
			t.Errorf("admin should NOT have permission bit %d", bit)
		}
	}
}

func TestHasSysPermission_MemberHasNone(t *testing.T) {
	bits := []int64{
		SysManageUsers, SysManageInvites, SysManageChats, SysViewAllChats,
		SysReadAllContent, SysManageSettings, SysManageContent, SysViewAuditLog,
		SysExportData, SysAssignRoles, SysManageSecurity,
	}
	for _, bit := range bits {
		if HasSysPermission("member", bit) {
			t.Errorf("member should NOT have permission bit %d", bit)
		}
	}
}

// ---------------------------------------------------------------------------
// IsPrivilegedRole
// ---------------------------------------------------------------------------

func TestIsPrivilegedRole(t *testing.T) {
	cases := []struct {
		role string
		want bool
	}{
		{"superadmin", true},
		{"compliance", true},
		{"admin", true},
		{"member", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		got := IsPrivilegedRole(tc.role)
		if got != tc.want {
			t.Errorf("IsPrivilegedRole(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// CanAssignRole — full matrix
// ---------------------------------------------------------------------------

func TestCanAssignRole_SuperadminCanAssignAll(t *testing.T) {
	for role := range ValidSystemRoles {
		if !CanAssignRole("superadmin", role) {
			t.Errorf("superadmin should be able to assign %q", role)
		}
	}
}

func TestCanAssignRole_AdminCanOnlyAssignMember(t *testing.T) {
	if !CanAssignRole("admin", "member") {
		t.Fatal("admin should be able to assign member")
	}
	for _, role := range []string{"admin", "compliance", "superadmin"} {
		if CanAssignRole("admin", role) {
			t.Errorf("admin should NOT be able to assign %q", role)
		}
	}
}

func TestCanAssignRole_ComplianceCanAssignNone(t *testing.T) {
	for role := range ValidSystemRoles {
		if CanAssignRole("compliance", role) {
			t.Errorf("compliance should NOT be able to assign %q", role)
		}
	}
}

func TestCanAssignRole_MemberCanAssignNone(t *testing.T) {
	for role := range ValidSystemRoles {
		if CanAssignRole("member", role) {
			t.Errorf("member should NOT be able to assign %q", role)
		}
	}
}

func TestCanAssignRole_InvalidRole(t *testing.T) {
	if CanAssignRole("superadmin", "root") {
		t.Fatal("should not assign invalid role")
	}
}

// ---------------------------------------------------------------------------
// CanModifyUser
// ---------------------------------------------------------------------------

func TestCanModifyUser_SuperadminCanModifyAnyone(t *testing.T) {
	for role := range ValidSystemRoles {
		if !CanModifyUser("superadmin", role) {
			t.Errorf("superadmin should be able to modify %q", role)
		}
	}
}

func TestCanModifyUser_AdminCanModifyMemberOnly(t *testing.T) {
	if !CanModifyUser("admin", "member") {
		t.Fatal("admin should be able to modify member")
	}
	for _, role := range []string{"admin", "compliance", "superadmin"} {
		if CanModifyUser("admin", role) {
			t.Errorf("admin should NOT be able to modify %q", role)
		}
	}
}

func TestCanModifyUser_ComplianceCanModifyNone(t *testing.T) {
	for role := range ValidSystemRoles {
		if CanModifyUser("compliance", role) {
			t.Errorf("compliance should NOT be able to modify %q", role)
		}
	}
}

func TestCanModifyUser_MemberCanModifyNone(t *testing.T) {
	for role := range ValidSystemRoles {
		if CanModifyUser("member", role) {
			t.Errorf("member should NOT be able to modify %q", role)
		}
	}
}

func TestCanModifyUser_UnknownRoles(t *testing.T) {
	if CanModifyUser("unknown", "member") {
		t.Fatal("unknown actor should not modify anyone")
	}
	if CanModifyUser("admin", "unknown") {
		t.Fatal("should not modify unknown target role")
	}
}
