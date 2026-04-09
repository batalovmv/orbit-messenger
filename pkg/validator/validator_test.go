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
