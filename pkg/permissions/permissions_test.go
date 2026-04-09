package permissions

import "testing"

// ---------------------------------------------------------------------------
// EffectivePermissions — role-based
// ---------------------------------------------------------------------------

func TestEffectivePermissions_Owner(t *testing.T) {
	got := EffectivePermissions("owner", "group", 0, 0)
	if got != AllPermissions {
		t.Fatalf("owner should get AllPermissions (%d), got %d", AllPermissions, got)
	}
}

func TestEffectivePermissions_Admin_DefaultPerms(t *testing.T) {
	// memberPerms=PermissionsUnset means "no per-user override, use role default".
	got := EffectivePermissions("admin", "group", PermissionsUnset, 100)
	if got != DefaultAdminPermissions {
		t.Fatalf("admin with memberPerms unset should get DefaultAdminPermissions (%d), got %d", DefaultAdminPermissions, got)
	}
}

// TestEffectivePermissions_Admin_ExplicitlyZero documents the sentinel contract:
// an admin with memberPerms == 0 (explicitly set, not unset) gets zero
// capabilities. This is how an owner strips an admin of all rights without
// demoting them.
func TestEffectivePermissions_Admin_ExplicitlyZero(t *testing.T) {
	got := EffectivePermissions("admin", "group", 0, 100)
	if got != 0 {
		t.Fatalf("admin with memberPerms=0 (explicit) should get 0, got %d", got)
	}
}

func TestEffectivePermissions_Admin_CustomPerms(t *testing.T) {
	custom := CanSendMessages | CanPinMessages // 9
	got := EffectivePermissions("admin", "group", custom, 100)
	if got != custom {
		t.Fatalf("admin with custom perms should get %d, got %d", custom, got)
	}
}

func TestEffectivePermissions_Banned(t *testing.T) {
	got := EffectivePermissions("banned", "group", AllPermissions, AllPermissions)
	if got != 0 {
		t.Fatalf("banned should get 0, got %d", got)
	}
}

func TestEffectivePermissions_Readonly(t *testing.T) {
	got := EffectivePermissions("readonly", "group", AllPermissions, AllPermissions)
	if got != 0 {
		t.Fatalf("readonly should get 0, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// EffectivePermissions — member in group vs channel
// ---------------------------------------------------------------------------

func TestEffectivePermissions_MemberGroup_DefaultPerms(t *testing.T) {
	// memberPerms=PermissionsUnset means fallback to chat default_permissions.
	got := EffectivePermissions("member", "group", PermissionsUnset, DefaultGroupPermissions)
	if got != DefaultGroupPermissions {
		t.Fatalf("member in group with memberPerms unset should fallback to defaultPerms (%d), got %d", DefaultGroupPermissions, got)
	}
}

// TestEffectivePermissions_MemberGroup_ExplicitlyZero documents the sentinel
// contract: a member with memberPerms == 0 (explicitly) gets zero, not the
// chat default. This is how an admin mutes a single member.
func TestEffectivePermissions_MemberGroup_ExplicitlyZero(t *testing.T) {
	got := EffectivePermissions("member", "group", 0, DefaultGroupPermissions)
	if got != 0 {
		t.Fatalf("member with memberPerms=0 (explicit) should get 0, got %d", got)
	}
}

func TestEffectivePermissions_MemberGroup_CustomPerms(t *testing.T) {
	custom := CanSendMessages | CanSendMedia // 3
	got := EffectivePermissions("member", "group", custom, AllPermissions)
	if got != custom {
		t.Fatalf("member with custom perms should get %d, got %d", custom, got)
	}
}

func TestEffectivePermissions_MemberChannel_AlwaysZero(t *testing.T) {
	// Channel members always get 0, regardless of memberPerms or defaultPerms
	got := EffectivePermissions("member", "channel", AllPermissions, AllPermissions)
	if got != 0 {
		t.Fatalf("member in channel should get 0, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// CanPerform
// ---------------------------------------------------------------------------

func TestCanPerform_OwnerCanDoAnything(t *testing.T) {
	bits := []int64{CanSendMessages, CanSendMedia, CanAddMembers, CanPinMessages, CanChangeInfo, CanDeleteMessages, CanBanUsers, CanInviteViaLink}
	for _, bit := range bits {
		if !CanPerform("owner", "channel", 0, 0, bit) {
			t.Fatalf("owner should be able to perform bit %d", bit)
		}
	}
}

func TestCanPerform_MemberWithout_CanSendMessages(t *testing.T) {
	perms := AllPermissions &^ CanSendMessages // 254
	if CanPerform("member", "group", perms, AllPermissions, CanSendMessages) {
		t.Fatal("member with perms=254 should NOT be able to send messages")
	}
	if !CanPerform("member", "group", perms, AllPermissions, CanSendMedia) {
		t.Fatal("member with perms=254 should still be able to send media")
	}
}

func TestCanPerform_ChannelMemberBlocked(t *testing.T) {
	if CanPerform("member", "channel", 0, 0, CanSendMessages) {
		t.Fatal("channel member should never be able to send messages")
	}
}

// ---------------------------------------------------------------------------
// Has / Set / Clear
// ---------------------------------------------------------------------------

func TestHas(t *testing.T) {
	mask := CanSendMessages | CanPinMessages // 9
	if !Has(mask, CanSendMessages) {
		t.Fatal("Has should return true for set bit")
	}
	if Has(mask, CanBanUsers) {
		t.Fatal("Has should return false for unset bit")
	}
}

func TestSet(t *testing.T) {
	mask := int64(0)
	mask = Set(mask, CanSendMessages)
	mask = Set(mask, CanBanUsers)
	if mask != CanSendMessages|CanBanUsers {
		t.Fatalf("expected %d, got %d", CanSendMessages|CanBanUsers, mask)
	}
}

func TestClear(t *testing.T) {
	mask := AllPermissions
	mask = Clear(mask, CanSendMessages)
	if Has(mask, CanSendMessages) {
		t.Fatal("CanSendMessages should be cleared")
	}
	if !Has(mask, CanSendMedia) {
		t.Fatal("CanSendMedia should still be set")
	}
}

// ---------------------------------------------------------------------------
// IsAdminOrOwner
// ---------------------------------------------------------------------------

func TestIsAdminOrOwner(t *testing.T) {
	cases := []struct {
		role string
		want bool
	}{
		{"owner", true},
		{"admin", true},
		{"member", false},
		{"banned", false},
		{"readonly", false},
		{"", false},
	}
	for _, tc := range cases {
		got := IsAdminOrOwner(tc.role)
		if got != tc.want {
			t.Errorf("IsAdminOrOwner(%q) = %v, want %v", tc.role, got, tc.want)
		}
	}
}
