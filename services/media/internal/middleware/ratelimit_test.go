package middleware_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"

	"github.com/mst-corp/orbit/services/media/internal/middleware"
)

// newTestRedis starts an in-process miniredis and returns a connected client.
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

// testApp builds a minimal Fiber app with the rate-limit middleware on GET /upload.
func testApp(rdb *redis.Client, maxPerMin int) *fiber.App {
	app := fiber.New()
	app.Use(middleware.UserUploadRateLimit(rdb, maxPerMin))
	app.Get("/upload", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	return app
}

// doRequest issues a GET /upload with the given userID header.
func doRequest(app *fiber.App, userID string) *http.Response {
	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	resp, _ := app.Test(req, -1)
	return resp
}

// drainBody reads and discards the response body so the connection can be reused.
func drainBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// TestUserUploadRateLimit_AllowsUpToLimit verifies that exactly maxPerMin
// requests from the same user succeed, and the (maxPerMin+1)-th returns 429.
func TestUserUploadRateLimit_AllowsUpToLimit(t *testing.T) {
	const maxPerMin = 5
	rdb := newTestRedis(t)
	app := testApp(rdb, maxPerMin)
	userID := "user-aaa"

	for i := 1; i <= maxPerMin; i++ {
		resp := doRequest(app, userID)
		drainBody(resp)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
	}

	// (maxPerMin+1)-th must be rejected
	resp := doRequest(app, userID)
	drainBody(resp)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on request %d, got %d", maxPerMin+1, resp.StatusCode)
	}
}

// TestUserUploadRateLimit_DifferentUsersIndependent verifies that rate-limit
// buckets are per-user: maxPerMin requests from N different users all succeed.
func TestUserUploadRateLimit_DifferentUsersIndependent(t *testing.T) {
	const maxPerMin = 5
	const numUsers = 3
	rdb := newTestRedis(t)
	app := testApp(rdb, maxPerMin)

	for u := 0; u < numUsers; u++ {
		userID := fmt.Sprintf("user-%d", u)
		for i := 1; i <= maxPerMin; i++ {
			resp := doRequest(app, userID)
			drainBody(resp)
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("user %s request %d: expected 200, got %d", userID, i, resp.StatusCode)
			}
		}
	}
}

// TestUserUploadRateLimit_NoUserID verifies that requests without X-User-ID
// are passed through (downstream auth middleware handles them).
func TestUserUploadRateLimit_NoUserID(t *testing.T) {
	rdb := newTestRedis(t)
	app := testApp(rdb, 5)

	resp := doRequest(app, "")
	drainBody(resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for no-user-id request, got %d", resp.StatusCode)
	}
}

// TestUserUploadRateLimit_HeadersSet verifies that X-RateLimit-* headers are
// present on allowed requests.
func TestUserUploadRateLimit_HeadersSet(t *testing.T) {
	const maxPerMin = 10
	rdb := newTestRedis(t)
	app := testApp(rdb, maxPerMin)
	userID := "user-bbb"

	resp := doRequest(app, userID)
	drainBody(resp)

	if resp.Header.Get("X-RateLimit-Limit") != fmt.Sprintf("%d", maxPerMin) {
		t.Errorf("X-RateLimit-Limit: got %q, want %d", resp.Header.Get("X-RateLimit-Limit"), maxPerMin)
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "" {
		t.Error("X-RateLimit-Remaining header missing")
	}
}
