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

// DatabaseDSN returns a keyword=value DSN (without password) and the raw password separately.
// The password is returned separately so callers can set it programmatically on pgx config,
// avoiding all DSN escaping issues with special characters.
// rawPassword is the URL-encoded password as-is from DATABASE_URL (before decoding).
func DatabaseDSN() (dsn string, password string, rawPassword string) {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		dsn, password, rawPassword = parsePostgresURL(v)
		// DB_PASSWORD overrides the password from DATABASE_URL if set
		if override := os.Getenv("DB_PASSWORD"); override != "" {
			password = override
			rawPassword = override
		}
		return dsn, password, rawPassword
	}
	host := EnvOr("DB_HOST", "localhost")
	port := EnvOr("DB_PORT", "5432")
	user := EnvOr("DB_USER", "postgres")
	pass := os.Getenv("DB_PASSWORD")
	name := EnvOr("DB_NAME", "postgres")
	sslmode := EnvOr("DB_SSLMODE", "disable")
	dsn = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s", host, port, user, name, sslmode)
	return dsn, pass, pass
}

// parsePostgresURL manually parses postgres://user:pass@host:port/dbname.
// Returns DSN without password, and the raw decoded password separately.
//
// Manual parsing is required because Saturn injects passwords with special chars
// ([], \, ?, ^, |) that may or may not be URL-encoded, breaking Go's url.Parse.
// Strategy: split on last "@" for host, first ":" in userinfo for user:pass.
func parsePostgresURL(raw string) (dsn string, password string, rawPassword string) {
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
		return raw, "", "" // malformed, let pgx try
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

	// Keep raw (URL-encoded) password before decoding
	rawPass := pass

	// URL-decode user and password — Saturn percent-encodes special chars
	if decoded, err := url.PathUnescape(user); err == nil {
		user = decoded
	}
	if decoded, err := url.PathUnescape(pass); err == nil {
		pass = decoded
	}

	// Parse host:port/dbname from hostpath
	host := hostpath
	dbname := ""
	if slashIdx := strings.Index(host, "/"); slashIdx >= 0 {
		dbname = host[slashIdx+1:]
		host = host[:slashIdx]
	}
	if qIdx := strings.Index(dbname, "?"); qIdx >= 0 {
		dbname = dbname[:qIdx]
	}

	port := "5432"
	if colonIdx := strings.LastIndex(host, ":"); colonIdx >= 0 {
		port = host[colonIdx+1:]
		host = host[:colonIdx]
	}

	dsn = fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=disable", host, port, user, dbname)
	return dsn, pass, rawPass
}

// NatsURL returns the NATS connection URL.
// Saturn generates URLs with http:// and port 80, but NATS needs nats:// and port 4222.
func NatsURL() string {
	raw := EnvOr("ORBIT_NATS_URL", "nats://localhost:4222")
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
