# The API sheds load instead of queueing it

summary: The API bounds in-flight requests and refuses the excess with HTTP 429, because unbounded queueing turned overload into a collapse it never recovered from.

## Context

The capacity baseline in `docs/benchmarks.md` shows the service handling 550
concurrent users and then, at 600, failing in a way that does not resemble
gradual degradation. A tenth of requests hit the client's 60 s timeout, and every
subsequent step ran at essentially zero throughput — the offered load kept rising
in the same gradual increments it always had, and the service never came back.

The mechanism is unbounded queueing. Nothing was rejected: every connection was
accepted and queued. Once the backlog grew longer than the client timeout, each
request that finally reached a handler had already been abandoned, so the service
spent its capacity producing answers nobody was waiting for. That is a stable
state, not a transient one, which is why lower load later in the run did not help.

Two things were missing, and both were needed:

- Nothing bounded the work in progress, so the queue could grow without limit.
- Nothing bounded a single outbound RPC. rpcx exposes no per-call timeout option;
  a deadline has to come from the context, and none was set, so an API goroutine
  waited indefinitely on a slow logic call.

## Decision

**Bound in-flight requests and refuse the excess.** A gin middleware
(`pkg/middleware/admission.go`) holds a buffered channel of slots. A request takes
a slot on the way in and returns it on the way out. A request that cannot get one
within `acquireTimeout` is refused with HTTP 429 before it reaches any business
logic. Configured under `[api-admission]`, and switchable so the same build can be
measured with it and without.

**Bound every outbound RPC.** `pkg/middleware/rpcx_client.go` applies a context
deadline, default 2 s, to all fifteen call sites. An existing deadline is
tightened, never relaxed.

## Why a wait for a slot, rather than a fixed concurrency limit

A plain limit needs the right number, and the right number is not knowable in
advance — the two published runs put the knee at 350 and at 2,000 VUs for
different request mixes on the same deployment.

Waiting for a slot turns the limit into a signal instead of a verdict. While the
service keeps up, slots free in microseconds and nothing waits, so the limit can
be set generously without costing throughput. When the service falls behind,
waits lengthen, and requests are refused in the order they would otherwise have
queued. The limit still has to be calibrated, but being wrong about it degrades
gracefully in both directions.

## What this does not do

**It does not raise throughput.** Past saturation the service was already
finishing less work than it accepted. Shedding does not change the ceiling; it
changes what happens above it. Any measurement claiming shedding improved
capacity should be treated as a measurement error.

## What it measured

An A/B on the same build, same ladder, differing only in configuration
(`docs/benchmarks.md`, evidence in `loadtest/reports/ab/`):

| | no deadline, no shedding | 2 s deadline + shedding |
|---|---|---|
| Capacity | 400 VUs | 400 VUs |
| p95 at 2,800 VUs | 3,061 ms | 806 ms |
| Served throughput at 2,800 VUs | 3,561 req/s | 2,666 req/s |
| Shed at 2,800 VUs | — | 63.7% |

Capacity is identical, as predicted. Latency past capacity is bounded — p95 stays
between 459 ms and 806 ms across a 7× range of offered load instead of climbing
past 3 s — and the price is 25% less useful work at the top of the range. That is
the trade this decision makes, and it is worth restating that it *is* a trade.

**One claim remains unproven.** The collapse this decision was taken to prevent
could not be reproduced on the hardware the A/B ran on: without a deadline or
shedding, the service degraded smoothly to 2,800 VUs rather than collapsing. So
the measured benefit is bounded latency, not averted collapse.

## Consequences

- 429 is a new response status. `tools/response.go` previously answered every
  request with HTTP 200 regardless of outcome, which is why the load test could
  not see refusals at all; codes describing the state of the service now carry a
  matching status (401, 429, 503).
- Capacity accounting changes with it. A shed request is not a failed request:
  see [0008](./0008-capacity-is-the-last-step-that-holds-an-slo.md).
- The RPC deadline propagates. rpcx forwards the remaining time to the server as
  request metadata, so logic stops working on requests whose callers have gone —
  which is the specific waste that kept the collapse alive.
- Adding a deadline exposed a bug it would otherwise have made worse: seven of
  the API's RPC wrappers discarded the call error, and the zero value of the
  reply's `Code` field is `CodeSuccess`, so a failed call was reported to the
  client as a success. Previously masked, because without a deadline the call
  never returned at all.
- `maxInFlight` is a calibrated number, not a derived one. It has to be re-checked
  when the hardware or the request mix changes, and the value in config is only
  meaningful alongside the run that produced it.
