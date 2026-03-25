package config

import (
	"fmt"
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

// DatabaseURL returns DATABASE_URL if set, otherwise builds it from DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME.
func DatabaseURL() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		// Ensure sslmode=disable for internal Docker networks
		if !strings.Contains(v, "sslmode=") {
			if strings.Contains(v, "?") {
				v += "&sslmode=disable"
			} else {
				v += "?sslmode=disable"
			}
		}
		return v
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
