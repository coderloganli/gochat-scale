package integration

import (
	"flag"
	"log"
	"os"
	"testing"
	"time"

	"gochat/tests/helpers"
)

// TestMain is the entry point for integration tests.
// It ensures services are ready before running tests.
func TestMain(m *testing.M) {
	// Parse flags first - required before calling testing.Short()
	flag.Parse()

	// Skip service check in short mode
	if testing.Short() {
		os.Exit(m.Run())
	}

	cfg := helpers.DefaultTestConfig()

	log.Println("Waiting for services to be ready...")

	// Wait for services with timeout
	timeout := 60 * time.Second
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		err := helpers.CheckServicesReady(cfg)
		if err == nil {
			log.Println("All services are ready")
			break
		}
		log.Printf("Services not ready yet: %v", err)
		time.Sleep(2 * time.Second)
	}

	// Final check
	if err := helpers.CheckServicesReady(cfg); err != nil {
		log.Printf("WARNING: Services may not be fully ready: %v", err)
		log.Println("Proceeding with tests anyway...")
	}

	// Run tests
	code := m.Run()

	// Cleanup if needed
	log.Println("Integration tests completed")

	os.Exit(code)
}
