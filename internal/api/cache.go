package api

import (
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// ttlCache keeps computed answers for a while (H-1B data changes only when
// a new disclosure file is imported). It holds at most max entries;
// when full, expired entries go first, then everything. Concurrent misses
// for one key share a single computation.
type ttlCache struct {
	group singleflight.Group
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	data  map[string]cacheEntry
}

type cacheEntry struct {
	value any
	until time.Time
}

func newTTLCache(ttl time.Duration, capacity int) *ttlCache {
	return &ttlCache{ttl: ttl, max: capacity, data: map[string]cacheEntry{}}
}

// get returns the cached value for key, or computes, stores and returns it
// (compute must not depend on one caller's cancellation: others may wait on it).
// Errors are not cached.
func (c *ttlCache) get(key string, compute func() (any, error)) (any, error) {
	now := time.Now()
	c.mu.Lock()
	if e, ok := c.data[key]; ok && now.Before(e.until) {
		c.mu.Unlock()
		return e.value, nil
	}
	c.mu.Unlock()
	v, err, _ := c.group.Do(key, func() (any, error) {
		v, err := compute()
		if err == nil {
			c.put(key, v)
		}
		return v, err
	})
	return v, err
}

func (c *ttlCache) put(key string, v any) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) >= c.max {
		for k, e := range c.data {
			if now.After(e.until) {
				delete(c.data, k)
			}
		}
		if len(c.data) >= c.max {
			c.data = map[string]cacheEntry{}
		}
	}
	c.data[key] = cacheEntry{value: v, until: now.Add(c.ttl)}
}
