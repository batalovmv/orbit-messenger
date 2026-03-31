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
}

// RateLimitMiddleware implements a Redis-backed sliding window rate limiter.
func RateLimitMiddleware(cfg RateLimitConfig) fiber.Handler {
	window := 60 * time.Second

	return func(c *fiber.Ctx) error {
		// Use IP for rate limiting — X-User-ID is not trustworthy at this point
		// because it comes from the raw request before JWT middleware validates it.
		identifier := c.IP()
		// If JWT middleware has already set a verified user ID (via c.Locals), use it.
		if uid, ok := c.Locals("user_id").(string); ok && uid != "" {
			identifier = uid
		}

		key := fmt.Sprintf("rl:%s:%s", cfg.KeyPrefix, identifier)
		ctx := c.Context()

		// Use pipeline to make INCR+EXPIRE atomic (avoids orphan keys without TTL)
		pipe := cfg.Redis.Pipeline()
		incrCmd := pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, window)
		_, err := pipe.Exec(ctx)
		if err != nil {
			// Fail-closed: reject requests when Redis is unavailable
			slog.Error("rate limiter Redis error, rejecting request", "error", err)
			return response.Error(c, apperror.Internal("Rate limiting unavailable"))
		}
		count := incrCmd.Val()

		// Set rate limit headers
		c.Set("X-RateLimit-Limit", strconv.Itoa(cfg.MaxPerMin))
		remaining := cfg.MaxPerMin - int(count)
		if remaining < 0 {
			remaining = 0
		}
		c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

		if int(count) > cfg.MaxPerMin {
			ttl, _ := cfg.Redis.TTL(ctx, key).Result()
			c.Set("Retry-After", strconv.Itoa(int(ttl.Seconds())))
			return response.Error(c, apperror.TooManyRequests("Rate limit exceeded"))
		}

		return c.Next()
	}
}
