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

// TestReadSyncCoalescer_ResetCancelsPendingTimer covers the easy
// (non-racy) path: a Submit inside the debounce window successfully
// stops the in-flight timer, no stale callback is ever queued, and the
// final flush carries the latest payload.
func TestReadSyncCoalescer_ResetCancelsPendingTimer(t *testing.T) {
	var calls atomic.Int32
	c := newReadSyncCoalescer(50*time.Millisecond, func(_, _ string, _ []byte) {
		calls.Add(1)
	})
	defer c.Stop()

	c.Submit("u-1", "c-1", []byte("v1"))
	time.Sleep(40 * time.Millisecond) // before timer fires
	c.Submit("u-1", "c-1", []byte("v2"))

	time.Sleep(200 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 flush after Stop+rearm path, got %d", got)
	}
}

// TestReadSyncCoalescer_GenerationGuardsStaleQueuedCallback exercises the
// actual race the gen counter was added for: timer fires → fire()
// goroutine launched but blocks on c.mu → a Submit-like mutation happens
// under the lock first → when the queued callback eventually runs it must
// observe entry.gen has advanced and no-op, leaving the flush to the
// freshly-armed timer.
//
// The bug is "flush fires too EARLY", not "flush fires too many times":
// without the gen guard, the queued callback (which currently holds the
// new payload because Submit already mutated it) would delete the entry
// and flush immediately, collapsing the debounce window to ~0. With the
// guard, that callback no-ops and the freshly-armed timer carries the
// flush after the new debounce. So the assertion is on TIMING, not count.
//
// Driven deterministically by holding c.mu directly while the gen=1 timer
// expires, then performing the gen++/replace mutation under the lock.
func TestReadSyncCoalescer_GenerationGuardsStaleQueuedCallback(t *testing.T) {
	var (
		flushMu    sync.Mutex
		flushTimes []time.Time
	)
	c := newReadSyncCoalescer(50*time.Millisecond, func(_, _ string, _ []byte) {
		flushMu.Lock()
		flushTimes = append(flushTimes, time.Now())
		flushMu.Unlock()
	})
	defer c.Stop()

	const (
		key       = "u-1:c-1"
		newWindow = 60 * time.Millisecond
	)

	c.mu.Lock()

	// Arm a timer with a 1ms debounce so it expires while we hold the
	// lock — its callback is queued waiting on c.mu.
	entry := &readSyncEntry{
		userID:  "u-1",
		chatID:  "c-1",
		payload: []byte("v1"),
		gen:     1,
	}
	entry.timer = time.AfterFunc(1*time.Millisecond, func() { c.fire(key, 1) })
	c.pending[key] = entry

	time.Sleep(20 * time.Millisecond) // past expiry — callback now parked on c.mu

	// Mirror Submit's mutation. Stop returns false (timer already fired),
	// which is exactly the path the gen guard exists to handle.
	entry.gen = 2
	entry.payload = []byte("v2")
	entry.timer.Stop()
	entry.timer = time.AfterFunc(newWindow, func() { c.fire(key, 2) })

	unlockAt := time.Now()
	c.mu.Unlock()

	// Drain both possible callback paths (queued gen=1 + armed gen=2).
	time.Sleep(150 * time.Millisecond)

	flushMu.Lock()
	defer flushMu.Unlock()
	if len(flushTimes) != 1 {
		t.Fatalf("expected exactly 1 flush, got %d", len(flushTimes))
	}
	delay := flushTimes[0].Sub(unlockAt)
	// The gen-guarded path flushes after ~newWindow (60ms). Without the
	// guard, the queued gen=1 callback would acquire the lock immediately
	// after Unlock, see the entry with the new payload, delete and flush
	// in <5ms. We assert the flush is closer to the freshly-armed window
	// to differentiate the two implementations.
	//
	// Threshold is newWindow/4 rather than /2 to leave a 4× safety margin
	// against scheduler starvation on CI: even a goroutine delayed by ~10ms
	// before acquiring an uncontested mutex would still fail this assertion
	// in the un-guarded case.
	minExpected := newWindow / 4
	if delay < minExpected {
		t.Fatalf("flush happened too early: %v after unlock (expected >= %v) — "+
			"gen guard regression: stale queued callback flushed instead of new timer",
			delay, minExpected)
	}
}
