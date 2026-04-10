package ws

import (
	"container/list"
	"sync"
)

// dedupCache is a thread-safe fixed-capacity LRU set for deduplicating event IDs.
// It stores the most recently seen eventIDs and evicts the oldest when full.
type dedupCache struct {
	mu       sync.Mutex
	cap      int
	index    map[string]*list.Element // key → list element
	lru      *list.List               // front = most recent
}

func newDedupCache(capacity int) *dedupCache {
	return &dedupCache{
		cap:   capacity,
		index: make(map[string]*list.Element, capacity),
		lru:   list.New(),
	}
}

// seen returns true if the key has been seen before, false if it is new.
// New keys are recorded; duplicate keys return true without modifying state.
func (c *dedupCache) seen(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.index[key]; ok {
		// Move to front (most recently used).
		c.lru.MoveToFront(el)
		return true
	}

	// New key — record it.
	el := c.lru.PushFront(key)
	c.index[key] = el

	// Evict the oldest entry if over capacity.
	if c.lru.Len() > c.cap {
		oldest := c.lru.Back()
		if oldest != nil {
			c.lru.Remove(oldest)
			delete(c.index, oldest.Value.(string))
		}
	}

	return false
}
