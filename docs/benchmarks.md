# Benchmarks

Measured results for GoChat, and what they say about where the system breaks.

Every number here comes from a file in `loadtest/reports/`, which is tracked in
git so the figures can be checked rather than taken on trust:

| Claim | Evidence |
|---|---|
| Capacity baseline | `loadtest/reports/capacity-baseline-steps.json` |
| Full-system mix | `loadtest/reports/full-system-steps.json` |

## Headline

- **HTTP ceiling ~5,700 req/s.** Two different request mixes hit the same wall,
  which points at a shared bottleneck rather than at one endpoint.
- **Full-system mix: 2,000 VUs sustained** at 5,832 req/s, p95 477 ms, zero
  errors. p95 breaks the 500 ms SLO at 3,000 VUs.
- **Capacity baseline: 550 VUs sustained** at 3,906 req/s, p95 220 ms, zero
  errors — but peak throughput is at 350 VUs, and the service collapses
  irrecoverably at 600 VUs.
- **The collapse is the interesting result.** Past the knee the service queues
  without bound instead of shedding load: no 429s, no 5xx, just clients timing
  out at 60 s, and it never recovers for the rest of the run.

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

The counters say why: **0 4xx, 0 5xx and 0 429 across the entire run.** Nothing
was ever rejected. The service accepted every connection and queued it until the
client gave up, and the backlog never drained. There is no admission control, no
bounded queue with a fast rejection path, and no connection cap. Adding one would
not raise the ceiling, but it would turn a hard outage into graceful degradation
— the 600 VU step would return 429s instead of dying.

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

**This mix is latency-bound, not error-bound.** Zero errors and zero timeouts at
every step, including the failing ones — the service kept answering all the way
to 5,000 VUs, just far too slowly. Throughput sits on a plateau of roughly
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
7. **Add a load-shedding path**, then re-run to show the 600 VU step degrading
   instead of collapsing. This is the one item that changes the system rather
   than the measurement.

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

make loadtest-stop
```

Reports land in `loadtest/reports/`. The `*-steps.json` files are tracked; the
HTML reports and raw k6 JSON are ignored because of their size. Open
`loadtest/reports/capacity-baseline.html` for the charted version of the table
above.

Load-test options and SLO overrides are documented in
[LOAD_TESTING.md](./LOAD_TESTING.md).
