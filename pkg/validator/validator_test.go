package validator

import "testing"

func TestRequireString_WhitespaceOnlyFails(t *testing.T) {
	if err := RequireString("   ", "name", 1, 10); err == nil {
		t.Fatal("expected whitespace-only string to fail")
	}
}

func TestRequireString_ZeroWidthSpaceOnlyFails(t *testing.T) {
	if err := RequireString("\u200B\u200B", "name", 1, 10); err == nil {
		t.Fatal("expected zero-width-space-only string to fail")
	}
}

func TestRequireString_TabOnlyFails(t *testing.T) {
	if err := RequireString("\t\t", "name", 1, 10); err == nil {
		t.Fatal("expected tab-only string to fail")
	}
}

func TestRequireString_SurroundingSpacesStillPass(t *testing.T) {
	if err := RequireString("  hello  ", "name", 1, 10); err != nil {
		t.Fatalf("expected trimmed string to pass, got %v", err)
	}
}

func TestRequireUUID_UppercasePasses(t *testing.T) {
	if err := RequireUUID("550E8400-E29B-41D4-A716-446655440000", "id"); err != nil {
		t.Fatalf("expected uppercase UUID to pass, got %v", err)
	}
}

func TestRequireUUID_LowercasePasses(t *testing.T) {
	if err := RequireUUID("550e8400-e29b-41d4-a716-446655440000", "id"); err != nil {
		t.Fatalf("expected lowercase UUID to pass, got %v", err)
	}
}

func TestRequireUUID_InvalidFails(t *testing.T) {
	if err := RequireUUID("not-a-uuid", "id"); err == nil {
		t.Fatal("expected invalid UUID to fail")
	}
}
