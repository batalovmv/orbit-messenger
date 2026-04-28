// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// signTestJWT mints a token signed with a throwaway HS256 key. The middleware
// only reads claims via ParseUnverified, so the signing key never has to match
// anything in production — the token just needs to parse cleanly.
func signTestJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte("test-key"))
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return signed
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// TestJWTMiddleware_TokensInvalidatedByReset_RejectsCachedToken is the locked
// test for the ResetAdmin cache gap: a token whose iat predates the per-user
// threshold must NOT pass even when its cache entry is still warm.
func TestJWTMiddleware_TokensInvalidatedByReset_RejectsCachedToken(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	userID := uuid.NewString()
	// Old iat — predates the reset threshold below.
	token := signTestJWT(t, jwt.MapClaims{
		"sub": userID,
		"jti": uuid.NewString(),
		"iat": float64(1_000_000_000),
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	cacheKey := "jwt_cache:" + sha256Hex(token)
	cuJSON, _ := json.Marshal(cachedUser{ID: userID, Role: "member"})
	if err := rdb.Set(t.Context(), cacheKey, string(cuJSON), 30*time.Second).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	// Threshold AFTER token's iat → token must be rejected even on cache hit.
	threshold := fmt.Sprintf("%d", time.Now().Unix())
	if err := rdb.Set(t.Context(), "user_tokens_invalid_before:"+userID, threshold, time.Hour).Err(); err != nil {
		t.Fatalf("seed threshold: %v", err)
	}

	app := fiber.New()
	app.Use(JWTMiddleware(JWTConfig{Redis: rdb, CacheTTL: 30 * time.Second}))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest(fiber.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("expected 401 after ResetAdmin, got %d", resp.StatusCode)
	}
	// Cache entry must be evicted so a subsequent request also re-validates.
	if exists, _ := rdb.Exists(t.Context(), cacheKey).Result(); exists != 0 {
		t.Fatalf("expected cache entry to be deleted after revoke, still present")
	}
}

// TestJWTMiddleware_TokensInvalidatedByReset_AllowsFreshToken covers the
// inverse: a token issued AFTER the threshold (e.g. logged back in post-reset)
// must pass through the cache-hit path without 401-ing the user.
func TestJWTMiddleware_TokensInvalidatedByReset_AllowsFreshToken(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	userID := uuid.NewString()
	now := time.Now().Unix()
	token := signTestJWT(t, jwt.MapClaims{
		"sub": userID,
		"jti": uuid.NewString(),
		"iat": float64(now),
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	cacheKey := "jwt_cache:" + sha256Hex(token)
	cuJSON, _ := json.Marshal(cachedUser{ID: userID, Role: "member"})
	if err := rdb.Set(t.Context(), cacheKey, string(cuJSON), 30*time.Second).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	// Old reset threshold; token is newer.
	if err := rdb.Set(t.Context(), "user_tokens_invalid_before:"+userID, fmt.Sprintf("%d", now-3600), time.Hour).Err(); err != nil {
		t.Fatalf("seed threshold: %v", err)
	}

	app := fiber.New()
	app.Use(JWTMiddleware(JWTConfig{Redis: rdb, CacheTTL: 30 * time.Second}))
	app.Get("/test", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusOK) })

	req := httptest.NewRequest(fiber.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("expected 200 for post-reset-issued token, got %d", resp.StatusCode)
	}
}
