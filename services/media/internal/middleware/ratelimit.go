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

// rateLimitScript atomically increments the counter and sets TTL on first hit.
// Returns [count, ttl_seconds]. Prevents race where INCR succeeds but EXPIRE
// fails, leaving a key with no TTL (permanent rate-limit lock).
var rateLimitScript = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then
  redis.call('EXPIRE', KEYS[1], ARGV[1])
end
local ttl = redis.call('TTL', KEYS[1])
return {count, ttl}
`)

// UserUploadRateLimit returns a per-user rate-limit middleware keyed on X-User-ID.
// maxPerMin is the maximum number of requests allowed per minute.
// Fail-closed: Redis errors result in 500 (request rejected).
func UserUploadRateLimit(rdb *redis.Client, maxPerMin int) fiber.Handler {
	windowSec := 60

	return func(c *fiber.Ctx) error {
		userID := c.Get("X-User-ID")
		if userID == "" {
			// No user ID — let downstream auth middleware handle it.
			return c.Next()
		}

		key := fmt.Sprintf("ratelimit:media:upload:user:%s", userID)
		ctx := c.Context()

		result, err := rateLimitScript.Run(ctx, rdb, []string{key}, windowSec).Int64Slice()
		if err != nil {
			slog.Error("media upload rate limiter Redis error, rejecting request", "error", err)
			return response.Error(c, apperror.Internal("Rate limiting unavailable"))
		}

		count := int(result[0])
		ttlSec := int(result[1])

		c.Set("X-RateLimit-Limit", strconv.Itoa(maxPerMin))
		remaining := maxPerMin - count
		if remaining < 0 {
			remaining = 0
		}
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if count > maxPerMin {
			if ttlSec > 0 {
				c.Set("Retry-After", strconv.Itoa(ttlSec))
			} else {
				c.Set("Retry-After", strconv.Itoa(windowSec))
				if expErr := rdb.Expire(ctx, key, time.Duration(windowSec)*time.Second).Err(); expErr != nil {
					slog.Error("media rate limiter: failed to set TTL", "key", key, "error", expErr)
				}
			}
			return response.Error(c, apperror.TooManyRequests("Rate limit exceeded"))
		}

		return c.Next()
	}
}
