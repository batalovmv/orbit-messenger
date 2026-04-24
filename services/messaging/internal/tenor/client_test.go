// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package tenor

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMiniredis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)
	return mr
}

func TestCheckRateLimit_FirstHitSetsTTLWithoutResettingWindow(t *testing.T) {
	mr := newMiniredis(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	client := NewClient("tenor-key", rdb, slog.Default())

	ctx := context.Background()
	key := "ratelimit:tenor:" + time.Now().UTC().Format("200601021504")

	if err := client.checkRateLimit(ctx); err != nil {
		t.Fatalf("first rate-limit check failed: %v", err)
	}

	firstTTL, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("read first ttl: %v", err)
	}
	if firstTTL <= 0 || firstTTL > rateLimitWindow {
		t.Fatalf("expected ttl within (0,%s], got %s", rateLimitWindow, firstTTL)
	}

	mr.FastForward(10 * time.Second)

	if err := client.checkRateLimit(ctx); err != nil {
		t.Fatalf("second rate-limit check failed: %v", err)
	}

	secondTTL, err := rdb.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("read second ttl: %v", err)
	}
	if secondTTL <= 0 {
		t.Fatalf("expected ttl to remain set, got %s", secondTTL)
	}
	if secondTTL >= firstTTL {
		t.Fatalf("expected ttl to keep decaying, got first=%s second=%s", firstTTL, secondTTL)
	}
}
