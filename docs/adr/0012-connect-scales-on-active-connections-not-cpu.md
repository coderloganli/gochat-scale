# connect scales on active connections, not CPU

summary: The connect-ws autoscaler targets `gochat_connections_active` per pod through prometheus-adapter, because a WebSocket server exhausts connection capacity long before it exhausts CPU, and scales down slowly because evicting live connections causes a reconnect storm.

## Context

The system's whole structure rests on separating connection state from business
logic (`docs/architecture.md`): connections are expensive and sticky, logic is
cheap and stateless. Autoscaling is where that separation either pays off or is
revealed to be words.

The default answer — a `Resource` metric on CPU utilisation — was available for
free, since `metrics-server` had to be installed anyway. The question was whether
it is the right signal for `connect`.

It is not. A `connect` process spends almost all of its time with idle sockets
parked in buckets. Its cost is file descriptors, bucket slots and the memory
behind each channel; its CPU is near zero until messages actually flow. It can
sit at single-digit CPU with every connection slot spoken for. The code concedes
the point itself: `connect/websocket.go` refuses connections past a hardcoded
`maxConnections = 10000`, a ceiling with nothing to do with CPU. Scaling on CPU
means the autoscaler stays asleep until well past the point where new clients
start being refused, and the failure shows up as rejected connections rather than
as a busy graph — the metric that would have explained it is the one nobody was
watching.

The metric that describes that ceiling is `gochat_connections_active`. It was
declared in `pkg/metrics/metrics.go` and never written to: the live count was
kept in a package-level atomic in `connect/websocket.go` and never reached
Prometheus, so the series did not exist at all. Wiring the gauge to that count
was a prerequisite for this decision rather than a consequence of it.

The `api` service, by contrast, is a conventional request/response service, and
already exports `gochat_admission_in_flight` — genuinely, from
`pkg/middleware/admission.go` — the same counter that admission control bounds
(ADR 0009).

## Decision

Two `autoscaling/v2` HorizontalPodAutoscalers, both on `Pods`-type custom
metrics served through `custom.metrics.k8s.io`:

- **connect-ws** on `gochat_connections_active`, `AverageValue` 200 per pod,
  1 to 6 replicas.
- **api** on `gochat_admission_in_flight`, `AverageValue` 300 per pod, against
  the admission limit of 512 in `config/dev/api.toml`.

`connect-ws` scales up with no stabilisation window and up to 100% more pods per
15 seconds; it scales down only after a 300-second stabilisation window and never
more than one pod per minute.

The metrics reach the API through **prometheus-adapter** v0.12.0, configured with
two rules that map the series onto pods via their `pod` and `namespace` labels.
`metrics-server` is installed alongside it, so CPU-based scaling remains
available for comparison.

`connect-tcp`, `task`, `logic` and `site` get no autoscaler.

## Why

**The metric is chosen from what exhausts first.** For connect that is
connections; for api it is in-flight work. The api metric was already measured;
the connect one only looked as though it was, which is worth remembering before
trusting any other declared-but-unwritten gauge in this repository.

**The api target sits below the shedding threshold on purpose.** 300 in flight
against a 512 limit means the autoscaler adds capacity before admission control
starts refusing requests. ADR 0009 made shedding the floor under overload —
bounded latency and an explicit 429 instead of an unbounded queue. Autoscaling
sits above that floor.

They are complementary because they act on different timescales. Admission
control decides in the 50ms acquire timeout; autoscaling decides in tens of
seconds, because a metric has to be scraped, relisted and polled before a pod can
even be created. Against a sharp ramp, shedding will happen first no matter how
the target is set — that is the point of having it. Scaling handles load that is
sustained; shedding handles load that arrives faster than pods can start.

**The scale-down asymmetry is the part that is specific to connections.**
Removing a `connect` pod does not drain gracefully — it kills live WebSocket
sessions, and every one of those clients reconnects immediately onto the
remaining pods. A scale-down at the wrong moment therefore raises load on exactly
the pods that were already carrying it, which can trigger a scale-up, which later
triggers another scale-down. A five-minute stabilisation window and a one-pod-
per-minute limit make that oscillation impossible in practice. Scaling up has no
such hazard, so it carries no stabilisation delay — bounded only by the
100%-per-15s policy and `maxReplicas`.

**prometheus-adapter rather than KEDA** because `custom.metrics.k8s.io` and a
stock HPA is the mechanism the platform actually defines, and the aggregated API
layer is the part worth being able to explain. KEDA's `ScaledObject` is less
configuration and would work; it also hides the step where a Prometheus series
becomes a Kubernetes metric, which is the step with the interesting failure modes.

## Alternatives

**CPU utilisation on both services.** Free, conventional, and wrong for connect
for the reasons above. Kept installed rather than rejected outright, precisely so
the two can be compared.

**KEDA.** See above. A reasonable choice; a different lesson.

**An `Object` metric on the Service rather than a `Pods` metric.** Would scale on
a cluster-wide total instead of a per-pod average, which means the target would
have to be restated every time the replica count changed. `Pods` with
`AverageValue` is the form that stays correct.

**Autoscaling every service.** Filler. Two autoscalers that disagree about which
signal matters make the point; six identical ones make noise.

## Consequences

- **The targets are guesses.** 200 connections per pod and 300 in-flight requests
  were not derived from a load test — they were picked to be demonstrable on a
  single-node kind cluster. `docs/benchmarks.md` has the measured capacity
  numbers for the Compose deployment; nothing equivalent has been run under
  Kubernetes. Any real use of these values should recalibrate them, exactly as
  `maxInFlight` had to be.
- Scaling depends on the whole chain: Prometheus scraping per pod, the adapter
  relisting series (60s by default), the HPA polling (15s). Expect tens of
  seconds of lag between load arriving and pods appearing. That is inherent to
  metrics-driven autoscaling and is why admission control matters underneath it.
- `gochat_connections_active` carries a `type` label (`websocket`/`tcp`), so the
  adapter rule must aggregate with `sum(...) by (<<.GroupBy>>)`. Without it the
  adapter sees more than one series per pod and the metric is ambiguous.
- Because the metric comes from the pods themselves, a pod that is scraped but
  not ready still contributes. On scale-up this briefly drags the average down
  and damps the next scaling decision — acceptable here, and worth knowing before
  reading the HPA's arithmetic and finding it surprising.
- **Scaling out does not relieve the pods that are already loaded, and the
  average hides that.** Measured: 450 connections on one pod scaled correctly to
  three pods, and all 450 stayed where they were, because Kubernetes balances per
  connection and a WebSocket lasts until its client leaves. The HPA then read a
  comfortable 150/200 while one pod held 450 and two held none. New connections
  do spread across the new pods, so this recovers over time with normal churn —
  but relying on it in earnest would need connection draining, clients that
  reconnect periodically, or a maximum-per-pod signal instead of an average.
  `AverageValue` was still the right choice over an `Object` total, which would
  have needed restating on every replica change; the limitation is inherent to
  autoscaling sticky connections, not to the metric type.
