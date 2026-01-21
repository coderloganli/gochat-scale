package tools

import (
	"time"

	"gochat/pkg/metrics"

	"github.com/go-redis/redis"
)

// InstrumentedRedisClient wraps a Redis client with metrics instrumentation.
type InstrumentedRedisClient struct {
	client      *redis.Client
	serviceName string
}

// NewInstrumentedRedisClient creates a new instrumented Redis client.
func NewInstrumentedRedisClient(client *redis.Client, serviceName string) *InstrumentedRedisClient {
	return &InstrumentedRedisClient{
		client:      client,
		serviceName: serviceName,
	}
}

// GetClient returns the underlying Redis client for operations not yet wrapped.
func (c *InstrumentedRedisClient) GetClient() *redis.Client {
	return c.client
}

func (c *InstrumentedRedisClient) recordMetrics(command string, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil && err != redis.Nil {
		status = "error"
	}
	metrics.RedisOperationDuration.WithLabelValues(c.serviceName, command).Observe(duration)
	metrics.RedisOperationsTotal.WithLabelValues(c.serviceName, command, status).Inc()
}

// Get wraps redis GET command with metrics.
func (c *InstrumentedRedisClient) Get(key string) *redis.StringCmd {
	start := time.Now()
	result := c.client.Get(key)
	c.recordMetrics("GET", start, result.Err())
	return result
}

// Set wraps redis SET command with metrics.
func (c *InstrumentedRedisClient) Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	start := time.Now()
	result := c.client.Set(key, value, expiration)
	c.recordMetrics("SET", start, result.Err())
	return result
}

// SetEX wraps redis SETEX command with metrics.
func (c *InstrumentedRedisClient) SetEX(key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	start := time.Now()
	result := c.client.Set(key, value, expiration)
	c.recordMetrics("SETEX", start, result.Err())
	return result
}

// Del wraps redis DEL command with metrics.
func (c *InstrumentedRedisClient) Del(keys ...string) *redis.IntCmd {
	start := time.Now()
	result := c.client.Del(keys...)
	c.recordMetrics("DEL", start, result.Err())
	return result
}

// Exists wraps redis EXISTS command with metrics.
func (c *InstrumentedRedisClient) Exists(keys ...string) *redis.IntCmd {
	start := time.Now()
	result := c.client.Exists(keys...)
	c.recordMetrics("EXISTS", start, result.Err())
	return result
}

// HGet wraps redis HGET command with metrics.
func (c *InstrumentedRedisClient) HGet(key, field string) *redis.StringCmd {
	start := time.Now()
	result := c.client.HGet(key, field)
	c.recordMetrics("HGET", start, result.Err())
	return result
}

// HSet wraps redis HSET command with metrics.
func (c *InstrumentedRedisClient) HSet(key, field string, value interface{}) *redis.BoolCmd {
	start := time.Now()
	result := c.client.HSet(key, field, value)
	c.recordMetrics("HSET", start, result.Err())
	return result
}

// HGetAll wraps redis HGETALL command with metrics.
func (c *InstrumentedRedisClient) HGetAll(key string) *redis.StringStringMapCmd {
	start := time.Now()
	result := c.client.HGetAll(key)
	c.recordMetrics("HGETALL", start, result.Err())
	return result
}

// HDel wraps redis HDEL command with metrics.
func (c *InstrumentedRedisClient) HDel(key string, fields ...string) *redis.IntCmd {
	start := time.Now()
	result := c.client.HDel(key, fields...)
	c.recordMetrics("HDEL", start, result.Err())
	return result
}

// HLen wraps redis HLEN command with metrics.
func (c *InstrumentedRedisClient) HLen(key string) *redis.IntCmd {
	start := time.Now()
	result := c.client.HLen(key)
	c.recordMetrics("HLEN", start, result.Err())
	return result
}

// LPush wraps redis LPUSH command with metrics.
func (c *InstrumentedRedisClient) LPush(key string, values ...interface{}) *redis.IntCmd {
	start := time.Now()
	result := c.client.LPush(key, values...)
	c.recordMetrics("LPUSH", start, result.Err())
	return result
}

// RPush wraps redis RPUSH command with metrics.
func (c *InstrumentedRedisClient) RPush(key string, values ...interface{}) *redis.IntCmd {
	start := time.Now()
	result := c.client.RPush(key, values...)
	c.recordMetrics("RPUSH", start, result.Err())
	return result
}

// LPop wraps redis LPOP command with metrics.
func (c *InstrumentedRedisClient) LPop(key string) *redis.StringCmd {
	start := time.Now()
	result := c.client.LPop(key)
	c.recordMetrics("LPOP", start, result.Err())
	return result
}

// RPop wraps redis RPOP command with metrics.
func (c *InstrumentedRedisClient) RPop(key string) *redis.StringCmd {
	start := time.Now()
	result := c.client.RPop(key)
	c.recordMetrics("RPOP", start, result.Err())
	return result
}

// LRange wraps redis LRANGE command with metrics.
func (c *InstrumentedRedisClient) LRange(key string, start, stop int64) *redis.StringSliceCmd {
	startTime := time.Now()
	result := c.client.LRange(key, start, stop)
	c.recordMetrics("LRANGE", startTime, result.Err())
	return result
}

// Publish wraps redis PUBLISH command with metrics.
func (c *InstrumentedRedisClient) Publish(channel string, message interface{}) *redis.IntCmd {
	start := time.Now()
	result := c.client.Publish(channel, message)
	c.recordMetrics("PUBLISH", start, result.Err())
	return result
}

// Subscribe wraps redis SUBSCRIBE command with metrics.
func (c *InstrumentedRedisClient) Subscribe(channels ...string) *redis.PubSub {
	start := time.Now()
	result := c.client.Subscribe(channels...)
	c.recordMetrics("SUBSCRIBE", start, nil)
	return result
}

// SAdd wraps redis SADD command with metrics.
func (c *InstrumentedRedisClient) SAdd(key string, members ...interface{}) *redis.IntCmd {
	start := time.Now()
	result := c.client.SAdd(key, members...)
	c.recordMetrics("SADD", start, result.Err())
	return result
}

// SMembers wraps redis SMEMBERS command with metrics.
func (c *InstrumentedRedisClient) SMembers(key string) *redis.StringSliceCmd {
	start := time.Now()
	result := c.client.SMembers(key)
	c.recordMetrics("SMEMBERS", start, result.Err())
	return result
}

// SRem wraps redis SREM command with metrics.
func (c *InstrumentedRedisClient) SRem(key string, members ...interface{}) *redis.IntCmd {
	start := time.Now()
	result := c.client.SRem(key, members...)
	c.recordMetrics("SREM", start, result.Err())
	return result
}

// SCard wraps redis SCARD command with metrics.
func (c *InstrumentedRedisClient) SCard(key string) *redis.IntCmd {
	start := time.Now()
	result := c.client.SCard(key)
	c.recordMetrics("SCARD", start, result.Err())
	return result
}

// Expire wraps redis EXPIRE command with metrics.
func (c *InstrumentedRedisClient) Expire(key string, expiration time.Duration) *redis.BoolCmd {
	start := time.Now()
	result := c.client.Expire(key, expiration)
	c.recordMetrics("EXPIRE", start, result.Err())
	return result
}

// TTL wraps redis TTL command with metrics.
func (c *InstrumentedRedisClient) TTL(key string) *redis.DurationCmd {
	start := time.Now()
	result := c.client.TTL(key)
	c.recordMetrics("TTL", start, result.Err())
	return result
}

// Incr wraps redis INCR command with metrics.
func (c *InstrumentedRedisClient) Incr(key string) *redis.IntCmd {
	start := time.Now()
	result := c.client.Incr(key)
	c.recordMetrics("INCR", start, result.Err())
	return result
}

// Decr wraps redis DECR command with metrics.
func (c *InstrumentedRedisClient) Decr(key string) *redis.IntCmd {
	start := time.Now()
	result := c.client.Decr(key)
	c.recordMetrics("DECR", start, result.Err())
	return result
}

// Pipeline wraps redis Pipeline command with metrics.
func (c *InstrumentedRedisClient) Pipeline() redis.Pipeliner {
	start := time.Now()
	result := c.client.Pipeline()
	c.recordMetrics("PIPELINE", start, nil)
	return result
}

// Ping wraps redis PING command with metrics.
func (c *InstrumentedRedisClient) Ping() *redis.StatusCmd {
	start := time.Now()
	result := c.client.Ping()
	c.recordMetrics("PING", start, result.Err())
	return result
}
