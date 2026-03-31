package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/gateway/internal/middleware"
)

// setupRateLimitApp creates a Fiber app with rate limiting backed by miniredis.
func setupRateLimitApp(t *testing.T, maxPerMin int, keyPrefix string) *fiber.App {
	t.Helper()

	mr := miniredis.RunT(t)

	rdb := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	t.Cleanup(func() {
		rdb.Close()
	})

	app := fiber.New()
	// Simulate JWT middleware setting user_id in Locals from X-User-ID header (for test convenience)
	app.Use(func(c *fiber.Ctx) error {
		if uid := c.Get("X-User-ID"); uid != "" {
			c.Locals("user_id", uid)
		}
		return c.Next()
	})
	app.Use(middleware.RateLimitMiddleware(middleware.RateLimitConfig{
		Redis:     rdb,
		MaxPerMin: maxPerMin,
		KeyPrefix: keyPrefix,
	}))
	app.Get("/test", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	return app
}

// doRequest sends a GET /test with an optional X-User-ID header.
func doRequest(app *fiber.App, userID string) *http.Response {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	resp, _ := app.Test(req, -1)
	return resp
}

// TestRateLimit_AllowsUnderLimit verifies that exactly MaxPerMin requests all succeed.
func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	const limit = 5
	app := setupRateLimitApp(t, limit, "test_under")

	for i := 0; i < limit; i++ {
		resp := doRequest(app, "user-1")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}
}

// TestRateLimit_BlocksOverLimit verifies the (MaxPerMin+1)th request is rejected with 429.
func TestRateLimit_BlocksOverLimit(t *testing.T) {
	const limit = 5
	app := setupRateLimitApp(t, limit, "test_over")

	for i := 0; i < limit; i++ {
		resp := doRequest(app, "user-2")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRequest(app, "user-2")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on request %d, got %d", limit+1, resp.StatusCode)
	}
}

// TestRateLimit_ReturnsRetryAfter verifies that a blocked response includes the Retry-After header.
func TestRateLimit_ReturnsRetryAfter(t *testing.T) {
	const limit = 2
	app := setupRateLimitApp(t, limit, "test_retry")

	for i := 0; i <= limit; i++ {
		doRequest(app, "user-3")
	}

	resp := doRequest(app, "user-3")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}

	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header to be set on 429 response")
	}

	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Retry-After header is not a valid integer: %q", retryAfter)
	}
	if seconds <= 0 || seconds > 60 {
		t.Fatalf("Retry-After value %d out of expected range (1-60)", seconds)
	}
}

// TestRateLimit_DifferentKeys verifies that different users have independent rate limit counters.
func TestRateLimit_DifferentKeys(t *testing.T) {
	const limit = 3
	app := setupRateLimitApp(t, limit, "test_keys")

	for i := 0; i < limit; i++ {
		doRequest(app, "user-A")
	}
	blocked := doRequest(app, "user-A")
	if blocked.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("user-A: expected 429 after exhausting limit, got %d", blocked.StatusCode)
	}

	for i := 0; i < limit; i++ {
		resp := doRequest(app, "user-B")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("user-B request %d: expected 200, got %d", i+1, resp.StatusCode)
		}
	}
}
