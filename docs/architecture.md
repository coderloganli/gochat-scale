# Architecture

## What this is

GoChat is a real-time chat backend: users hold a long-lived WebSocket or TCP
connection, send messages through a REST API, and receive them pushed down the
connection they already hold. It is a fork of
[LockGit/gochat](https://github.com/LockGit/gochat); what this repository adds is
the part that makes the design testable — containerised deployment, service
discovery that works when services are replicated, metrics and traces, a load
test model with published results, and integration tests that run against real
containers in CI.

The unit of interest is the split between *connection state* and *business
logic*. Connections are expensive and sticky; logic is cheap and stateless.
Keeping them in separate services is what allows either one to be scaled without
the other, and most of the structure below follows from that.

## Shape

One binary, six roles. `main.go` dispatches on `-module`:

```
gochat -module {logic|connect_websocket|connect_tcp|task|api|site}
```

| Directory | Role |
|---|---|
| `api/` | REST gateway. Terminates client HTTP, authenticates, calls logic over RPC. |
| `connect/` | Holds the long-lived connections. WebSocket (`websocket.go`) and TCP (`server_tcp.go`) share the bucket/room machinery. |
| `logic/` | Business logic and the only service that touches the database. Exposes RPC to api and connect; publishes outbound messages to RabbitMQ. |
| `task/` | Consumes RabbitMQ and delivers each message to the connect instance that holds the recipient. |
| `site/` | Serves the static frontend. |
| `proto/` | Wire types shared across services. |
| `db/` | Connection pool and SQL migrations. |
| `tools/` | Redis and RabbitMQ clients, hashing, network helpers. |
| `pkg/` | Cross-cutting concerns: metrics, tracing, logging, password hashing, TCP framing. |

### How a message travels

```
client --HTTP--> api --RPC--> logic --AMQP--> task --RPC--> connect --WS--> client
```

1. `api` authenticates the caller (see the auth cache in
   `api/cache/auth_cache.go`) and calls `logic.Push` over rpcx.
2. `logic` looks up which connect instance holds the recipient — Redis maps
   `userId -> serverId` — and publishes to the `gochat.direct` exchange with a
   routing key naming the message kind.
3. `task` consumes the queue, resolves `serverId` to a live connect instance
   through etcd, and calls that instance's RPC.
4. `connect` finds the recipient's channel in its buckets and writes the frame.

Room broadcasts skip the `userId -> serverId` lookup: `task` fans the message out
to every connect instance, and each one delivers to whichever members of that
room it happens to hold.

### What holds state

| Store | Holds |
|---|---|
| PostgreSQL | User accounts. The only durable application state. |
| Redis | Sessions (`sess_*`), the `userId -> serverId` routing map, room membership and online counts. |
| RabbitMQ | Messages in flight between logic and task. |
| etcd | Service registry. Every logic and connect instance registers itself; api, connect and task discover through it. |
| connect process memory | The connections themselves, sharded into buckets. Lost on restart, by design — clients reconnect. |

## Boundaries

Nothing here calls out to a third-party service. Everything the system talks to
runs in the same compose project: PostgreSQL, Redis, RabbitMQ, etcd, and the
observability stack (Prometheus, Grafana, Jaeger).

Between services, two protocols and one queue:

- **rpcx over etcd** for synchronous calls (api → logic, connect → logic,
  task → connect). Service discovery is dynamic, so replicas can come and go.
- **AMQP** for asynchronous fan-out (logic → task), through one direct exchange
  with three queues split by message kind.
- **HTTP** at the edge only.

The frontend in `site/` is a prebuilt React bundle committed to the repository;
it is not built from source here.

## Conventions that are not obvious from the code

**Configuration.** TOML files under `config/{dev,staging,prod}/`, merged by Viper
at startup. `RUN_MODE` selects the directory. Credentials are the exception:
`DB_PASSWORD` and the other `DB_*` variables override the file, so deployments
inject secrets from the environment rather than committing them. The values in
the TOML files are development defaults and nothing else.

**Service startup order is not assumed.** `db.Init` retries, and compose gates
services on healthchecks. A service that cannot reach its dependencies panics
rather than starting in a degraded state.

**The database is only reachable from logic.** No other service imports `db/`.
Anything that needs user data goes through a logic RPC. This is what keeps the
database a single shared dependency rather than a coupling between every service.

**Schema changes are SQL files, not AutoMigrate.** `db/migrations/*.sql` runs in
the postgres container on first start, or via `make db-migrate`. The table is
named `users`, not `user`, because `user` is reserved in PostgreSQL.

**The API answers HTTP 200 to everything.** Success, auth failure, bad input and
upstream failure all come back as 200 with the real status in a `code` field in
the JSON body (`tools/response.go`). This is inherited from upstream and is worth
knowing before reading any handler, because it means an HTTP client — including
the load test — cannot tell a failure from a success without parsing the body.
See the caveat in `docs/benchmarks.md`.

**Metrics ports are fixed per role**: logic 9091, connect-ws 9092, connect-tcp
9093, task 9094, api 9095, site 9096. Prometheus scrapes them over the compose
network. Only the dev overlay publishes them to the host — the base compose file
does not, because a fixed host port makes `--scale` fail.

**Tests are split by what they need.** `go test -short` runs only what needs no
infrastructure; `tests/integration/` runs against a live compose stack and is
skipped under `-short`. CI runs both, the second against containers it starts
itself.

**Load test results are evidence, not decoration.** `loadtest/reports/*-steps.json`
is tracked in git so that any figure quoted in the documentation can be checked
against the run that produced it. See `docs/benchmarks.md`.

**No module handles its own signals.** Each entry point is
`Start() (lifecycle.Stopper, error)`: it starts its work, returns a way to stop
it, and does not block. `main.go` installs the one signal handler and calls
`pkg/lifecycle.WaitAndStop`, which runs the stopper under a five second cap. That
cap is a constant, not a configuration key. `connect` uses it to leave the
cluster in order — deregister from etcd, refuse new connections, send every live
connection a 1001 close frame, let the existing disconnect path clear Redis —
which is the difference between a restart that costs a two-minute routing hole
and one that costs a reconnect. `GOCHAT_GRACEFUL_SHUTDOWN=false` restores the old
hard-kill behaviour and exists only to produce the control arm of the measurement
in `docs/benchmarks.md`.
## Deployment targets

There are two, and neither is a staging post on the way to the other.

**Docker Compose** (`docker-compose.yml` plus overlays in `deployments/`) is the
one the development loop, the CI integration tests and the load tests run on. It
starts fastest and it is where every number in `docs/benchmarks.md` came from.

**Kubernetes** (`deployments/k8s/`, Kustomize, on a local kind cluster) is where
the questions Compose cannot ask get answered: what readiness is worth when a
dependency fails, and which signal an autoscaler should watch. See
`docs/kubernetes.md`.

The same image and the same configuration mechanism serve both; only the way
they are scheduled differs. Both bind the same host ports, so only one can run at
a time.

Two things behave differently by necessity rather than by choice, each with a
decision record: Prometheus discovers pods under Kubernetes and reads a static
list under Compose (ADR 0013), and only Kubernetes autoscales (ADR 0012).

## Known gaps

Recorded here because they are structural, not bugs to be fixed in passing:

- **Messages queued for a departing connect instance are still lost.** `task`
  resolves `serverId` at delivery time; if that instance has gone, the message is
  dropped rather than redelivered to wherever the user reconnected
  (`task/push.go`). Graceful shutdown narrows the window from the two-minute etcd
  TTL to the length of a shutdown, but does not close it.
- **Shutdown does not drain queued messages.** A message already sitting in a
  channel's `broadcast` buffer when the signal arrives may not be written. The
  five second cap covers the close handshake only — see
  `docs/adr/0014-a-departing-connect-instance-deregisters-before-it-closes-connections.md`.
- **The bundled frontend does not reconnect.** connect now sends a 1001 close
  frame so a client can tell a planned shutdown from a failure, but `site/` is a
  prebuilt bundle with no source here and does not act on it.
- **`task` leaks an rpcx client per connect registration, every watch event.**
  `setInstanceMap` (`task/rpc.go`) replaces the whole `serverId -> instances` map
  with freshly built clients and never closes the ones it drops, so each client's
  connection and heartbeat goroutine is abandoned. Registrations refresh on a
  one-minute interval (ADR 0002), so this accrues for the life of the process.
  Fixing it needs care rather than effort: a push may be in flight on a client at
  the moment it is replaced, so they have to be closed after a grace period
  rather than immediately.
- **Every role hard-depends on etcd at process start, whether it uses it or
  not.** `connect/server_tcp.go` has a package-level `init()` calling
  `rpc.InitLogicRpcClient()`, and `main.go` imports `gochat/connect` so that one
  binary can be any role — so that `init()` runs in all six. It calls
  `logrus.Fatalf` when etcd is unreachable, which is why `site`, which serves
  static files and talks to nothing, exits on startup if etcd is not up. Both
  deployments work around it rather than fix it: Compose with `depends_on`,
  Kubernetes with an init container. The real problem is network I/O in a package
  `init()`.
- **Under Compose, Prometheus scrapes logic through a static target**, so with
  several logic replicas its logic metrics describe whichever replica DNS
  resolves to. Compose has no discovery mechanism to fix this with. The
  Kubernetes deployment does not have the gap — it discovers pods and labels
  every series with its pod (ADR 0013).

## Where decisions live

Decision records are in `docs/adr/`, one decision per file. Search them rather
than reading the directory.
