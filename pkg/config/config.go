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

// DatabaseURL returns a DSN connection string for pgx.
// Parses DATABASE_URL (URL format) and converts to key=value DSN to avoid URL-encoding issues.
func DatabaseURL() string {
	raw := os.Getenv("DATABASE_URL")
	if raw != "" {
		// Parse URL format: postgresql://user:pass@host:port/dbname
		u, err := url.Parse(raw)
		if err == nil && u.Host != "" {
			host := u.Hostname()
			port := u.Port()
			if port == "" {
				port = "5432"
			}
			user := u.User.Username()
			pass, _ := u.User.Password()
			dbname := strings.TrimPrefix(u.Path, "/")
			if dbname == "" {
				dbname = "postgres"
			}
			// Get sslmode from query params, default to disable
			sslmode := u.Query().Get("sslmode")
			if sslmode == "" {
				sslmode = "disable"
			}
			if pass == "" {
				return fmt.Sprintf("host=%s port=%s user=%s dbname=%s sslmode=%s", host, port, user, dbname, sslmode)
			}
			return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s", host, port, user, pass, dbname, sslmode)
		}
		// If parse failed, use as-is with sslmode
		if !strings.Contains(raw, "sslmode=") {
			if strings.Contains(raw, "?") {
				raw += "&sslmode=disable"
			} else {
				raw += "?sslmode=disable"
			}
		}
		return raw
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
