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

## Documentation

- **Multi-Container Guide**: See [README-compose.md](./README-compose.md)
- **Original Documentation**: See below or visit [github.com/LockGit/gochat](https://github.com/LockGit/gochat)

---
