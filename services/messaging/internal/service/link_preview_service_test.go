package service

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestLinkPreviewRateLimit_FirstHitSetsTTLWithoutResettingWindow(t *testing.T) {
	mr := newMiniredis(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	svc := NewLinkPreviewService(rdb, slog.Default())

	ctx := context.Background()
	key := "ratelimit:linkpreview:user-1"

	if !svc.CheckRateLimit(ctx, "user-1") {
		t.Fatal("expected first request to pass")
	}

	firstTTL, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("read first ttl: %v", err)
	}
	if firstTTL <= 0 || firstTTL > 60*time.Second {
		t.Fatalf("expected ttl within (0,60s], got %s", firstTTL)
	}

	mr.FastForward(10 * time.Second)

	if !svc.CheckRateLimit(ctx, "user-1") {
		t.Fatal("expected second request to pass")
	}

	secondTTL, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("read second ttl: %v", err)
	}
	if secondTTL <= 0 {
		t.Fatalf("expected ttl to remain set, got %s", secondTTL)
	}
	if secondTTL >= firstTTL {
		t.Fatalf("expected ttl window to keep decaying, got first=%s second=%s", firstTTL, secondTTL)
	}
}
