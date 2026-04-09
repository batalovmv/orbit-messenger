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

func TestRedactURL_KeywordValueDSN(t *testing.T) {
	got := RedactURL("host=db.example.com port=5432 user=orbit password=s3cret dbname=orbit")
	if got == "" {
		t.Fatal("expected redacted DSN")
	}
	if got == "host=db.example.com port=5432 user=orbit password=s3cret dbname=orbit" {
		t.Fatal("expected password to be redacted")
	}
	if got != "host=db.example.com port=5432 user=orbit password=*** dbname=orbit" {
		t.Fatalf("unexpected redacted DSN: %q", got)
	}
}

func TestRedactURL_PostgresURL(t *testing.T) {
	got := RedactURL("postgres://orbit:s3cret@db.example.com/orbit")
	if got == "" {
		t.Fatal("expected redacted URL")
	}
	if got == "postgres://orbit:s3cret@db.example.com/orbit" {
		t.Fatal("expected URL password to be redacted")
	}
}

func TestRedactURL_Empty(t *testing.T) {
	if got := RedactURL(""); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestRedactURL_InvalidInput(t *testing.T) {
	if got := RedactURL("not a url at all"); got != "***" {
		t.Fatalf("expected safe default, got %q", got)
	}
}
