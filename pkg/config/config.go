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

// DatabaseURL returns a pgx-compatible DSN string.
// If DATABASE_URL is set (typically a postgres:// URL), it parses it and converts
// to keyword=value DSN format. This correctly handles URL-encoded passwords with
// special characters (@, #, %, etc.) that break pgx URL parsing.
func DatabaseURL() string {
	if raw := os.Getenv("DATABASE_URL"); raw != "" {
		return postgresURLToDSN(raw)
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

// postgresURLToDSN converts a postgres:// URL to keyword=value DSN format.
// This avoids issues with URL-encoded special characters in passwords.
func postgresURLToDSN(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		// Fallback: return as-is and let pgx try
		return raw
	}

	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "5432"
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	dbname := strings.TrimPrefix(u.Path, "/")

	// Extract sslmode from query params, default to disable
	sslmode := u.Query().Get("sslmode")
	if sslmode == "" {
		sslmode = "disable"
	}

	dsn := fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s", host, port, user, dbname, sslmode)
	if pass != "" {
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host, port, user, pass, dbname, sslmode)
	}
	return dsn
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
