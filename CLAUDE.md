# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

GoChat is a scalable real-time chat application with microservices architecture. It uses etcd for service discovery, RabbitMQ for message fan-out, Redis for session storage, PostgreSQL for user data, and supports horizontal scaling of all application services.

## Common Commands

### Development
```bash
make compose-dev              # Start all services (dev mode)
make compose-dev-down         # Stop dev environment
make compose-logs             # Follow logs from all services
```

### Testing
```bash
make test                     # All tests with race detector
make test-unit                # Unit tests only (-short flag)
make test-coverage            # Generate coverage.html report
go test -v -run TestName ./path/to/package  # Single test
```

### Code Quality
```bash
make fmt                      # Format code
make vet                      # Run go vet
make lint                     # Run golangci-lint
```

### Load Testing (k6)
```bash
make loadtest-smoke                       # Quick 30s smoke test
make loadtest-capacity                    # Step-based capacity test
make loadtest-full K6_VUS=100 K6_DURATION=5m
make loadtest-{login,register,websocket,push,pushroom}  # Single endpoint
```

Measured results and bottleneck analysis: `docs/benchmarks.md`. Tracked
evidence lives in `loadtest/reports/*-steps.json`; do not quote a figure
that no tracked file backs.

### Building
```bash
make build-binary             # Build Linux binary to bin/gochat
make build-image              # Build Docker image
```

## Architecture

### Service Startup
All services share a single binary (`main.go`), started with module flag:
```bash
gochat -module {logic|connect_websocket|connect_tcp|task|api|site}
```

### Services & Ports

| Service | Ports | Role |
|---------|-------|------|
| logic | 6900-6901 (RPC), 9091 (metrics) | Business logic, auth, database |
| connect-ws | 7000 (WS), 9092 (metrics) | WebSocket connections |
| connect-tcp | 7001-7002, 9093 (metrics) | TCP connections |
| api | 7070, 9095 (metrics) | REST API gateway |
| task | 6923 (RPC), 9094 (metrics) | Async message processor |
| site | 8080, 9096 (metrics) | Frontend static files |
| etcd | 2379 | Service discovery |
| postgres | 5432 | User accounts (shared by all logic replicas) |
| redis | 6379 | Pub/sub, cache |

### Request Flow
1. Client → API (7070) or Connect-WS (7000)
2. API/Connect → Logic (RPC) for auth/business logic
3. Logic → Redis (pub/sub) → Task (async processing)
4. Task → Connect (RPC) → Client delivery

### Key Directories
- `logic/` - Business logic service (auth, database via GORM/PostgreSQL)
- `connect/` - Connection handlers (websocket.go, server_tcp.go, room.go)
- `api/handler/` - REST endpoints (user.go, push.go)
- `task/` - Message queue processor (queue.go, push.go)
- `proto/` - Message protocol definitions
- `config/` - TOML configs by environment (dev/, prod/, staging/)
- `loadtest/scripts/` - k6 test scenarios

### Configuration
TOML configs in `config/{env}/`:
- `common.toml` - etcd/redis/postgres/rabbitmq connection
- `{service}.toml` - Per-service config (ports, resources)

Environment loaded via Viper in `config/config.go`.

### Database

User accounts live in PostgreSQL. All logic replicas share one database, which
is what makes `--scale logic=N` correct: any replica can serve any user.

- Schema lives in `db/migrations/`, applied automatically by the postgres
  container on first start, or manually with `make db-migrate`.
- Connection settings come from `[common-db]` in `config/{env}/common.toml`.
  `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME` and `DB_SSLMODE`
  override them, which is how deployments inject credentials.
- `make db-shell` opens a psql session against the running container.

### Passwords

Passwords are hashed with bcrypt in `pkg/password`. Never store or compare a
plaintext password. `GOCHAT_BCRYPT_COST` lowers the cost factor for load tests
so that the login path measures the service rather than the KDF; it must not be
lowered outside load testing.

### Overload behaviour

The API bounds in-flight requests and refuses the excess with HTTP 429
(`pkg/middleware/admission.go`), and every outbound RPC carries a deadline
(`pkg/middleware/rpcx_client.go`). Both exist because unbounded queueing turned
overload into an unrecoverable collapse; see `docs/adr/0009`.

- `[api-admission]` in `config/{env}/api.toml`; `ADMISSION_ENABLED` and
  `ADMISSION_MAX_IN_FLIGHT` override it so one build can be measured both ways.
- `[common-rpc] timeout`, overridable with `RPC_TIMEOUT`.
- `maxInFlight` is calibrated by load test, not derived. Re-check it when the
  hardware or the request mix changes.

A shed request is not a failed request. Keep the two apart in any metric or
report, or graceful degradation will look worse than collapsing.

## Docker Compose

Base: `docker-compose.yml`
Overlays: `deployments/docker-compose.{dev,prod,test}.yml`

Scale services: `docker compose up --scale logic=3 --scale connect-ws=2`

## Metrics

All services expose Prometheus metrics. Scraped by Prometheus, visualized in Grafana dashboards at `deployments/grafana/`.

## Documentation

- `docs/product.md` - what the product is, who it is for, and what it
  deliberately does not do.
- `docs/architecture.md` - the shape of the system, what holds state, and the
  known structural gaps.
- `docs/adr/` - one decision per file. Search it rather than reading it.
- `docs/benchmarks.md` - measured capacity and bottleneck analysis.

When a decision changes, edit its record in place rather than adding a new one.

## Coding Style

- All code and comments must be written in English.
