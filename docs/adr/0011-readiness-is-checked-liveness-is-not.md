# Readiness is checked, liveness is not

summary: Services answer `/ready` only once their dependencies respond, while `/health` keeps returning 200 unconditionally, because the two probes answer different questions and conflating them turns a dependency blip into a restart loop.

## Context

Four of the six roles served `/health` on a metrics port
(`pkg/metrics/server.go`) — logic, connect-ws, connect-tcp and task. It returns
200 the instant the listener is up, and keeps returning 200 forever. Nothing
behind it is checked.

The other two had no health endpoint at all. `api` and `site` never called
`metrics.StartMetricsServer`; each served `/metrics` from its own application
server instead (`api/router/router.go:39`, `site/site.go:51`), so ports 9095 and
9096 were not listening despite `docs/architecture.md` recording them as a
convention. That was fixed as part of this work: all six roles now start the
shared metrics server, and `/metrics` moved onto it, so there is one place per
role that answers for the process's health.

Kubernetes asks two separate questions, and the existing endpoint can only
honestly answer one of them:

- *Liveness*: is this process wedged, such that killing it would help?
- *Readiness*: should this pod be receiving traffic right now?

Pointing both probes at `/health` would produce manifests that look complete and
behave exactly as if no readiness probe were configured at all. A `logic` pod
would enter the Service endpoints before it had a database connection; a
`connect` pod would take WebSocket clients before it had registered itself in
etcd, which is what makes it reachable for message delivery in the first place.

The same defect already existed in Compose, less visibly. Every application
service's healthcheck is `pgrep -f 'gochat.*<module>'`, true from process start,
and `depends_on: condition: service_healthy` gates the entire startup order on
it. The ordering was decorative.

## Decision

A new package `pkg/health` holds a registry of named checks:

```go
func Register(name string, check func(context.Context) error)
func Check(ctx context.Context) (ok bool, failures map[string]string)
```

`pkg/metrics/server.go` gains `/ready`, which runs every registered check under a
2-second timeout and returns 200, or 503 with the failing check names in the
body.

`/health` is left exactly as it was, and stays the liveness probe.

Each role registers what it genuinely needs before it can serve:

| Role | Checks |
|---|---|
| logic | database ping, Redis ping, RabbitMQ channel open, etcd registration completed |
| connect-ws, connect-tcp | Redis ping, etcd registration completed, buckets initialised |
| task | RabbitMQ channel open, connect instances discovered |
| api | logic resolvable through discovery |
| site | none — it serves static files |

Two of these needed the code to change before they could be honest. `etcd
registration completed` is set inside the RPC server goroutine immediately after
`RegisterName` returns, because `logic` and `connect` both start that server with
`go` and return before registration has happened. And `task`'s discovery check
required seeding the instance map from the initial discovery result: it was
previously populated only by the watcher, so before the first watch event `task`
had nothing to report and was silently dropping the messages it consumed.

Compose healthchecks are switched from `pgrep` to the same `/ready` endpoint, so
that both deployments gate on the same definition of ready.

## Why

**Because the two questions have different right answers when a dependency
fails.** If PostgreSQL goes away, a `logic` pod should stop receiving traffic —
it cannot serve. It should *not* be restarted: restarting it will not bring
PostgreSQL back, and a restart loop across every replica turns a recoverable
outage into an outage plus a thundering herd of reconnects the moment the
database returns. Readiness withdraws; liveness holds.

That asymmetry is the whole reason to keep `/health` deliberately dumb. A
liveness probe that checks dependencies is a well-known way to convert a
dependency incident into a cluster-wide crash loop.

**Because `etcd registration completed` is the check that actually matters for
connect.** A connect pod that is accepting WebSocket connections but has not
registered in etcd is a black hole: `task` resolves a recipient's `serverId`
through the registry (see ADR 0002), cannot find that instance, and the message
is dropped. The pod looks healthy from outside and silently loses traffic. That
state is short — but it is exactly the window a rolling update walks every pod
through.

## Alternatives

**Point both probes at the existing `/health`.** Zero work, and produces a
deployment whose probes are theatre. Rejected — the point of the exercise was the
probes.

**Readiness probe as a TCP socket check.** Slightly better than `/health`, since
it at least waits for the listener. Still says nothing about whether the process
can do anything once the connection is accepted.

**Check dependencies in the liveness probe as well.** Simpler — one endpoint, one
answer. Rejected for the crash-loop reason above.

**A separate health port or a dedicated HTTP server.** The metrics port is
already excluded from application traffic and is already the thing Prometheus
talks to. Adding a second port would have bought nothing — and making api and
site start the metrics server they were supposed to have all along was cheaper
than special-casing them.

## Consequences

- A dependency outage now removes pods from Service endpoints. That is intended,
  and it is more moving parts than always answering 200 — a bug in a check
  function can take the service out while the service itself is fine.
- The checks run on every probe interval (5s per pod). They are cheap — a PING, a
  ping, a flag read — but they are not free, and a check that blocks would stall
  the readiness endpoint. Hence the 2-second timeout around the whole set.
- Startup ordering in Compose stops being decorative, which means a service that
  never becomes ready now blocks the things that depend on it instead of letting
  them start and fail later. That is the intended behaviour and it makes a
  misconfiguration louder.
- `site` registers no checks, so its `/ready` is equivalent to `/health`. That is
  honest for what it serves: once the listener is up, a static file needs nothing
  else. It is not true that site depends on nothing, though - see below.
- **Every role depends on etcd at process start, whether it uses it or not.**
  `connect/server_tcp.go` has a package-level `init()` that calls
  `rpc.InitLogicRpcClient()`, and `main.go` imports `gochat/connect` in order to
  offer the module switch, so that `init()` runs in all six roles. The client
  calls `logrus.Fatalf` when etcd is unreachable, so `site` - which touches
  neither etcd nor logic - exits on startup if etcd is not up yet. Readiness
  cannot help with this, because the process is gone before it could answer; it
  is handled in the deployment, by making site wait for etcd like everything
  else. Doing network I/O in a package `init()` is the underlying problem and it
  is not fixed here.
