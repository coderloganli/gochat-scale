# Capacity is the last step that holds an SLO

summary: Load is applied as a ladder of steps with warm-up excluded, and capacity is defined as the last step meeting a stated SLO rather than as a peak throughput number.

status: reconstructed from the implementation of `loadtest/scripts/lib/`.

## Context

"How much can it handle" has no answer until someone says what counts as
handling it. A single number — peak requests per second — is the usual answer and
is close to meaningless: a system will happily report its highest throughput at a
latency nobody would accept, and the peak often occurs *after* the point where
service has already degraded.

Ramping load continuously makes it worse, because every sample is taken at a
different offered load and the percentiles blend them together.

## Decision

Load is applied as a **ladder**. Each rung holds a fixed number of virtual users
through three phases:

| Phase | Measured |
|---|---|
| ramp | no |
| warm-up | no |
| steady | yes |

Only steady-state samples are tagged with `{step, vus}` and rolled up
(`loadtest/scripts/lib/step-report.js`), so connection setup and cold caches do
not contaminate the numbers.

A step passes if **all** of: p95 ≤ 500 ms, error rate ≤ 1%, timeout rate = 0.
**Capacity** is the last passing step; **bottleneck** is the first failing step,
reported with the reasons it failed. Thresholds are overridable per run
(`SLO_P95_MS` and friends).

## Why

Discarding warm-up and ramp is what makes a step's percentiles describe one
level of load rather than an average over a climb. Defining capacity against a
stated SLO makes the number arguable in the right way: someone who disagrees can
change the threshold and re-derive it, instead of disagreeing about what the
number meant.

Reporting the first *failing* step alongside the last passing one matters as much.
The failure mode — latency, errors, or timeouts — says which subsystem gave way,
and that is the part worth acting on.

## What this deliberately does not capture

Capacity by SLO is not the throughput knee, and the two can be far apart. In the
capacity baseline run, throughput peaks at 350 VUs while the SLO does not fail
until 550 VUs, by which point the system is doing 31% less work at 3.6× the p95.
Everything between is queueing that the SLO happens to tolerate. Both numbers are
reported in `docs/benchmarks.md`; neither alone is the answer.

## Alternatives

**Fixed VUs for a fixed duration.** Still available (`K6_VUS` with
`K6_DURATION`). Answers "does it hold at this load", not "where does it break".

**Continuous ramp to failure.** Finds the breaking point faster and gives
percentiles that cannot be attributed to a load level.

**Peak throughput as the headline.** The thing being rejected.

## Consequences

- A run costs `steps × (ramp + warm-up + steady)`. The published baseline is 20
  steps at 30 s steady, about 14 minutes.
- Capacity is only as precise as the step size. A ladder rising by 1,000 VUs
  reports capacity to within 1,000 VUs, and the report says so.
- Per-step rollups are written to `loadtest/reports/*-steps.json`, which is
  tracked in git so published figures can be checked against the run.
