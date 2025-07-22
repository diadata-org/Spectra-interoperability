package processor

import (
	"context"
	"sync"
	"time"
)

// DedupCache is an in-memory cache for deduplication
type DedupCache struct {
	mu       sync.RWMutex
	items    map[string]*cacheItem
	maxSize  int
	ttl      time.Duration
}

type cacheItem struct {
	addedAt time.Time
}

// NewDedupCache creates a new deduplication cache
func NewDedupCache(maxSize int, ttl time.Duration) *DedupCache {
	return &DedupCache{
		items:   make(map[string]*cacheItem),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// Add adds an item to the cache
func (c *DedupCache) Add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check size limit
	if len(c.items) >= c.maxSize {
		// Remove oldest item
		c.evictOldest()
	}

	c.items[key] = &cacheItem{
		addedAt: time.Now(),
	}
}

// Has checks if an item exists in the cache
func (c *DedupCache) Has(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	item, exists := c.items[key]
	if !exists {
		return false
	}

	// Check if expired
	if time.Since(item.addedAt) > c.ttl {
		return false
	}

	return true
}

// Size returns the current cache size
func (c *DedupCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Clear removes all items from the cache
func (c *DedupCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*cacheItem)
}

// StartCleaner starts a background goroutine to clean expired items
func (c *DedupCache) StartCleaner(ctx context.Context) {
	ticker := time.NewTicker(c.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanExpired()
		}
	}
}

// cleanExpired removes expired items from the cache
func (c *DedupCache) cleanExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, item := range c.items {
		if now.Sub(item.addedAt) > c.ttl {
			delete(c.items, key)
		}
	}
}

// evictOldest removes the oldest item from the cache
func (c *DedupCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, item := range c.items {
		if oldestKey == "" || item.addedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = item.addedAt
		}
	}

	if oldestKey != "" {
		delete(c.items, oldestKey)
	}
}