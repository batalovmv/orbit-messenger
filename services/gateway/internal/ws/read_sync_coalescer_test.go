// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestReadSyncCoalescer_FlushesAfterDebounce verifies the simplest case: one
// Submit, wait past the debounce window, the flush callback fires once with
// the submitted payload.
func TestReadSyncCoalescer_FlushesAfterDebounce(t *testing.T) {
	var (
		mu       sync.Mutex
		received []string
	)
	c := newReadSyncCoalescer(40*time.Millisecond, func(uid, cid string, payload []byte) {
		mu.Lock()
		received = append(received, uid+":"+cid+":"+string(payload))
		mu.Unlock()
	})
	defer c.Stop()

	c.Submit("u-1", "c-1", []byte("v1"))

	// Wait past the debounce + a small slack for the timer goroutine to run.
	time.Sleep(120 * time.Millisecond)

	mu.Lock()
	got := append([]string(nil), received...)
	mu.Unlock()

	if len(got) != 1 || got[0] != "u-1:c-1:v1" {
		t.Fatalf("expected one flush u-1:c-1:v1, got %v", got)
	}
}

// TestReadSyncCoalescer_CoalescesBurst verifies that a burst of submits for
// the SAME (user, chat) within the debounce window collapses to ONE flush
// with the LATEST payload.
func TestReadSyncCoalescer_CoalescesBurst(t *testing.T) {
	var calls atomic.Int32
	var mu sync.Mutex
	var lastPayload string
	c := newReadSyncCoalescer(60*time.Millisecond, func(_, _ string, payload []byte) {
		calls.Add(1)
		mu.Lock()
		lastPayload = string(payload)
		mu.Unlock()
	})
	defer c.Stop()

	// 5 rapid submits — all within the debounce window, each resets the timer.
	for _, p := range []string{"v1", "v2", "v3", "v4", "v5"} {
		c.Submit("u-1", "c-1", []byte(p))
		time.Sleep(15 * time.Millisecond)
	}

	// Wait for the final debounce to elapse.
	time.Sleep(150 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 flush after coalesced burst, got %d", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastPayload != "v5" {
		t.Fatalf("expected latest payload v5 to win coalesce, got %q", lastPayload)
	}
}

// TestReadSyncCoalescer_DistinctKeysFlushIndependently asserts that two
// different (user, chat) pairs don't collide — both get their own flush.
func TestReadSyncCoalescer_DistinctKeysFlushIndependently(t *testing.T) {
	var calls atomic.Int32
	c := newReadSyncCoalescer(40*time.Millisecond, func(_, _ string, _ []byte) {
		calls.Add(1)
	})
	defer c.Stop()

	c.Submit("u-1", "c-1", []byte("a"))
	c.Submit("u-1", "c-2", []byte("b")) // same user, different chat
	c.Submit("u-2", "c-1", []byte("c")) // different user, same chat as first

	time.Sleep(150 * time.Millisecond)

	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 independent flushes, got %d", got)
	}
}

// TestReadSyncCoalescer_StopCancelsPending guards against a goroutine leak
// at shutdown: pending entries are dropped, no flush fires.
func TestReadSyncCoalescer_StopCancelsPending(t *testing.T) {
	var calls atomic.Int32
	c := newReadSyncCoalescer(80*time.Millisecond, func(_, _ string, _ []byte) {
		calls.Add(1)
	})

	c.Submit("u-1", "c-1", []byte("a"))
	c.Submit("u-2", "c-2", []byte("b"))

	c.Stop()

	// If Stop didn't cancel, this wait would let the timers fire.
	time.Sleep(150 * time.Millisecond)

	if got := calls.Load(); got != 0 {
		t.Fatalf("expected 0 flushes after Stop, got %d", got)
	}
}

// TestReadSyncCoalescer_SubmitAfterStopIsNoop_NoPanic spell-checks that the
// coalescer does not panic if another submit races in after Stop is called
// (e.g. a NATS callback in flight at shutdown). The contract is "at-most-once
// flush per submit"; missed submits at shutdown are acceptable.
func TestReadSyncCoalescer_SubmitAfterStopIsNoop_NoPanic(t *testing.T) {
	c := newReadSyncCoalescer(40*time.Millisecond, func(_, _ string, _ []byte) {})
	c.Stop()

	// Must not panic — pending map was reset to fresh empty by Stop, so
	// Submit may register a new entry whose timer fires harmlessly.
	c.Submit("u-1", "c-1", []byte("v"))
	time.Sleep(120 * time.Millisecond)
}
