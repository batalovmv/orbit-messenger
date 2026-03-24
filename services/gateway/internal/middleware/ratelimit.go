package middleware

import (
	"fmt"
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
		// Use user ID if available, otherwise use IP
		identifier := c.Get("X-User-ID")
		if identifier == "" {
			identifier = c.IP()
		}

		key := fmt.Sprintf("rl:%s:%s", cfg.KeyPrefix, identifier)
		ctx := c.Context()

		count, err := cfg.Redis.Incr(ctx, key).Result()
		if err != nil {
			// If Redis is down, allow the request
			return c.Next()
		}

		if count == 1 {
			cfg.Redis.Expire(ctx, key, window)
		}

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
