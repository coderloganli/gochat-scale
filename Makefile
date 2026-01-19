

.PHONY: build help compose-dev compose-dev-build compose-dev-down compose-prod compose-prod-build compose-prod-down compose-scale compose-logs compose-ps clean test-infra test test-coverage test-unit test-integration fmt fmt-check vet lint build-binary build-image \
	loadtest-help loadtest-setup loadtest-start loadtest-stop loadtest-full loadtest-capacity loadtest-login loadtest-register loadtest-logout loadtest-checkauth loadtest-websocket loadtest-push loadtest-pushroom loadtest-count loadtest-roominfo loadtest-smoke loadtest-custom loadtest-report loadtest-grafana loadtest-clean

# Default HOST_IP for development
HOST_IP ?= 127.0.0.1

# Legacy build command (for backward compatibility)
build: # make build TAG=1.23;make build TAG=latest,自定义版本号,构建自己机器可以运行的镜像(因为M1机器拉取提供的镜像架构不同,只能自己构建)
	cd ./docker && docker build -t lockgit/gochat:${TAG} .

help:
	@echo "GoChat Multi-Container Deployment"
	@echo ""
	@echo "Available targets:"
	@echo "  build                - Legacy: Build single-container image (use TAG=version)"
	@echo "  compose-dev          - Start development environment"
	@echo "  compose-dev-build    - Build and start development environment"
	@echo "  compose-dev-down     - Stop development environment"
	@echo "  compose-prod         - Start production environment (detached)"
	@echo "  compose-prod-build   - Build and start production environment"
	@echo "  compose-prod-down    - Stop production environment"
	@echo "  compose-scale        - Start with scaled services (logic=3, connect-ws=2, task=2)"
	@echo "  compose-logs         - View logs from all services"
	@echo "  compose-ps           - Show running services"
	@echo "  test-infra           - Test infrastructure only (etcd + redis)"
	@echo "  clean                - Remove all containers, volumes, and images"
	@echo ""
	@echo "Testing & Quality:"
	@echo "  test                   - Run all tests"
	@echo "  test-coverage          - Run tests with coverage report"
	@echo "  test-unit              - Run unit tests only"
	@echo "  test-integration       - Run integration tests with Docker (starts services)"
	@echo "  test-integration-quick - Run integration tests (services must be running)"
	@echo "  fmt                    - Format code with go fmt"
	@echo "  fmt-check              - Check if code is formatted"
	@echo "  vet                    - Run go vet"
	@echo "  lint                   - Run golangci-lint"
	@echo ""
	@echo "Building:"
	@echo "  build-binary         - Build gochat binary"
	@echo "  build-image          - Build Docker image"
	@echo ""
	@echo "Load Testing:"
	@echo "  loadtest-help        - Show load testing help"
	@echo "  loadtest-full        - Run full system load test"
	@echo "  loadtest-capacity    - Run step-based capacity test"
	@echo "  loadtest-smoke       - Quick smoke test"
	@echo ""
	@echo "Environment variables:"
	@echo "  HOST_IP=<ip>         - Set host IP address (default: 127.0.0.1)"

# Development commands
compose-dev:
	@echo "Starting GoChat in development mode..."
	HOST_IP=$(HOST_IP) docker compose -f docker-compose.yml -f deployments/docker-compose.dev.yml up

compose-dev-build:
	@echo "Building and starting GoChat in development mode..."
	HOST_IP=$(HOST_IP) docker compose -f docker-compose.yml -f deployments/docker-compose.dev.yml up --build

compose-dev-down:
	@echo "Stopping development environment..."
	docker compose -f docker-compose.yml -f deployments/docker-compose.dev.yml down

# Production commands
compose-prod:
	@echo "Starting GoChat in production mode..."
	@if [ -z "$(HOST_IP)" ] || [ "$(HOST_IP)" = "127.0.0.1" ]; then \
		echo "WARNING: HOST_IP is set to localhost. For production, use: make compose-prod HOST_IP=<your-server-ip>"; \
		echo "Continuing in 3 seconds..."; \
		sleep 3; \
	fi
	HOST_IP=$(HOST_IP) docker compose -f docker-compose.yml -f deployments/docker-compose.prod.yml up -d

compose-prod-build:
	@echo "Building and starting GoChat in production mode..."
	@if [ -z "$(HOST_IP)" ] || [ "$(HOST_IP)" = "127.0.0.1" ]; then \
		echo "WARNING: HOST_IP is set to localhost. For production, use: make compose-prod-build HOST_IP=<your-server-ip>"; \
		echo "Continuing in 3 seconds..."; \
		sleep 3; \
	fi
	HOST_IP=$(HOST_IP) docker compose -f docker-compose.yml -f deployments/docker-compose.prod.yml up -d --build

compose-prod-down:
	@echo "Stopping production environment..."
	docker compose -f docker-compose.yml -f deployments/docker-compose.prod.yml down

# Scaling example
compose-scale:
	@echo "Starting GoChat with scaled services..."
	HOST_IP=$(HOST_IP) docker compose -f docker-compose.yml up \
		--scale logic=3 \
		--scale connect-ws=2 \
		--scale connect-tcp=2 \
		--scale task=2 \
		--scale api=2

# Utility commands
compose-logs:
	docker compose -f docker-compose.yml logs -f

compose-ps:
	docker compose -f docker-compose.yml ps

# Test infrastructure only (etcd + redis)
test-infra:
	@echo "Starting infrastructure services only..."
	docker compose -f docker-compose.yml up etcd redis

# Clean up everything
clean:
	@echo "WARNING: This will remove all GoChat containers, volumes, and images."
	@echo "Press Ctrl+C to cancel, or wait 5 seconds to continue..."
	@sleep 5
	docker compose -f docker-compose.yml -f deployments/docker-compose.dev.yml down -v --rmi all || true
	docker compose -f docker-compose.yml -f deployments/docker-compose.prod.yml down -v --rmi all || true
	@echo "Clean complete!"

# Testing targets
test:
	@echo "Running all tests..."
	go test -v -race ./...

test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

test-unit:
	@echo "Running unit tests..."
	go test -v -short ./...

test-integration:
	@echo "Running integration tests with Docker..."
	@echo "Starting services..."
	docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml up -d --build
	@echo "Waiting for services to be healthy..."
	@sleep 30
	@echo "Running integration tests from host..."
	TEST_API_URL=http://localhost:7070 \
	TEST_WS_URL=ws://localhost:7000/ws \
	TEST_REDIS_ADDR=localhost:6379 \
	go test -v -race -timeout 10m ./tests/integration/... || (docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml down && exit 1)
	@echo "Stopping services..."
	docker compose -f docker-compose.yml -f deployments/docker-compose.test.yml down

test-integration-quick:
	@echo "Running integration tests (assumes services are already running)..."
	TEST_API_URL=http://localhost:7070 \
	TEST_WS_URL=ws://localhost:7000/ws \
	TEST_REDIS_ADDR=localhost:6379 \
	go test -v -race -timeout 10m ./tests/integration/...

# Code quality targets
fmt:
	@echo "Formatting code..."
	go fmt ./...

fmt-check:
	@echo "Checking code formatting..."
	@test -z "$$(go fmt ./...)" || (echo "Code not formatted, run 'make fmt'" && exit 1)

vet:
	@echo "Running go vet..."
	go vet ./...

lint:
	@echo "Running golangci-lint..."
	golangci-lint run

# Build targets
build-binary:
	@echo "Building gochat binary..."
	CGO_ENABLED=1 GOOS=linux go build -tags=etcd -ldflags="-w -s" -o bin/gochat main.go
	@echo "Binary built: bin/gochat"

build-image:
	@echo "Building Docker image..."
	docker build -t gochat:latest -f docker/Dockerfile .
	@echo "Image built: gochat:latest"

# ============================================
# Load Testing Targets
# ============================================

# Load test configuration (all runtime-configurable)
K6_VUS ?=
K6_DURATION ?=
K6_START_VUS ?= 10
K6_END_VUS ?= 100
K6_STEP_VUS ?= 10
K6_STEP_DURATION ?= 1m
K6_RAMP_DURATION ?= 30s
K6_WARMUP_DURATION ?= 20s
LOADTEST_SCRIPT ?= full-system.js

# Compose files for load testing
LOADTEST_COMPOSE = docker compose -f docker-compose.yml -f loadtest/docker-compose.loadtest.yml

# Only pass K6_VUS when explicitly set to avoid forcing fixed-VU mode.
K6_ENV_VARS = \
	-e K6_DURATION=$(K6_DURATION) \
	-e K6_START_VUS=$(K6_START_VUS) \
	-e K6_END_VUS=$(K6_END_VUS) \
	-e K6_STEP_VUS=$(K6_STEP_VUS) \
	-e K6_STEP_DURATION=$(K6_STEP_DURATION) \
	-e K6_RAMP_DURATION=$(K6_RAMP_DURATION) \
	-e K6_WARMUP_DURATION=$(K6_WARMUP_DURATION)

ifneq ($(strip $(K6_VUS)),)
	K6_ENV_VARS += -e K6_VUS=$(K6_VUS)
endif

# Show load test help
loadtest-help:
	@echo "GoChat Load Testing Commands"
	@echo ""
	@echo "Setup & Infrastructure:"
	@echo "  loadtest-setup       - Pull required Docker images"
	@echo "  loadtest-start       - Start services (with rebuild)"
	@echo "  loadtest-start-quick - Start services (no rebuild, faster)"
	@echo "  loadtest-stop        - Stop all load test services"
	@echo "  loadtest-clean       - Remove reports and volumes"
	@echo ""
	@echo "  Tip: Use NOBUILD=1 to skip rebuild: make loadtest-start NOBUILD=1"
	@echo ""
	@echo "Test Scenarios:"
	@echo "  loadtest-full       - Run complete system load test"
	@echo "  loadtest-capacity   - Run step-based capacity baseline test"
	@echo "  loadtest-login      - Test /user/login endpoint only"
	@echo "  loadtest-register   - Test /user/register endpoint only"
	@echo "  loadtest-logout     - Test /user/logout endpoint only"
	@echo "  loadtest-checkauth  - Test /user/checkAuth endpoint only"
	@echo "  loadtest-websocket  - Test WebSocket connections only"
	@echo "  loadtest-push       - Test /push/push endpoint only"
	@echo "  loadtest-pushroom   - Test /push/pushRoom endpoint only"
	@echo "  loadtest-count      - Test /push/count endpoint only"
	@echo "  loadtest-roominfo   - Test /push/getRoomInfo endpoint only"
	@echo "  loadtest-smoke      - Quick 30s smoke test (5 VUs)"
	@echo "  loadtest-custom     - Run custom script (LOADTEST_SCRIPT=file.js)"
	@echo ""
	@echo "Reporting:"
	@echo "  loadtest-report     - Generate HTML report from results"
	@echo "  loadtest-grafana    - Start Grafana for real-time monitoring"
	@echo ""
	@echo "Configuration (all runtime-configurable):"
	@echo "  K6_VUS              - Fixed number of virtual users"
	@echo "  K6_DURATION         - Test duration (e.g., 5m, 10m)"
	@echo "  K6_START_VUS        - Starting VUs for capacity test (default: 10)"
	@echo "  K6_END_VUS          - Maximum VUs for capacity test (default: 100)"
	@echo "  K6_STEP_VUS         - VU increment per step (default: 10)"
	@echo "  K6_STEP_DURATION    - Duration at each step (default: 1m)"
	@echo "  K6_RAMP_DURATION    - Ramp duration between steps (default: 30s)"
	@echo "  K6_WARMUP_DURATION  - Warm-up per step (excluded from stats, default: 20s)"
	@echo ""
	@echo "Examples:"
	@echo "  make loadtest-full K6_VUS=200 K6_DURATION=10m"
	@echo "  make loadtest-capacity K6_START_VUS=10 K6_END_VUS=500 K6_STEP_VUS=50"
	@echo "  make loadtest-login K6_VUS=50 K6_DURATION=2m"

# Setup load testing environment
loadtest-setup:
	@echo "Setting up load testing environment..."
	@mkdir -p loadtest/reports
	@mkdir -p loadtest/scripts/lib/vendor
	@if [ ! -f loadtest/scripts/lib/vendor/k6-reporter.js ]; then \
		echo "Downloading k6-reporter..."; \
		curl -sL "https://raw.githubusercontent.com/benc-uk/k6-reporter/2.4.0/dist/bundle.js" \
			-o loadtest/scripts/lib/vendor/k6-reporter.js; \
	fi
	$(LOADTEST_COMPOSE) pull k6 || true
	@echo "Setup complete"

# Ensure k6-reporter dependency exists
loadtest-deps:
	@mkdir -p loadtest/scripts/lib/vendor
	@if [ ! -f loadtest/scripts/lib/vendor/k6-reporter.js ]; then \
		echo "Downloading k6-reporter..."; \
		curl -sL "https://raw.githubusercontent.com/benc-uk/k6-reporter/2.4.0/dist/bundle.js" \
			-o loadtest/scripts/lib/vendor/k6-reporter.js; \
	fi

# Start services for load testing (with resource limits)
# Use NOBUILD=1 to skip rebuilding images (e.g., make loadtest-start NOBUILD=1)
loadtest-start: loadtest-deps
	@echo "Starting GoChat services for load testing..."
ifeq ($(NOBUILD),1)
	$(LOADTEST_COMPOSE) up -d etcd redis logic connect-ws connect-tcp task api
else
	$(LOADTEST_COMPOSE) up -d --build etcd redis logic connect-ws connect-tcp task api
endif
	@echo "Waiting for services to be healthy (60s)..."
	@sleep 60
	@echo "Services ready for load testing"

# Start services without rebuilding (quick start for load testing)
loadtest-start-quick: loadtest-deps
	@echo "Starting GoChat services for load testing (no rebuild)..."
	$(LOADTEST_COMPOSE) up -d etcd redis logic connect-ws connect-tcp task api
	@echo "Waiting for services to be healthy (60s)..."
	@sleep 60
	@echo "Services ready for load testing"

# Stop load testing environment
loadtest-stop:
	@echo "Stopping load testing environment..."
	$(LOADTEST_COMPOSE) down

# Run full system load test
loadtest-full: loadtest-start
	@echo "Running full system load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/full-system.js
	@echo "Test complete. Report: loadtest/reports/full-system.html"

# Run capacity baseline test (step-based)
loadtest-capacity: loadtest-start
	@echo "Running capacity baseline test..."
	@echo "Configuration: Start=$(K6_START_VUS) End=$(K6_END_VUS) Step=$(K6_STEP_VUS)"
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/capacity-baseline.js
	@echo "Test complete. Report: loadtest/reports/capacity-baseline.html"

# Run login endpoint test only
loadtest-login: loadtest-start
	@echo "Running login endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/user-login.js
	@echo "Test complete. Report: loadtest/reports/user-login.html"

# Run register endpoint test only
loadtest-register: loadtest-start
	@echo "Running register endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/user-register.js
	@echo "Test complete. Report: loadtest/reports/user-register.html"

# Run WebSocket endpoint test only
loadtest-websocket: loadtest-start
	@echo "Running WebSocket load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/websocket.js
	@echo "Test complete. Report: loadtest/reports/websocket.html"

# Run push endpoint test only
loadtest-push: loadtest-start
	@echo "Running push endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/push-push.js
	@echo "Test complete. Report: loadtest/reports/push-push.html"

# Run push room endpoint test only
loadtest-pushroom: loadtest-start
	@echo "Running push room endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/push-room.js
	@echo "Test complete. Report: loadtest/reports/push-room.html"

# Run logout endpoint test only
loadtest-logout: loadtest-start
	@echo "Running logout endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/user-logout.js
	@echo "Test complete. Report: loadtest/reports/user-logout.html"

# Run checkAuth endpoint test only
loadtest-checkauth: loadtest-start
	@echo "Running checkAuth endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/user-checkauth.js
	@echo "Test complete. Report: loadtest/reports/user-checkauth.html"

# Run push count endpoint test only
loadtest-count: loadtest-start
	@echo "Running push count endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/push-count.js
	@echo "Test complete. Report: loadtest/reports/push-count.html"

# Run room info endpoint test only
loadtest-roominfo: loadtest-start
	@echo "Running room info endpoint load test..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/scenarios/push-roominfo.js
	@echo "Test complete. Report: loadtest/reports/push-roominfo.html"

# Run quick smoke test
loadtest-smoke: loadtest-start
	@echo "Running smoke test (5 VUs, 30s)..."
	$(LOADTEST_COMPOSE) run --rm \
		-e K6_VUS=5 \
		-e K6_DURATION=30s \
		k6 run /scripts/full-system.js
	@echo "Smoke test complete"

# Run custom script
loadtest-custom: loadtest-start
	@echo "Running custom load test: $(LOADTEST_SCRIPT)..."
	$(LOADTEST_COMPOSE) run --rm \
		$(K6_ENV_VARS) \
		k6 run /scripts/$(LOADTEST_SCRIPT)

# Start Grafana for load test visualization
loadtest-grafana:
	@echo "Starting Grafana for load test visualization..."
	$(LOADTEST_COMPOSE) --profile monitoring up -d grafana
	@echo "Grafana available at http://localhost:3000 (admin/admin)"

# Clean load test artifacts
loadtest-clean:
	@echo "Cleaning load test artifacts..."
	rm -rf loadtest/reports/*.json loadtest/reports/*.html loadtest/reports/*.txt 2>/dev/null || true
	$(LOADTEST_COMPOSE) down -v 2>/dev/null || true
	@echo "Load test cleanup complete"
