package base

import (
	"sync"
	"time"
)

type ttlDedupeCache struct {
	mu    sync.Mutex
	items map[string]time.Time
}

func newTTLDedupeCache() *ttlDedupeCache {
	return &ttlDedupeCache{
		items: make(map[string]time.Time),
	}
}

func (c *ttlDedupeCache) hitOrStore(key string, ttl time.Duration, now time.Time) (hit bool, age time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for k, exp := range c.items {
		if now.After(exp) {
			delete(c.items, k)
		}
	}

	if exp, ok := c.items[key]; ok && now.Before(exp) {
		age = ttl - exp.Sub(now)
		if age < 0 {
			age = 0
		}
		return true, age
	}

	c.items[key] = now.Add(ttl)
	return false, 0
}
