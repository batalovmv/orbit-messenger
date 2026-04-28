// Copyright (C) 2024 MST Corp. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package ws

import (
	"sync"
	"time"
)

// readSyncCoalescer debounces and coalesces silent read-sync push fanouts.
//
// Without it, a user who scrolls quickly through 10 unread chats with
// notifications would generate 10 silent pushes per offline device — well
// inside iOS's APNs throttle window. With debounce + per-key coalescing, the
// last update for a given (userID, chatID) wins and only one push goes out.
//
// Submit() is non-blocking. The flush callback runs on a goroutine spawned by
// time.AfterFunc, so callers must accept that flushes happen asynchronously
// after the debounce window. Stop() cancels every pending timer; in-flight
// flush callbacks are NOT cancelled — they observe the cancellation by
// finding their entry already gone and returning without work.
type readSyncCoalescer struct {
	mu       sync.Mutex
	pending  map[string]*readSyncEntry
	debounce time.Duration
	flush    func(userID, chatID string, payload []byte)
}

type readSyncEntry struct {
	userID  string
	chatID  string
	payload []byte
	timer   *time.Timer
	// gen is incremented by every Submit that resets the timer. The fire()
	// callback captures its expected generation when the timer is armed and
	// only flushes if it still matches; a stale callback (race: timer fired
	// → callback queued → Submit acquired the lock and Reset before the
	// callback ran) finds gen has advanced, no-ops, and lets the rescheduled
	// timer run the actual debounce window.
	gen uint64
}

// newReadSyncCoalescer constructs a coalescer with the given debounce window
// and flush callback. flush is invoked once per (userID, chatID) after the
// debounce expires with no further updates for that key.
func newReadSyncCoalescer(debounce time.Duration, flush func(userID, chatID string, payload []byte)) *readSyncCoalescer {
	return &readSyncCoalescer{
		pending:  make(map[string]*readSyncEntry),
		debounce: debounce,
		flush:    flush,
	}
}

// Submit registers a pending read-sync. If a previous submit for the same
// (userID, chatID) is still within the debounce window, the payload is
// replaced with the newer one and the timer is reset — the newest read state
// always wins.
func (c *readSyncCoalescer) Submit(userID, chatID string, payload []byte) {
	if userID == "" || chatID == "" || len(payload) == 0 {
		return
	}
	key := userID + ":" + chatID

	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.pending[key]; ok {
		existing.payload = payload
		existing.gen++
		expected := existing.gen
		// Stop the old timer and replace with a fresh one carrying the new
		// generation. Stop returns false if the timer already fired and
		// the callback is queued waiting on this lock; without the gen
		// check below, that stale callback would acquire the lock after
		// we release it and flush the new payload immediately, collapsing
		// the debounce. The new timer with expected==entry.gen is the only
		// path that should successfully flush.
		existing.timer.Stop()
		existing.timer = time.AfterFunc(c.debounce, func() { c.fire(key, expected) })
		return
	}

	entry := &readSyncEntry{
		userID:  userID,
		chatID:  chatID,
		payload: payload,
		gen:     1,
	}
	entry.timer = time.AfterFunc(c.debounce, func() { c.fire(key, 1) })
	c.pending[key] = entry
}

func (c *readSyncCoalescer) fire(key string, expectedGen uint64) {
	c.mu.Lock()
	entry, ok := c.pending[key]
	if !ok || entry.gen != expectedGen {
		// Either Stop drained the map, or a Submit ran after the timer fired
		// and re-armed; the freshly-armed timer carries the right generation.
		c.mu.Unlock()
		return
	}
	delete(c.pending, key)
	userID, chatID, payload := entry.userID, entry.chatID, entry.payload
	c.mu.Unlock()

	if c.flush != nil {
		c.flush(userID, chatID, payload)
	}
}

// Stop cancels every pending timer. Safe to call multiple times. Does not
// flush pending entries — at shutdown, dropping a few silent notifications is
// acceptable; the next foreground reconciliation on the offline device will
// purge any stale banners via the existing /chats sync path.
func (c *readSyncCoalescer) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.pending {
		e.timer.Stop()
	}
	c.pending = make(map[string]*readSyncEntry)
}
