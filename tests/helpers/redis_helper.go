// Package helpers provides test utilities for integration tests.
package helpers

import (
	"github.com/go-redis/redis"
)

// RedisHelper provides test utilities for Redis
type RedisHelper struct {
	Client *redis.Client
}

// NewRedisHelper creates a Redis test helper
func NewRedisHelper(addr string) *RedisHelper {
	return &RedisHelper{
		Client: redis.NewClient(&redis.Options{
			Addr: addr,
			DB:   0,
		}),
	}
}

// CleanTestData removes test-related keys by pattern
func (h *RedisHelper) CleanTestData(pattern string) error {
	keys, err := h.Client.Keys(pattern).Result()
	if err != nil {
		return err
	}
	if len(keys) > 0 {
		return h.Client.Del(keys...).Err()
	}
	return nil
}

// GetQueueLength returns current queue length
func (h *RedisHelper) GetQueueLength(queueName string) (int64, error) {
	return h.Client.LLen(queueName).Result()
}

// GetSessionData retrieves session data as hash
func (h *RedisHelper) GetSessionData(sessionKey string) (map[string]string, error) {
	return h.Client.HGetAll(sessionKey).Result()
}

// GetStringValue retrieves a string value
func (h *RedisHelper) GetStringValue(key string) (string, error) {
	return h.Client.Get(key).Result()
}

// KeyExists checks if a key exists
func (h *RedisHelper) KeyExists(key string) (bool, error) {
	result, err := h.Client.Exists(key).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

// GetRoomUserCount gets the number of users in a room
func (h *RedisHelper) GetRoomUserCount(roomId int) (int64, error) {
	key := "gochat_room_" + string(rune(roomId))
	return h.Client.HLen(key).Result()
}

// FlushDB clears all test data (use with caution)
func (h *RedisHelper) FlushDB() error {
	return h.Client.FlushDB().Err()
}

// Close closes the Redis connection
func (h *RedisHelper) Close() error {
	return h.Client.Close()
}

// Ping checks Redis connectivity
func (h *RedisHelper) Ping() error {
	return h.Client.Ping().Err()
}
