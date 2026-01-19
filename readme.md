# GoChat - Multi-Container Deployment

> This is a fork of [LockGit/gochat](https://github.com/LockGit/gochat) with Docker Compose multi-container deployment support.

## What's New

This fork adds **production-ready multi-container deployment** to the original gochat project:

- ✅ **8 Independent Containers**: Each service (etcd, redis, logic, connect-ws, connect-tcp, task, api, site) runs in its own container
- ✅ **Horizontal Scaling**: Scale Logic, Connect, Task, and API services independently
- ✅ **Docker Compose**: One-command deployment with `make compose-dev` or `make compose-prod`
- ✅ **Auto-Configuration**: Container IPs automatically registered to etcd for proper RPC communication
- ✅ **Health Checks**: Each service monitors its own health with auto-restart
- ✅ **Dev/Prod Configs**: Separate configurations for development and production environments

### Quick Start

```bash
# Start all services
make compose-dev

# Or manually
docker compose -f docker-compose.yml -f deployments/docker-compose.dev.yml up

# Visit the chat app
http://localhost:8080
```

### Scaling Services

```bash
# Scale specific services
docker compose up --scale logic=3 --scale connect-ws=2 --scale task=2
```

### Production Deployment

```bash
make compose-prod HOST_IP=<your-server-ip>
```

## Architecture

```
├── etcd          → Service discovery
├── redis         → Message queue & cache
├── logic × N     → Business logic RPC (scalable)
├── connect-ws × N→ WebSocket handler (scalable)
├── connect-tcp × N→ TCP handler (scalable)
├── task × N      → Message worker (scalable)
├── api × N       → REST API (scalable)
└── site          → Frontend
```

## Load Testing Model

This repo includes a k6-based load testing model under `loadtest/`. It uses a step-based ramp model with explicit phases:

- ramp: VU ramp up between levels
- warmup: excluded from steady-state metrics
- steady: included in SLO and capacity analysis

### Core configuration

All parameters are set via environment variables in `loadtest/scripts/lib/config.js`:

- `K6_START_VUS`, `K6_END_VUS`, `K6_STEP_VUS`
- `K6_STEP_DURATION`, `K6_RAMP_DURATION`, `K6_WARMUP_DURATION`
- Fixed mode: `K6_VUS` + `K6_DURATION`
- SLO overrides: `SLO_P95_MS`, `SLO_P99_MS`, `SLO_ERROR_RATE`, `SLO_TIMEOUT_RATE`

### Step metrics and capacity logic

Steady-state metrics are tagged by `{step, vus}` and rolled up by `loadtest/scripts/lib/step-report.js`. The report computes:

- capacity: last step that passes SLO
- bottleneck: first step that fails SLO

### Full system model

`loadtest/scripts/full-system.js` runs HTTP + WebSocket concurrently:

- HTTP scenario uses a mixed request ratio per iteration:
  - checkAuth x1
  - push x5
  - pushRoom x5
  - pushCount x2
  - getRoomInfo x1
- WebSocket VUs are 25% of HTTP VUs, with 20-40s sessions.

### Capacity baseline model

`loadtest/scripts/capacity-baseline.js` runs step-based HTTP only, per iteration:

- login x1
- checkAuth x1 (if token)
- push x3
- pushRoom x3
- pushCount x1
- getRoomInfo x1

### Single-endpoint models

Scenarios under `loadtest/scripts/scenarios/` (login, register, websocket, push, etc.) reuse the same step model and steady-state reporting.

## Documentation

- **Multi-Container Guide**: See [README-compose.md](./README-compose.md)
- **Original Documentation**: See below or visit [github.com/LockGit/gochat](https://github.com/LockGit/gochat)

---
