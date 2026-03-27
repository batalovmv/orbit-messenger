package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// MustEnv returns the value of the environment variable or panics if not set.
func MustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required environment variable %s is not set", key))
	}
	return v
}

// EnvOr returns the value of the environment variable or the fallback if not set.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// EnvIntOr returns the integer value of the environment variable or the fallback.
func EnvIntOr(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// DatabaseURL returns a pgx keyword=value DSN.
// If DATABASE_URL is set (postgres:// URL from Saturn), it is parsed manually
// and converted to DSN format. Saturn may inject passwords with special chars
// ([], \, ?, ^, |) without URL-encoding, which breaks Go's url.Parse.
func DatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return parsePostgresURL(v)
	}
	host := EnvOr("DB_HOST", "localhost")
	port := EnvOr("DB_PORT", "5432")
	user := EnvOr("DB_USER", "postgres")
	pass := os.Getenv("DB_PASSWORD")
	name := EnvOr("DB_NAME", "postgres")
	sslmode := EnvOr("DB_SSLMODE", "disable")
	if pass == "" {
		return fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s", host, port, user, name, sslmode)
	}
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host, port, user, pass, name, sslmode)
}

// parsePostgresURL manually parses postgres://user:pass@host:port/dbname
// into a keyword=value DSN. Does NOT use url.Parse because Saturn injects
// raw passwords with chars that break standard URL parsing ([], \, ?, ^, |).
//
// Format: postgres://USER:PASSWORD@HOST:PORT/DBNAME
// The password runs from the first ":" after "://" to the LAST "@" before host.
func parsePostgresURL(raw string) string {
	// Strip scheme
	s := raw
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(s, prefix) {
			s = s[len(prefix):]
			break
		}
	}

	// Find the last "@" — everything before is userinfo, after is host/db
	atIdx := strings.LastIndex(s, "@")
	if atIdx < 0 {
		return raw // malformed, let pgx try
	}
	userinfo := s[:atIdx]
	hostpath := s[atIdx+1:]

	// Parse user:password from userinfo (split on first ":")
	var user, pass string
	if colonIdx := strings.Index(userinfo, ":"); colonIdx >= 0 {
		user = userinfo[:colonIdx]
		pass = userinfo[colonIdx+1:]
	} else {
		user = userinfo
	}

	// URL-decode user and password — Saturn may percent-encode special chars
	if decoded, err := url.PathUnescape(user); err == nil {
		user = decoded
	}
	if decoded, err := url.PathUnescape(pass); err == nil {
		pass = decoded
	}

	// Parse host:port/dbname from hostpath
	// Strip query string if present (everything after "?")
	// But note: password might have had "?" — we already split on last "@" so hostpath is clean
	host := hostpath
	dbname := ""
	if slashIdx := strings.Index(host, "/"); slashIdx >= 0 {
		dbname = host[slashIdx+1:]
		host = host[:slashIdx]
	}
	// Strip query from dbname
	if qIdx := strings.Index(dbname, "?"); qIdx >= 0 {
		dbname = dbname[:qIdx]
	}

	port := "5432"
	if colonIdx := strings.LastIndex(host, ":"); colonIdx >= 0 {
		port = host[colonIdx+1:]
		host = host[:colonIdx]
	}

	sslmode := "disable"

	// Build DSN with single-quoted password to handle any special chars
	dsn := fmt.Sprintf("host=%s port=%s user=%s password='%s' dbname=%s sslmode=%s",
		host, port, user, escapeDSNValue(pass), dbname, sslmode)
	return dsn
}

// escapeDSNValue escapes single quotes and backslashes for libpq DSN values.
func escapeDSNValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// NatsURL returns the NATS connection URL.
// Saturn generates URLs with http:// and port 80, but NATS needs nats:// and port 4222.
func NatsURL() string {
	raw := EnvOr("NATS_URL", EnvOr("ORBIT_NATS_URL", "nats://localhost:4222"))
	// Convert http(s):// to nats://
	raw = strings.Replace(raw, "https://", "nats://", 1)
	raw = strings.Replace(raw, "http://", "nats://", 1)
	// Fix Saturn default port 80 → NATS port 4222
	raw = strings.Replace(raw, ":80", ":4222", 1)
	// Ensure nats:// prefix
	if !strings.HasPrefix(raw, "nats://") {
		raw = "nats://" + raw
	}
	return raw
}

// EnvDurationOr returns the duration value of the environment variable or the fallback.
func EnvDurationOr(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
