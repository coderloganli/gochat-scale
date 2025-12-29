
.PHONY: build help compose-dev compose-dev-build compose-dev-down compose-prod compose-prod-build compose-prod-down compose-scale compose-logs compose-ps clean test-infra

# Default HOST_IP for development
HOST_IP ?= 127.0.0.1

# Legacy build command (for backward compatibility)
build: # make build TAG=1.18;make build TAG=latest,自定义版本号,构建自己机器可以运行的镜像(因为M1机器拉取提供的镜像架构不同,只能自己构建)
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
	@echo "Environment variables:"
	@echo "  HOST_IP=<ip>         - Set host IP address (default: 127.0.0.1)"

# Development commands
compose-dev:
	@echo "Starting GoChat in development mode..."
	HOST_IP=$(HOST_IP) docker-compose -f docker-compose.yml -f docker-compose.dev.yml up

compose-dev-build:
	@echo "Building and starting GoChat in development mode..."
	HOST_IP=$(HOST_IP) docker-compose -f docker-compose.yml -f docker-compose.dev.yml up --build

compose-dev-down:
	@echo "Stopping development environment..."
	docker-compose -f docker-compose.yml -f docker-compose.dev.yml down

# Production commands
compose-prod:
	@echo "Starting GoChat in production mode..."
	@if [ -z "$(HOST_IP)" ] || [ "$(HOST_IP)" = "127.0.0.1" ]; then \
		echo "WARNING: HOST_IP is set to localhost. For production, use: make compose-prod HOST_IP=<your-server-ip>"; \
		echo "Continuing in 3 seconds..."; \
		sleep 3; \
	fi
	HOST_IP=$(HOST_IP) docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d

compose-prod-build:
	@echo "Building and starting GoChat in production mode..."
	@if [ -z "$(HOST_IP)" ] || [ "$(HOST_IP)" = "127.0.0.1" ]; then \
		echo "WARNING: HOST_IP is set to localhost. For production, use: make compose-prod-build HOST_IP=<your-server-ip>"; \
		echo "Continuing in 3 seconds..."; \
		sleep 3; \
	fi
	HOST_IP=$(HOST_IP) docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

compose-prod-down:
	@echo "Stopping production environment..."
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml down

# Scaling example
compose-scale:
	@echo "Starting GoChat with scaled services..."
	HOST_IP=$(HOST_IP) docker-compose -f docker-compose.yml up \
		--scale logic=3 \
		--scale connect-ws=2 \
		--scale connect-tcp=2 \
		--scale task=2 \
		--scale api=2

# Utility commands
compose-logs:
	docker-compose -f docker-compose.yml logs -f

compose-ps:
	docker-compose -f docker-compose.yml ps

# Test infrastructure only (etcd + redis)
test-infra:
	@echo "Starting infrastructure services only..."
	docker-compose -f docker-compose.yml up etcd redis

# Clean up everything
clean:
	@echo "WARNING: This will remove all GoChat containers, volumes, and images."
	@echo "Press Ctrl+C to cancel, or wait 5 seconds to continue..."
	@sleep 5
	docker-compose -f docker-compose.yml -f docker-compose.dev.yml down -v --rmi all || true
	docker-compose -f docker-compose.yml -f docker-compose.prod.yml down -v --rmi all || true
	@echo "Clean complete!"