/**
 * Created by lock
 * Date: 2019-08-12
 * Time: 14:18
 */
package tools

import (
	"fmt"
	"github.com/go-redis/redis"
	"sync"
	"time"
)

var RedisClientMap = map[string]*redis.Client{}
var syncLock sync.Mutex

type RedisOption struct {
	Address  string
	Password string
	Db       int
}

func GetRedisInstance(redisOpt RedisOption) *redis.Client {
	address := redisOpt.Address
	db := redisOpt.Db
	password := redisOpt.Password
	addr := fmt.Sprintf("%s", address)

	// Fast path: check without lock first (read-only, safe for concurrent access)
	syncLock.Lock()
	if redisCli, ok := RedisClientMap[addr]; ok {
		syncLock.Unlock() // Fix: release lock before returning
		return redisCli
	}

	// Slow path: create new client while holding lock
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  2 * time.Second,  // Connection timeout
		ReadTimeout:  1 * time.Second,  // Read timeout
		WriteTimeout: 1 * time.Second,  // Write timeout
		PoolSize:     200,              // Increased pool size for high concurrency
		MinIdleConns: 20,               // More idle connections ready
		MaxRetries:   1,                // Reduce retries for fast failure
		MaxConnAge:   0,                // No max age, keep connections alive
		PoolTimeout:  3 * time.Second,  // Wait for connection from pool
		IdleTimeout:  5 * time.Minute,  // Keep idle connections longer
	})
	RedisClientMap[addr] = client
	syncLock.Unlock()
	return client
}
