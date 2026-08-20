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

## Known gaps

Recorded here because they are structural, not bugs to be fixed in passing:

- **No load shedding.** Past its knee the system queues without bound and
  collapses rather than degrading. Measured and analysed in `docs/benchmarks.md`.
- **Messages are lost if a connect instance dies while they are queued.** `task`
  resolves `serverId` at delivery time; if that instance is gone, the message is
  dropped rather than redelivered to wherever the user reconnected
  (`task/push.go`).
- **The tracer is shut down immediately after startup.** `Run()` registers
  `defer shutdown()` and then returns, while the process waits for a signal in
  `main.go`. Affects logic and connect.
- **Prometheus scrapes logic through a static target**, so with several logic
  replicas its logic metrics describe whichever replica DNS resolves to.

## Where decisions live

Decision records are in `docs/adr/`, one decision per file. Search them rather
than reading the directory.
