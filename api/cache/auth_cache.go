// Package cache provides local caching for API service
package cache

import (
	"os"
	"strconv"
	"sync"
	"time"

	"gochat/pkg/metrics"

	"github.com/sirupsen/logrus"
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
	enabled bool
}

const (
	// defaultTTL bounds how long a revoked token keeps working. Logout deletes
	// the entry on the API instance that served it, so this is the worst case
	// for the other instances.
	defaultTTL = 30 * time.Second
)

// config reads the cache settings from the environment. The cache can be turned
// off so that the same build can be measured with and without it; the A/B is
// what makes the latency claim in docs/benchmarks.md reproducible.
func config() (enabled bool, ttl time.Duration) {
	enabled = true
	if v := os.Getenv("AUTH_CACHE_ENABLED"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			logrus.Warnf("invalid AUTH_CACHE_ENABLED %q, keeping the cache enabled", v)
		} else {
			enabled = parsed
		}
	}
	ttl = defaultTTL
	if v := os.Getenv("AUTH_CACHE_TTL"); v != "" {
		parsed, err := time.ParseDuration(v)
		if err != nil || parsed <= 0 {
			logrus.Warnf("invalid AUTH_CACHE_TTL %q, keeping %s", v, defaultTTL)
		} else {
			ttl = parsed
		}
	}
	return enabled, ttl
}

// Global auth cache instance
var (
	globalAuthCache *AuthCache
	cacheOnce       sync.Once
)

// GetAuthCache returns the singleton auth cache instance
func GetAuthCache() *AuthCache {
	cacheOnce.Do(func() {
		enabled, ttl := config()
		globalAuthCache = &AuthCache{
			entries: make(map[string]*AuthCacheEntry),
			ttl:     ttl,
			enabled: enabled,
		}
		if !enabled {
			logrus.Warn("auth cache disabled, every request will hit the logic RPC")
			return
		}
		logrus.Infof("auth cache enabled, ttl %s", ttl)
		// Start background cleanup goroutine
		go globalAuthCache.cleanupLoop()
	})
	return globalAuthCache
}

// Get retrieves auth info from cache
// Returns (userId, userName, found)
func (c *AuthCache) Get(token string) (int, string, bool) {
	if !c.enabled {
		metrics.AuthCacheMisses.Inc()
		return 0, "", false
	}
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
	if !c.enabled {
		return
	}
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
	if !c.enabled {
		return
	}
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
