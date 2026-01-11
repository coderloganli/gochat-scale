// Package helpers provides test utilities for integration tests.
package helpers

import (
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/go-redis/redis"
)

// TestConfig holds test environment configuration
type TestConfig struct {
	APIBaseURL   string
	WSBaseURL    string
	TCPAddress   string
	RedisAddress string
	EtcdAddress  string
}

// DefaultTestConfig returns configuration for docker-compose.test.yml
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		APIBaseURL:   getEnv("TEST_API_URL", "http://localhost:7070"),
		WSBaseURL:    getEnv("TEST_WS_URL", "ws://localhost:7000/ws"),
		TCPAddress:   getEnv("TEST_TCP_ADDR", "localhost:7001"),
		RedisAddress: getEnv("TEST_REDIS_ADDR", "localhost:6379"),
		EtcdAddress:  getEnv("TEST_ETCD_ADDR", "localhost:2379"),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// WaitForServices waits for all services to be healthy
func WaitForServices(t *testing.T, cfg *TestConfig, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	checks := []struct {
		name  string
		check func() error
	}{
		{"API", func() error { return checkHTTP(cfg.APIBaseURL + "/") }},
		{"Redis", func() error { return checkRedis(cfg.RedisAddress) }},
	}

	for _, c := range checks {
		for time.Now().Before(deadline) {
			if err := c.check(); err == nil {
				t.Logf("%s is ready", c.name)
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
	return nil
}

func checkHTTP(url string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Accept any response as healthy (including 404 for non-existent endpoints)
	if resp.StatusCode >= 500 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func checkRedis(addr string) error {
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	return client.Ping().Err()
}

// CheckServicesReady performs a quick health check without waiting
func CheckServicesReady(cfg *TestConfig) error {
	if err := checkHTTP(cfg.APIBaseURL + "/"); err != nil {
		return fmt.Errorf("API service not ready: %w", err)
	}
	if err := checkRedis(cfg.RedisAddress); err != nil {
		return fmt.Errorf("Redis not ready: %w", err)
	}
	return nil
}
