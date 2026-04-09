package config

import (
	"testing"
)

func TestMustEnv_WhitespaceOnlyPanics(t *testing.T) {
	t.Setenv("CONFIG_TEST_WHITESPACE", "   ")

	defer func() {
		if recover() == nil {
			t.Fatal("expected MustEnv to panic on whitespace-only value")
		}
	}()

	MustEnv("CONFIG_TEST_WHITESPACE")
}

func TestMustEnv_ReturnsTrimmedValue(t *testing.T) {
	t.Setenv("CONFIG_TEST_SECRET", "  real-secret  ")

	got := MustEnv("CONFIG_TEST_SECRET")
	if got != "real-secret" {
		t.Fatalf("expected trimmed value, got %q", got)
	}
}

func TestEnvOr_WhitespaceOnlyFallsBack(t *testing.T) {
	t.Setenv("CONFIG_TEST_DEFAULT", "   ")

	if got := EnvOr("CONFIG_TEST_DEFAULT", "default"); got != "default" {
		t.Fatalf("expected fallback, got %q", got)
	}
}

func TestParsePostgresURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		wantDSN  string
		wantPass string
	}{
		{
			name:     "url-encoded special chars",
			url:      "postgres://myuser:dM%5B%5DC%3Fa%5E4fG0%7CtJ%3Famd.ZAaIgh%5C%2FyViO@myhost:5432/mydb",
			wantDSN:  "host=myhost port=5432 user=myuser dbname=mydb sslmode=disable",
			wantPass: `dM[]C?a^4fG0|tJ?amd.ZAaIgh\/yViO`,
		},
		{
			name:     "simple password",
			url:      "postgres://user:simplepass@host:5432/db",
			wantDSN:  "host=host port=5432 user=user dbname=db sslmode=disable",
			wantPass: "simplepass",
		},
		{
			name:     "password with encoded @",
			url:      "postgres://user:p%40ss@host:5432/db",
			wantDSN:  "host=host port=5432 user=user dbname=db sslmode=disable",
			wantPass: "p@ss",
		},
		{
			name:     "password with literal @ (split on last @)",
			url:      "postgres://user:p@ss@host:5432/db",
			wantDSN:  "host=host port=5432 user=user dbname=db sslmode=disable",
			wantPass: "p@ss",
		},
		{
			name:     "raw special chars not encoded",
			url:      `postgres://myuser:dM[]C?a^4fG0|tJ?amd.ZAaIgh\/yViO@myhost:5432/mydb`,
			wantDSN:  "host=myhost port=5432 user=myuser dbname=mydb sslmode=disable",
			wantPass: `dM[]C?a^4fG0|tJ?amd.ZAaIgh\/yViO`,
		},
		{
			name:     "no password",
			url:      "postgres://user@host:5432/db",
			wantDSN:  "host=host port=5432 user=user dbname=db sslmode=disable",
			wantPass: "",
		},
		{
			name:     "default port",
			url:      "postgres://user:pass@host/db",
			wantDSN:  "host=host port=5432 user=user dbname=db sslmode=disable",
			wantPass: "pass",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDSN, gotPass, _ := parsePostgresURL(tt.url)
			if gotDSN != tt.wantDSN {
				t.Errorf("DSN:\n  got  = %s\n  want = %s", gotDSN, tt.wantDSN)
			}
			if gotPass != tt.wantPass {
				t.Errorf("Password:\n  got  = %q\n  want = %q", gotPass, tt.wantPass)
			}
		})
	}
}
