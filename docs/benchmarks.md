# Benchmarks

Measured results for GoChat, and what they say about where the system breaks.

Every number here comes from a file in `loadtest/reports/`, which is tracked in
git so the figures can be checked rather than taken on trust:

| Claim | Evidence |
|---|---|
| Capacity baseline (2026-01, `78bce1c`) | `loadtest/reports/capacity-baseline-steps.json` |
| Full-system mix (2026-01, `78bce1c`) | `loadtest/reports/full-system-steps.json` |
| Overload behaviour A/B (2026-08) | `loadtest/reports/ab/*-steps.json` |

Two sets of runs, on different machines and different builds. They are reported
separately and are **not** comparable to each other; each is internally
consistent. [Overload behaviour](#overload-behaviour-2026-08) is the newer one.

## Headline

- **HTTP ceiling ~5,700 req/s.** Two different request mixes hit the same wall,
  which points at a shared bottleneck rather than at one endpoint.
- **Full-system mix: 2,000 VUs sustained** at 5,832 req/s, p95 477 ms, zero
  errors. p95 breaks the 500 ms SLO at 3,000 VUs.
- **Capacity baseline: 550 VUs sustained** at 3,906 req/s, p95 220 ms, zero
  errors — but peak throughput is at 350 VUs, and the service collapses
  irrecoverably at 600 VUs.
- **The collapse is the interesting result.** Past the knee the service queues
  without bound instead of shedding load: a tenth of requests hit the client's
  60 s timeout, and it never recovers for the remaining eight steps of the run.
- **Admission control bounds latency past capacity, and costs throughput to do
  it.** In a later A/B on different hardware, p95 at 2,800 VUs is 806 ms with
  shedding against 3,061 ms without, while useful throughput drops 25% and two
  thirds of requests are refused. Capacity itself is unchanged.
- **The measurement found a bug the service had been hiding.** Redis
  transactions were opened with a raw `MULTI` on a pooled client, corrupting
  sessions under concurrency; a fifth of logins were failing and being reported
  as successes. See [Overload behaviour](#overload-behaviour-2026-08).

## What was measured, and on what

| | |
|---|---|
| Code under test | `78bce1c` (master) |
| Dates | capacity baseline 2026-01-17, full-system 2026-01-25 |
| Driver | k6 0.49.0, in-network (`http://api:7070`), via `make loadtest-*` |
| Deployment profile | `loadtest/docker-compose.loadtest.yml`, which caps each app service at 0.5 CPU (logic 1 GB, api/task/connect-tcp 256 MB, connect-ws 512 MB) |
| Replicas | 1 of each service |

**These numbers predate the PostgreSQL migration and bcrypt password hashing.**
At the time of the runs, user data lived in SQLite and login compared passwords
in plaintext. See [Gaps to close](#gaps-to-close) for what that invalidates.

Two things were **not** recorded with the runs and are therefore unknown: the
host machine's specs, and the ramp/warm-up settings. Only the step ladder and the
steady window can be recovered from the artifacts. Recording the full environment
alongside the results is the first item under [Gaps to close](#gaps-to-close).

> A superseded figure: an earlier summary (2026-01-18) quoted "1500 VUs / 7522
> rps / p95 224 ms" for the full-system mix. That came from a different run with
> a finer step ladder, and was never updated when the 2026-01-25 run replaced the
> data file underneath it. The numbers in this document are the ones the tracked
> JSON actually contains.

## Method

Each level of load runs in three phases: **ramp** (VUs climbing), **warm-up**
(discarded), and **steady** (measured). Only steady-state samples are tagged with
`{step, vus}` and rolled up, so connection setup and cold-start effects do not
pollute the percentiles. `loadtest/scripts/lib/step-report.js` does the rollup.

For a step to count as passing, all four must hold:

| Metric | Threshold |
|---|---|
| p95 latency | ≤ 500 ms |
| p99 latency | not enforced |
| error rate | ≤ 1% |
| timeout rate | 0% |

From that the report derives two numbers: **capacity** is the last step that
passes, **bottleneck** is the first step that fails. Both are resolution-limited
by the step size — a capacity of 2,000 VUs on a ladder that jumps by 1,000 means
"somewhere in [2000, 3000)", not 2,000 exactly.

**What the error rate counts.** A request counts as failed if it times out or
returns a status of 400 or above, except 429 — a refusal is the service working
as intended, and is tracked as its own shed rate instead
([ADR 0008](./adr/0008-capacity-is-the-last-step-that-holds-an-slo.md)).

**In the January runs this counted almost nothing.** The API answered every
request with HTTP 200 at the time, signalling the real outcome in a `code` field
in the body, so no HTTP client could tell a failure from a success and the error
rate was a timeout rate under another name. Failure codes carry a matching HTTP
status as of the 2026-08 work, which is what makes the shed rate measurable at
all.

## Run 1 — Capacity baseline

HTTP only. Per iteration: `login` ×1, `checkAuth` ×1, `push` ×3, `pushRoom` ×3,
`count` ×1, `getRoomInfo` ×1 — 10 requests, 6 of them message sends.

Ladder: 50 → 1000 VUs in steps of 50, 30 s steady per step, 20 steps, 856 s total.

| VUs | req/s | p90 ms | p95 ms | p99 ms | errors | timeouts | SLO |
|----:|------:|-------:|-------:|-------:|-------:|---------:|:---:|
| 50 | 986 | 2 | 2 | 2 | 0% | 0 | pass |
| 100 | 1,964 | 2 | 2 | 3 | 0% | 0 | pass |
| 150 | 2,921 | 2 | 3 | 8 | 0% | 0 | pass |
| 200 | 3,804 | 4 | 7 | 30 | 0% | 0 | pass |
| 250 | 4,566 | 8 | 18 | 77 | 0% | 0 | pass |
| 300 | 5,226 | 14 | 30 | 84 | 0% | 0 | pass |
| **350** | **5,680** | 28 | 61 | 95 | 0% | 0 | pass |
| 400 | 5,588 | 66 | 94 | 178 | 0% | 0 | pass |
| 450 | 4,581 | 108 | 159 | 319 | 0% | 0 | pass |
| 500 | 4,667 | 116 | 153 | 259 | 0% | 0 | pass |
| **550** | **3,906** | 182 | 220 | 485 | 0% | 0 | pass |
| 600 | 1,177 | 678 | 59,603 | 59,639 | 7.76% | 2,741 | **fail** |
| 650–1000 | 1–12 | ~59,640 | ~59,640 | ~59,640 | 100% | 41–345 | fail |

**Capacity is 550 VUs, but the knee is at 350 VUs.** Those are different
questions and it is worth keeping them apart. Throughput peaks at 350 VUs
(5,680 req/s, p95 61 ms) and then *falls* while latency keeps climbing — by 550
VUs the service is doing 31% less work at 3.6× the p95. Everything past the knee
is queueing, not capacity. The SLO happens to tolerate it up to 550 VUs; a
tighter p95 target would put capacity at the knee, which is where it belongs.

**At 600 VUs the failure is a cliff, not a slope.** p90 is still 678 ms while p95
is 59.6 s: roughly a tenth of requests hit the client's 60 s timeout while the
rest were served normally. Then it never comes back — every remaining step runs
at 100% timeouts and near-zero throughput, even though offered load kept
increasing in the same gradual way it had before.

The shape says why. Nothing was rejected and nothing drained: the service
accepted every connection, queued it, and kept queueing while the backlog grew
past the point where anything it finished was still wanted. By the later steps
every request that finally reached a handler had already been abandoned by its
client, so the work was spent producing results nobody was waiting for — which is
why increasing load in the same gradual increments never let it recover.

At the time of this run there was no admission control anywhere in the request
path: no bounded queue with a fast rejection path, no connection cap, and no
deadline on the RPC to logic, so an api goroutine waited indefinitely on a slow
call. Both of the missing bounds were added later; what that changed, and what
it did not, is measured in [Overload behaviour](#overload-behaviour-2026-08).

> **A caveat on this run's counters, and a correction.** The table shows 0 4xx,
> 0 5xx and 0 429, and an earlier version of this document offered that as
> evidence that nothing was ever rejected. It is not evidence. At the time of this
> run `tools/response.go` answered **every** request with HTTP 200, putting the
> real status in a `code`
> field in the JSON body, so those three counters were zero by construction and
> would have been zero no matter how the service behaved. The claim above rests
> on the timeouts and the failure to recover, which are real, and on the absence
> of any shedding path in the code. See [Gaps to close](#gaps-to-close): making
> failures visible at the HTTP layer has to come before shedding can be measured
> at all.

Message throughput at these points: ~3,408 msg/s at the knee, ~2,344 msg/s at SLO
capacity (6 of every 10 requests are sends).

## Run 2 — Full-system mix

HTTP and WebSocket concurrently. Per HTTP iteration: `checkAuth` ×1, `push` ×5,
`pushRoom` ×5, `count` ×2, `getRoomInfo` ×1 — 14 requests, 10 of them message
sends. WebSocket VUs run at 25% of HTTP VUs with 20–40 s sessions.

Ladder: 1000 → 5000 VUs in steps of 1000, 60 s steady per step, 5 steps, 663 s
total. Note the coarse ladder: it locates the limit to within 1,000 VUs, no finer.

| VUs | req/s | msg/s | p90 ms | p95 ms | p99 ms | errors | SLO |
|----:|------:|------:|-------:|-------:|-------:|-------:|:---:|
| 1,000 | 5,084 | 3,632 | 95 | 129 | 190 | 0% | pass |
| **2,000** | **5,832** | **4,166** | 392 | 477 | 781 | 0% | pass |
| 3,000 | 5,424 | 3,874 | 798 | 1,029 | 2,937 | 0% | **fail** |
| 4,000 | 5,416 | 3,869 | 1,103 | 1,428 | 2,972 | 0% | fail |
| 5,000 | 3,591 | 2,565 | 3,423 | 4,673 | 7,592 | 0% | fail |

**This mix is latency-bound, not error-bound.** Zero timeouts at every step,
including the failing ones — the service kept answering all the way to 5,000 VUs,
just far too slowly. (The zero error rate carries less weight than it looks:
because every response is HTTP 200, the only failures this run could observe were
timeouts. See the caveat under Run 1.) Throughput sits on a plateau of roughly
5,400–5,800 req/s from 2,000 through 4,000 VUs while p95 grows 3×, which is the
textbook shape of a saturated server: the queue absorbs the extra concurrency and
hands it back as latency.

**Capacity is 2,000 VUs** (5,832 req/s, p95 477 ms — 23 ms of headroom under the
SLO). The real limit is somewhere in [2000, 3000); a finer ladder would pin it
down, and that run is worth doing.

## Reading the two runs together

The two mixes exercise different endpoint ratios, and one of them adds WebSocket
traffic, yet both plateau at about the same place — 5,680 req/s and 5,832 req/s.
A ceiling that does not move when the workload composition changes is a property
of the deployment, not of any one endpoint. The obvious candidate is the 0.5 CPU
cap the load-test profile puts on each app service.

That is a hypothesis this data cannot settle. Confirming it means re-running with
the cap raised and checking whether the ceiling moves with it.

## Overload behaviour (2026-08)

A second set of runs, measuring what the service does past its capacity and what
two changes did about it: a deadline on every outbound RPC, and admission control
that refuses work rather than queueing it ([ADR
0009](./adr/0009-the-api-sheds-load-instead-of-queueing-it.md)).

Unlike the January runs, the environment is recorded — that was the first item on
the gap list, and quoting a figure that cannot be reproduced from the artifact
was the reason it was there.

| | |
|---|---|
| Host | AMD Ryzen 7 7800X3D, 8 cores, 31 GB RAM, Windows 11 |

Development moved to an Apple Silicon Mac in September 2026. Nothing below was
re-measured there, so every number on this page describes hardware the project
no longer runs on, and none of it is a prediction for the Mac. Gaps 1 and 12
below already ask for this; the backlog ticket `rerun-mixes-on-current-build`
now tracks it.
| Docker | 29.1.3, Linux engine, 8 CPUs and 15.6 GB allocated |
| Build | working tree at the commit that introduced admission control |
| Profile | `loadtest/docker-compose.loadtest.yml`, 0.5 CPU per app service |
| Replicas | 1 of each |
| Driver | k6 0.49.0, in-network, `capacity-baseline.js` |
| Ladder | 15 s ramp, 15 s warm-up, 30 s steady per step |
| bcrypt cost | 4 (`GOCHAT_BCRYPT_COST`), so the KDF does not dominate `login` |
| Admission | `maxInFlight` 512, `acquireTimeout` 50 ms |

**These runs are on faster hardware than the January ones.** Nothing here should
be compared against the numbers above; the comparisons that matter are between
the arms below, which differ only in configuration.

### The three arms

| Arm | RPC deadline | Admission control | Evidence |
|---|---|---|---|
| A | none (120 s) | off | `ab/stress-no-deadline-steps.json` |
| B | 2 s | on | `ab/stress-admission-on-steps.json` |
| — | 2 s | off | `ab/admission-off-steps.json`, `ab/no-deadline-steps.json` (100–800 VU ladder) |

### Result: latency stops growing, at a price

| VUs | A: served req/s | A: p95 | B: served req/s | B: p95 | B: shed |
|----:|----------------:|-------:|----------------:|-------:|--------:|
| 400 | 2,862 | 486 ms | 3,088 | 459 ms | 0% |
| 800 | 3,306 | 1,262 ms | 3,125 | 629 ms | 26.0% |
| 1,200 | 3,004 | 1,936 ms | 3,128 | 501 ms | 48.9% |
| 1,600 | 3,391 | 2,094 ms | 2,438 | 524 ms | 65.5% |
| 2,000 | 3,698 | 2,660 ms | 2,520 | 584 ms | 65.9% |
| 2,400 | 3,424 | 3,314 ms | 2,569 | 725 ms | 62.9% |
| 2,800 | 3,561 | 3,061 ms | 2,666 | 806 ms | 63.7% |

"Served" excludes shed requests; B's raw request rate reaches 7,395/s, most of
which is the cost of saying no.

**Capacity is 400 VUs in both arms, and in every other arm run.** That is the
result to state first, because it is the one most likely to be misreported.
Shedding does not raise the ceiling and was never going to: past saturation the
service was already finishing less work than it was offered.

**What changes is everything above the ceiling.** Without shedding, p95 climbs to
3.3 s and keeps climbing; with it, p95 stays between 459 ms and 806 ms across a
7× range of offered load. At 2,800 VUs that is 806 ms against 3,061 ms — **3.8×
lower**.

**The price is real and is not hidden.** Served throughput at 2,800 VUs falls
from 3,561 to 2,666 req/s, **25% less useful work**, and by then two thirds of
requests are being refused. `maxInFlight` of 512 is conservative: served
throughput dips to 2,438 req/s at 1,600 VUs and recovers above it, which is the
signature of a limit set below what the service could actually sustain. A larger
value would give back some of that throughput at the cost of some latency. The
number is a dial between the two, and 512 has not been tuned — it is the first
value tried.

**Upstream failures drop.** In the 100–800 VU ladder run with a 2 s deadline and
no shedding, 503s from the deadline reached 1,050 and 1,735 in the top two steps;
with shedding they fell to 69 and 186. Refusing at the edge keeps logic inside
the envelope where it answers in time.

### What could not be reproduced

The January baseline collapsed at 600 VUs and never recovered. **On this hardware
that does not happen** — arm A was pushed to 2,800 VUs, with no deadline and no
shedding, and degraded smoothly the whole way: latency grew to about 3 s,
throughput held near 3,400 req/s, and there were zero timeouts and zero errors.

So the claim that admission control prevents collapse is **not tested here**. What
is tested is the behaviour past capacity, and there the difference is clear. The
collapse would need the slower machine, a much longer ladder, or both to
reproduce, and that run has not been done.

There is also a strong candidate for why the old run collapsed and this one does
not, unrelated to hardware — see below.

### A bug the measurement found

The first run of this A/B returned **10.6% HTTP 503 from the very first step**,
at 100 VUs, with a p99 of 278 ms — far too fast to be timeouts. The cause was not
load at all:

```
136,483  GetRoomInfo failed: getRoomInfo no this user
 23,097  Login failed: ERR EXEC without MULTI
 15,471  PushRoom failed: redis: can't parse array reply: "+QUEUED"
```

`logic` opened Redis transactions with `Do("MULTI")` on a pooled client. A raw
`MULTI` does not pin the connection that the following commands go out on, so
under concurrency the transaction interleaved across connections: some commands
executed outside a transaction, others returned the literal `+QUEUED` where a
value was expected. Sessions were being written incorrectly under load, and login
was failing outright a fifth of the time. Fixed by using `TxPipeline`.

The second error was in the measurement, not the service: `GetRoomInfo` returning
"no this user" is a *business* outcome, and this change had been mapping every
RPC error to 503. rpcx distinguishes the two — a `ServiceError` is the remote
method returning an error, anything else is the call itself failing — so only the
latter is now reported as unavailable.

After both fixes the error floor is zero up to 500 VUs, against 10.6% before.

None of this was visible before this work: seven of the API's RPC wrappers
discarded the call error, and the zero value of the reply's `Code` field is
`CodeSuccess`, so **a failed call was reported to the client as a success**. The
January numbers were measured on a build with the Redis bug in it, silently. How
much of that 600 VU collapse was congestion and how much was a corrupted session
store is not knowable from the artifacts.

## Shutdown behaviour (2026-08)

What a connect instance's departure costs its clients, measured with graceful
shutdown on and then off. Same image, same procedure, one environment variable
changed — the method used for the overload A/B above.

### What the control arm reproduces

`GOCHAT_GRACEFUL_SHUTDOWN=false` makes the process exit on the signal without
running any teardown, which reproduces the *effects* of the SIGTERM nobody
handled: the kernel tears the sockets down, the etcd node is left to its
two-minute TTL, and Redis keeps the routing key. It does not reproduce the
signal disposition itself, and the difference is worth naming rather than
glossing.

### The two arms

Both run `make drain-demo`, which brings up a stack with `--scale connect-ws=2`,
holds N WebSocket connections across the two replicas, sends room messages
throughout, restarts replica 1, and records per connection what the client saw.

50 connections, 25 on the replica that gets restarted and 25 on the one that does
not, room messages every 200 ms, a 20 second observation window. Evidence in
`loadtest/reports/drain/{graceful,control}.json`.

| | control (`false`) | graceful (`true`) |
|---|---|---|
| Closed with 1001 going away | 0 (0%) | **25 (100%)** |
| Closed with another close code | 25 (100%) | 0 |
| Time to notice, p50 / p95 | 46.7 ms / 48.7 ms | 52.1 ms / 52.1 ms |
| etcd registration outlived the stop by | **still present after 20 s** | **683 ms** |
| Room messages sent | 99 | 99 |
| Frames received on the surviving replica | 2,475 | 3,100 |

Three things in that table are worth reading carefully.

**The close code is the whole point, and it is binary.** Every connection on the
restarted replica gets 1001 in one arm and none does in the other. The control
arm's "another close code" is 1006, abnormal closure, which gorilla synthesises
when the socket dies without a close frame — indistinguishable from a network
failure, which is exactly the problem.

**Time to notice barely moves, and that is not a disappointment.** Both arms
notice in about 50 ms, because the TCP close reaches the client at the same
moment either way. Graceful shutdown does not make a client notice *faster*; it
makes the client able to tell *what happened*. Any claim that it shortens the
outage should be treated as a measurement error.

**etcd residency is where the large number is.** 683 ms against a registration
still standing when the window closed 20 seconds later — and the ceiling on that
is the two-minute TTL, not 20 seconds. That window is `task` routing messages to
an instance that does not exist and dropping them silently.

**The frame counts differ for a reason worth naming, and the arithmetic is
exact.** Both arms sent 99 room messages; 99 to 25 connections is 2,475, which is
the control arm's number to the frame. The graceful arm's 3,100 is 625 higher,
and 625 is exactly 25 × 25: each of the 25 disconnects runs a `DisConnect` that
republishes room membership to all 25 surviving connections. In the control arm
no disconnect ever runs, so **the surviving clients are never told the room
emptied** and their membership view stays stale. That is a second, unlooked-for
consequence of the same fix — and it is not extra chat throughput, which is why
the row says frames rather than messages.

The exactness is itself the check. An earlier run of this measurement counted
2,500 in the control arm, because the sender counted any HTTP response as a send
and this API answers 200 to everything with the real status in the body. The
figure only became checkable once the sender read the body's `code`.

### What this cannot show

**It does not reduce message loss to zero.** `task` still resolves `serverId` at
delivery time and drops the message if that instance has gone
(`task/push.go`). Graceful shutdown narrows the window from the etcd TTL to the
length of a shutdown; it does not close it. Expect the surviving replica's
message count to be similar in both arms — if it is dramatically better in the
graceful arm, suspect the measurement.

**It does not make clients reconnect.** Sending 1001 makes correct client
behaviour possible. The bundled frontend does not act on it. What the
measurement can show is that the information reached the client, not that anyone
used it.

See
[ADR 0014](./adr/0014-a-departing-connect-instance-deregisters-before-it-closes-connections.md).

## Gaps to close

Ordered by how much each would change what this document can claim.

1. **Record the environment with the run.** Host specs, replica counts and the
   full k6 parameter set should be written into the results file. Right now
   "5,832 req/s" cannot be reproduced from the artifact alone.
2. **Re-measure after the PostgreSQL and bcrypt changes.** bcrypt costs 50–100 ms
   per hash by design, which will dominate `login` — and `login` is 1 of the 10
   requests in the capacity-baseline mix. Those numbers will move, and the
   current ones should not be quoted for the current code. The full-system mix
   contains no `login` and authenticates against Redis, so it should be far less
   affected, but that is a prediction, not a measurement.
3. **A/B the auth cache.** `api/cache/auth_cache.go` caches `checkAuth` results
   for 30 s to save an RPC hop to logic. `AUTH_CACHE_ENABLED=false` turns it off,
   so one build can be measured both ways. The commit that added the cache has no
   before/after number attached to it.
4. **Scale-out curve.** Every run so far used one replica of each service. The
   headline claim of this repository is horizontal scalability, and nothing here
   measures it. Run the capacity baseline at 1, 2 and 3 logic replicas
   (`make loadtest-capacity LOGIC_REPLICAS=3`) and plot throughput against
   replica count. Note that Prometheus scrapes `logic:9091` from a static target,
   so with more than one replica its logic metrics cover whichever replica DNS
   resolves to — the k6 client-side numbers are still complete.
5. **Test the ceiling hypothesis.** Raise the CPU caps and re-run; if the ceiling
   moves with them, the bottleneck is the cap and the software has more room than
   these numbers suggest.
6. **Finer ladder near the limits.** 1,000-VU steps in the full-system run are
   too coarse to locate the limit, and the capacity baseline never resolves what
   happens between 550 and 600 VUs, which is where the cliff is.
7. ~~Make failures visible at the HTTP layer.~~ Done: failure codes carry a
   matching HTTP status, so a refusal is a 429 and an upstream failure a 503.
8. ~~Bound the RPC to logic.~~ Done: a context deadline, default 2 s, on every
   outbound call.
9. ~~Add a load-shedding path.~~ Done, and measured in
   [Overload behaviour](#overload-behaviour-2026-08). It bounds latency past
   capacity; whether it prevents the January collapse is still untested, because
   the collapse could not be reproduced on the newer hardware.
10. **Tune `maxInFlight`.** 512 is the first value tried, and the throughput dip
    at 1,600 VUs says it is set below what the service can sustain. Sweep it and
    pick the point where added latency stops buying throughput.
11. **Reproduce the collapse.** Either on slower hardware or with a much longer
    ladder. Without it, the strongest claim about shedding remains unproven.
12. **Re-run the January mixes on current hardware**, so that one set of numbers
    describes the current build end to end instead of two sets describing
    different ones.

## Reproducing

```bash
# Capacity baseline (HTTP only, step ladder)
make loadtest-capacity K6_START_VUS=50 K6_END_VUS=1000 K6_STEP_VUS=50 \
    K6_STEP_DURATION=30s

# Full-system mix (HTTP + WebSocket)
make loadtest-full K6_START_VUS=1000 K6_END_VUS=5000 K6_STEP_VUS=1000 \
    K6_STEP_DURATION=60s

# Same build with the auth cache off, for the A/B
AUTH_CACHE_ENABLED=false make loadtest-capacity

# The overload A/B. One arm per configuration, same ladder, same build;
# ADMISSION_ENABLED and RPC_TIMEOUT are read by the api service.
make loadtest-capacity K6_START_VUS=400 K6_END_VUS=2800 K6_STEP_VUS=400 \
    K6_STEP_DURATION=30s K6_RAMP_DURATION=15s K6_WARMUP_DURATION=15s

make loadtest-stop

# The shutdown A/B. Brings up its own stack with two connect-ws replicas, runs
# both arms, and writes loadtest/reports/drain/{graceful,control}.json.
make drain-demo
```

Flush Redis and truncate `users` between arms, or the second run starts with the
first one's sessions warm. `make loadtest-start` does both.

Reports land in `loadtest/reports/`. The `*-steps.json` files are tracked; the
HTML reports and raw k6 JSON are ignored because of their size. Open
`loadtest/reports/capacity-baseline.html` for the charted version of the table
above.

Load-test options and SLO overrides are documented in
[LOAD_TESTING.md](./LOAD_TESTING.md).
