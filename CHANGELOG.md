# GoChat Multi-Container Deployment - Change Log

## 2026-01-25

### Session: Added Message Persistence and History API

**Feature: Chat Message Persistence**
- Messages are now persisted to SQLite database before delivery
- Supports both single chat and room messages
- Synchronous persistence ensures messages are saved reliably

**logic/dao/message.go** (new file)
- Created `Message` model with fields: Id, FromUserId, FromUserName, ToUserId, ToUserName, RoomId, MessageType, Content, CreateTime
- Added `Add()` method for inserting messages
- Added `GetSingleChatHistory()` for retrieving chat history between two users
- Added `GetRoomHistory()` for retrieving room message history

**db/db.go**
- Migrated from SQLite to PostgreSQL
- Added `User` struct for auto-migration
- Added `Message` struct for auto-migration
- Added auto-migration for user and message tables on database initialization
- Added connection pool configuration (maxIdleConns, maxOpenConns, connMaxLifetime)

**config/config.go**
- Added `CommonPostgreSQL` struct with database connection settings

**config/{dev,staging,prod}/common.toml**
- Added `[common-postgresql]` section with host, port, user, password, dbname, sslmode, connection pool settings

**docker-compose.yml**
- Added PostgreSQL service (postgres:15-alpine) on 172.28.0.13
- Added `postgres-data` volume for data persistence
- Updated logic service to depend on postgres

**logic/rpc.go**
- Modified `Push()` to persist single chat messages before publishing to RabbitMQ
- Modified `PushRoom()` to persist room messages before publishing to RabbitMQ
- Added `GetSingleChatHistory()` RPC method for retrieving single chat history
- Added `GetRoomHistory()` RPC method for retrieving room message history

**proto/logic.go**
- Added `GetSingleChatHistoryRequest` with CurrentUserId, OtherUserId, Limit, Offset
- Added `GetRoomHistoryRequest` with RoomId, Limit, Offset
- Added `MessageItem` for representing messages in responses
- Added `GetMessageHistoryResponse` with Code and Messages array

**api/handler/message.go** (new file)
- Created `GetSingleChatHistory()` HTTP handler
- Created `GetRoomHistory()` HTTP handler

**api/rpc/rpc.go**
- Added `GetSingleChatHistory()` RPC client method
- Added `GetRoomHistory()` RPC client method

**api/router/router.go**
- Added route `POST /push/history/single` for single chat history
- Added route `POST /push/history/room` for room chat history

**tests/helpers/api_client.go**
- Added `GetSingleChatHistory()` test client method
- Added `GetRoomHistory()` test client method

**tests/integration/message_history_test.go** (new file)
- Added integration tests for single chat history retrieval
- Added integration tests for room chat history retrieval
- Added tests for pagination and invalid token handling

**scripts/perf-optimization.sh** (new file)
- Created functional test script for message persistence feature
- Tests user registration, message sending, and history retrieval
- Validates both single chat and room message persistence

---

## 2025-12-30

### Session 3: Added CI/CD Pipeline

**Makefile**
- Added `test` - Run all tests with race detector
- Added `test-coverage` - Generate HTML coverage report
- Added `test-unit` - Run unit tests only
- Added `test-integration` - Run integration tests with Docker
- Added `fmt` - Format code with go fmt
- Added `fmt-check` - Check if code is properly formatted
- Added `vet` - Run go vet static analysis
- Added `lint` - Run golangci-lint
- Added `build-binary` - Build gochat binary
- Added `build-image` - Build Docker image

**.github/workflows/ci-cd.yml**
- Created GitHub Actions CI/CD workflow
- Test job: go fmt, go vet, tests with Redis service container
- Build job: Docker image with caching
- Push job: Push to Docker Hub with multiple tags (latest, branch, git-sha)
- Deploy jobs: Optional deployment to dev/staging/prod (commented out, requires server setup)

**docker-compose.test.yml**
- Created test environment with Redis and etcd service containers
- Test runner service for integration tests

**docker/Dockerfile.test**
- Created test Dockerfile for running tests in Docker

**scripts/deploy.sh**
- Created deployment script for manual deployments
- Supports dev, staging, prod environments
- Pulls image, stops old services, starts new services

**config/staging/**
- Created staging configuration (copied from dev)

**docker-compose.staging.yml**
- Created staging environment override
- Uses staging config with info log level

**README.md**
- Added CI/CD Pipeline section with workflow description
- Added GitHub Secrets setup instructions
- Added branch strategy documentation
- Added manual deployment commands
- Added testing and building commands to Commands section

---

### Session 2: Removed entrypoint.sh

**tools/network.go**
- Added `GetContainerIP()` - Returns container's actual IPv4 address
- Added `GetServiceAddress()` - Replaces 0.0.0.0 with container IP for etcd registration

**logic/publish.go**
- Changed `addRegistryPlugin()` - ServiceAddress now uses `tools.GetServiceAddress(network, addr)`

**connect/rpc.go**
- Changed `addRegistryPlugin()` - ServiceAddress now uses `tools.GetServiceAddress(network, addr)`

**docker/Dockerfile**
- Removed entrypoint.sh copy and chmod commands
- Changed from `ENTRYPOINT ["/app/entrypoint.sh"]` to `CMD ["/app/gochat", "-module", "api"]`

**docker-compose.yml**
- All services: Removed `ETCD_HOST` and `REDIS_HOST` environment variables
- All services: Changed command from `["-module", "xxx"]` to `["/app/gochat", "-module", "xxx"]`

**docker/entrypoint.sh**
- Deleted (no longer needed)

---

### Session 1: Cleaned Up Legacy Files

**Removed directories:**
- `docker/dev/` - Supervisord configs for development (10 files)
- `docker/prod/` - Supervisord configs for production (10 files)

**Removed files:**
- `run.sh` - Legacy single-container deployment script
- `reload.sh` - Legacy reload script

**README.md**
- Removed "Legacy Deployment" section mentioning `run.sh`

---

## Future Updates

Add entries above in reverse chronological order (newest first).
