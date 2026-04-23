package cache

import (
	"container/list"
	"sync"
	"time"
)

// Cache is an in-memory cache with TTL support and LRU eviction.
type Cache[K comparable, V any] struct {
	items    map[K]*cacheItem[V]
	order    *list.List
	orderMap map[K]*list.Element
	mu       sync.RWMutex
	maxSize  int
	ttl      time.Duration
	cleanup  *time.Ticker
	done     chan struct{}
}

type cacheItem[V any] struct {
	value     V
	expiresAt time.Time
}

// Config holds cache configuration.
type Config struct {
	// MaxSize is the maximum number of items in the cache.
	MaxSize int
	// TTL is the time-to-live for cache items.
	TTL time.Duration
	// CleanupInterval is how often to run the cleanup goroutine.
	CleanupInterval time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxSize:         1000,
		TTL:             5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}
}

// New creates a new cache with the given configuration.
func New[K comparable, V any](cfg Config) *Cache[K, V] {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 1000
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 5 * time.Minute
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 1 * time.Minute
	}

	c := &Cache[K, V]{
		items:    make(map[K]*cacheItem[V]),
		order:    list.New(),
		orderMap: make(map[K]*list.Element),
		maxSize:  cfg.MaxSize,
		ttl:      cfg.TTL,
		cleanup:  time.NewTicker(cfg.CleanupInterval),
		done:     make(chan struct{}),
	}

	go c.cleanupLoop()
	return c
}

// Get retrieves an item from the cache.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	item, ok := c.items[key]
	c.mu.RUnlock()

	if !ok {
		var zero V
		return zero, false
	}

	// Check if expired
	if time.Now().After(item.expiresAt) {
		c.Delete(key)
		var zero V
		return zero, false
	}

	// Move to front (most recently used)
	c.mu.Lock()
	if elem, exists := c.orderMap[key]; exists {
		c.order.MoveToFront(elem)
	}
	c.mu.Unlock()

	return item.value, true
}

// Set adds or updates an item in the cache.
func (c *Cache[K, V]) Set(key K, value V) {
	c.SetWithTTL(key, value, c.ttl)
}

// SetWithTTL adds or updates an item with a custom TTL.
func (c *Cache[K, V]) SetWithTTL(key K, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing item
	if _, exists := c.items[key]; exists {
		c.items[key] = &cacheItem[V]{
			value:     value,
			expiresAt: time.Now().Add(ttl),
		}
		if elem, ok := c.orderMap[key]; ok {
			c.order.MoveToFront(elem)
		}
		return
	}

	// Evict oldest items if at capacity
	for c.order.Len() >= c.maxSize {
		c.evictOldest()
	}

	// Add new item
	c.items[key] = &cacheItem[V]{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}
	elem := c.order.PushFront(key)
	c.orderMap[key] = elem
}

// Delete removes an item from the cache.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.items, key)
	if elem, ok := c.orderMap[key]; ok {
		c.order.Remove(elem)
		delete(c.orderMap, key)
	}
}

// Clear removes all items from the cache.
func (c *Cache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[K]*cacheItem[V])
	c.order = list.New()
	c.orderMap = make(map[K]*list.Element)
}

// Len returns the number of items in the cache.
func (c *Cache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Close stops the cleanup goroutine.
func (c *Cache[K, V]) Close() {
	close(c.done)
	c.cleanup.Stop()
}

// evictOldest removes the least recently used item.
// Must be called with lock held.
func (c *Cache[K, V]) evictOldest() {
	elem := c.order.Back()
	if elem == nil {
		return
	}

	key := elem.Value.(K)
	delete(c.items, key)
	c.order.Remove(elem)
	delete(c.orderMap, key)
}

// cleanupLoop periodically removes expired items.
func (c *Cache[K, V]) cleanupLoop() {
	for {
		select {
		case <-c.cleanup.C:
			c.removeExpired()
		case <-c.done:
			return
		}
	}
}

// removeExpired removes all expired items.
func (c *Cache[K, V]) removeExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, item := range c.items {
		if now.After(item.expiresAt) {
			delete(c.items, key)
			if elem, ok := c.orderMap[key]; ok {
				c.order.Remove(elem)
				delete(c.orderMap, key)
			}
		}
	}
}

// ── Convenience cache types ─────────────────────────────────────────────────

// StringCache is a cache with string keys and values.
type StringCache = Cache[string, string]

// NewStringCache creates a new string cache.
func NewStringCache(cfg Config) *StringCache {
	return New[string, string](cfg)
}

// ProfileCache caches user profiles.
type ProfileCache[V any] struct {
	*Cache[string, V]
}

// NewProfileCache creates a cache for user profiles.
func NewProfileCache[V any](ttl time.Duration, maxSize int) *ProfileCache[V] {
	return &ProfileCache[V]{
		Cache: New[string, V](Config{
			MaxSize:         maxSize,
			TTL:             ttl,
			CleanupInterval: ttl / 2,
		}),
	}
}

// MealPlanCache caches meal plans.
type MealPlanCache[V any] struct {
	*Cache[string, V]
}

// NewMealPlanCache creates a cache for meal plans.
func NewMealPlanCache[V any](ttl time.Duration, maxSize int) *MealPlanCache[V] {
	return &MealPlanCache[V]{
		Cache: New[string, V](Config{
			MaxSize:         maxSize,
			TTL:             ttl,
			CleanupInterval: ttl / 2,
		}),
	}
}

// ── GetOrSet pattern ────────────────────────────────────────────────────────

// GetOrSet retrieves an item from cache or computes it using the loader function.
func GetOrSet[K comparable, V any](c *Cache[K, V], key K, loader func() (V, error)) (V, error) {
	// Try to get from cache first
	if value, ok := c.Get(key); ok {
		return value, nil
	}

	// Load the value
	value, err := loader()
	if err != nil {
		var zero V
		return zero, err
	}

	// Store in cache
	c.Set(key, value)
	return value, nil
}

// GetOrSetWithTTL retrieves an item from cache or computes it with custom TTL.
func GetOrSetWithTTL[K comparable, V any](c *Cache[K, V], key K, ttl time.Duration, loader func() (V, error)) (V, error) {
	if value, ok := c.Get(key); ok {
		return value, nil
	}

	value, err := loader()
	if err != nil {
		var zero V
		return zero, err
	}

	c.SetWithTTL(key, value, ttl)
	return value, nil
}
