// Package cache provides local caching for API service
package cache

import (
	"sync"
	"time"

	"gochat/pkg/metrics"
)

// AuthCacheEntry stores cached auth result
type AuthCacheEntry struct {
	UserId    int
	UserName  string
	ExpiresAt time.Time
}

// AuthCache provides a simple in-memory cache for auth tokens
// to reduce RPC calls to logic service
type AuthCache struct {
	mu      sync.RWMutex
	entries map[string]*AuthCacheEntry
	ttl     time.Duration
}

// Global auth cache instance
var (
	globalAuthCache *AuthCache
	cacheOnce       sync.Once
)

// GetAuthCache returns the singleton auth cache instance
func GetAuthCache() *AuthCache {
	cacheOnce.Do(func() {
		globalAuthCache = &AuthCache{
			entries: make(map[string]*AuthCacheEntry),
			ttl:     30 * time.Second, // Cache auth for 30 seconds
		}
		// Start background cleanup goroutine
		go globalAuthCache.cleanupLoop()
	})
	return globalAuthCache
}

// Get retrieves auth info from cache
// Returns (userId, userName, found)
func (c *AuthCache) Get(token string) (int, string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[token]
	c.mu.RUnlock()

	if !ok {
		metrics.AuthCacheMisses.Inc()
		return 0, "", false
	}

	// Check if expired
	if time.Now().After(entry.ExpiresAt) {
		// Entry expired, remove it
		c.mu.Lock()
		delete(c.entries, token)
		c.mu.Unlock()
		metrics.AuthCacheMisses.Inc()
		return 0, "", false
	}

	metrics.AuthCacheHits.Inc()
	return entry.UserId, entry.UserName, true
}

// Set stores auth info in cache
func (c *AuthCache) Set(token string, userId int, userName string) {
	c.mu.Lock()
	c.entries[token] = &AuthCacheEntry{
		UserId:    userId,
		UserName:  userName,
		ExpiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

// Delete removes a token from cache (used on logout)
func (c *AuthCache) Delete(token string) {
	c.mu.Lock()
	delete(c.entries, token)
	c.mu.Unlock()
}

// cleanupLoop periodically removes expired entries
func (c *AuthCache) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup removes all expired entries
func (c *AuthCache) cleanup() {
	now := time.Now()
	c.mu.Lock()
	for token, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, token)
		}
	}
	size := len(c.entries)
	c.mu.Unlock()

	// Update cache size metric
	metrics.AuthCacheSize.Set(float64(size))
}

// Size returns the number of cached entries (for monitoring)
func (c *AuthCache) Size() int {
	c.mu.RLock()
	size := len(c.entries)
	c.mu.RUnlock()
	return size
}
