// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/pkg/apperror"
	"github.com/mst-corp/orbit/pkg/response"
)

type RateLimitConfig struct {
	Redis      *redis.Client
	MaxPerMin  int
	KeyPrefix  string
	Identifier func(*fiber.Ctx) string
}

func AuthRateLimitIdentifierByIP(c *fiber.Ctx) string {
	return "ip:" + c.IP()
}

// rateLimitScript atomically increments the counter and sets TTL on first hit.
// Returns [count, ttl_seconds]. This prevents the race where INCR succeeds but
// EXPIRE fails, leaving a key with no TTL (permanent rate-limit lock).
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`)

// RateLimitMiddleware implements a Redis-backed sliding window rate limiter.
func RateLimitMiddleware(cfg RateLimitConfig) fiber.Handler {
	windowSec := 60

	return func(c *fiber.Ctx) error {
		// Use IP for pre-auth routes. For post-auth routes, JWT middleware sets
		// a Fiber local (not a header) so clients cannot spoof another user's bucket.
		identifier := c.IP()
		if cfg.Identifier != nil {
			if customIdentifier := cfg.Identifier(c); customIdentifier != "" {
				identifier = customIdentifier
			}
		}
		if uid, ok := c.Locals("userID").(string); ok && uid != "" {
			identifier = uid
		}

		key := fmt.Sprintf("rl:%s:%s", cfg.KeyPrefix, identifier)
		ctx := c.Context()

		// Atomic INCR + EXPIRE via Lua — prevents race where key gets no TTL.
		result, err := rateLimitScript.Run(ctx, cfg.Redis, []string{key}, windowSec).Int64Slice()
		if err != nil {
			slog.Error("rate limiter Redis error, rejecting request", "error", err)
			return response.Error(c, apperror.Internal("Rate limiting unavailable"))
		}

		count := int(result[0])
		ttlSec := int(result[1])

		// Set rate limit headers
		c.Set("X-RateLimit-Limit", strconv.Itoa(cfg.MaxPerMin))
		remaining := cfg.MaxPerMin - count
		if remaining < 0 {
			remaining = 0
		}
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if count > cfg.MaxPerMin {
			if ttlSec > 0 {
				c.Set("Retry-After", strconv.Itoa(ttlSec))
			} else {
				// Safety: key has no TTL (shouldn't happen with Lua), set a sane default
				c.Set("Retry-After", strconv.Itoa(windowSec))
				if expErr := cfg.Redis.Expire(ctx, key, time.Duration(windowSec)*time.Second).Err(); expErr != nil {
					slog.Error("rate limiter: failed to set TTL, key may persist indefinitely", "key", key, "error", expErr)
				}
			}
			return response.Error(c, apperror.TooManyRequests("Rate limit exceeded"))
		}

		return c.Next()
	}
}
