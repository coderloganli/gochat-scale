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

### Kubernetes (local kind cluster)
```bash
make k8s-cluster-up           # create the kind cluster from the committed config
make k8s-up                   # build+load the image, deploy everything, wait for rollouts
make k8s-status               # pods, services and autoscalers
make k8s-hpa                  # watch the autoscalers
make k8s-down                 # remove the workloads, keep the cluster
make k8s-cluster-down         # remove the cluster
```

The second deployment target; see `docs/kubernetes.md`. Compose stays the one the
development loop, the CI integration tests and the load tests use. Both bind the
same host ports, and the kind node holds them for its whole lifetime, so only one
can run at a time - `docker stop gochat-control-plane` frees them without
destroying the cluster.

### Building
```bash
make build-binary             # Build Linux binary to bin/gochat
make build-image              # Build Docker image (TAG=dev for the k8s tag)
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

### Shutdown

No module handles its own signals. Each entry point is
`Start() (lifecycle.Stopper, error)`: it starts its work, returns a way to stop
it, and does not block. `main.go` installs the one signal handler and calls
`pkg/lifecycle.WaitAndStop`, which runs the stopper under a five second cap.
That cap is a constant, not a configuration key.

`connect` uses it to leave the cluster in order — deregister from etcd, refuse
new connections, send every live connection a 1001 close frame, let the existing
disconnect path clear Redis — which is the difference between a restart that
costs a two-minute routing hole and one that costs a reconnect. See
`docs/adr/0014`.

- `GOCHAT_GRACEFUL_SHUTDOWN=false` restores the old hard-kill behaviour. It is a
  measurement switch for the A/B in `docs/benchmarks.md`, not an operational one.
- `make drain-demo` runs both arms and prints the comparison.
- `/health` on the metrics port answers 503 once a shutdown has begun, while
  `/metrics` keeps serving. Alive and ready are now two different questions.
- Never add a `RegisterOnShutdown` hook to an rpcx server. In v1.7.4 the
  `onShutdown` slice is appended to and never read, so the callback never runs.
  `Shutdown` is what deregisters.

## Docker Compose

Base: `docker-compose.yml`
Overlays: `deployments/docker-compose.{dev,prod,test}.yml`

Scale services: `docker compose up --scale logic=3 --scale connect-ws=2`

`make compose-dev` does not rebuild. The healthchecks now curl `/ready`, and
`curl` was only added to the runtime image recently, so a stale local image fails
every healthcheck with no obvious cause. Use `make compose-dev-build` after
pulling changes that touch `docker/Dockerfile`.

## Health and readiness

Every role serves three endpoints on its metrics port (9091-9096):

- `/metrics` - Prometheus.
- `/health` - liveness. Returns 200 unconditionally, on purpose. A dependency
  being down is not a reason to restart a process, and a liveness probe that
  checks dependencies turns an outage into a cluster-wide restart loop.
- `/ready` - readiness. Runs the checks that role actually needs and returns 503
  naming what failed. Registered through `pkg/health`.

The two answer different questions and must not be conflated; see
`docs/adr/0011`. Compose healthchecks and Kubernetes readiness probes both use
`/ready`, so the two deployments agree on what "ready" means.

## Metrics

All services expose Prometheus metrics. Scraped by Prometheus, visualized in Grafana dashboards at `deployments/grafana/`.

Under Compose, Prometheus reads a static target list. Under Kubernetes it
discovers pods by annotation, which is what gives every series a `pod` label -
and that label is what lets prometheus-adapter serve `custom.metrics.k8s.io` for
the autoscalers (`docs/adr/0012`, `docs/adr/0013`).

A declared metric is not an exported one: a `GaugeVec` with no observed label
values exports no series at all. `gochat_connections_active` was declared for a
long time with no writer, which is invisible until something tries to read it.

## Documentation

- `docs/product.md` - what the product is, who it is for, and what it
  deliberately does not do.
- `docs/architecture.md` - the shape of the system, what holds state, and the
  known structural gaps.
- `docs/adr/` - one decision per file. Search it rather than reading it.
- `docs/benchmarks.md` - measured capacity and bottleneck analysis.
- `docs/kubernetes.md` - the Kubernetes deployment: how to run it, and what is
  deliberately not production-grade.

When a decision changes, edit its record in place rather than adding a new one.

## Coding Style

- All code and comments must be written in English.
