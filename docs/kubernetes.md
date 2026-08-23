# Running GoChat on Kubernetes

The second deployment target. Docker Compose remains the one the development
loop, the CI integration tests and the load tests use; this one exists to answer
the questions Compose cannot ask — what a readiness probe is worth when a
dependency fails, and which signal an autoscaler should watch.

Manifests are in `deployments/k8s/`. The cluster is `kind`, configured by a
committed file so that the cluster you get is the cluster this was verified on.

## Bringing it up

```bash
make k8s-cluster-up     # create the kind cluster from deployments/k8s/kind-cluster.yaml
make k8s-up             # build and load the image, then deploy everything
```

`make k8s-up` is idempotent and waits for every rollout, so it exits when the
system is actually usable rather than when the API server has accepted the
manifests.

| What | Where |
|---|---|
| GoChat UI | http://localhost:8080 |
| API | http://localhost:7070 |
| WebSocket | ws://localhost:7000/ws |
| Grafana | http://localhost:3000 (admin/admin) |
| Prometheus | http://localhost:19090 |
| Jaeger | http://localhost:16686 |

Then `make k8s-status` for a summary, `make k8s-hpa` to watch the autoscalers,
`make k8s-down` to remove the workloads, `make k8s-cluster-down` to remove the
cluster.

**Compose and Kubernetes cannot run at the same time.** Both bind host ports
8080, 7070, 7000, 3000, 19090 and 16686.

Note which thing holds them: the `extraPortMappings` are on the kind *node
container*, so it binds those ports for as long as it exists. `make k8s-down`
removes the workloads and does **not** free them — Compose will still fail with
`port is already allocated`. To switch to Compose, stop the node:

```bash
docker stop gochat-control-plane     # keeps the cluster, frees the ports
docker start gochat-control-plane    # bring it back, then: make k8s-up
```

or `make k8s-cluster-down` to remove the cluster entirely.

## Why NodePorts and not an Ingress

The frontend in `site/static/js/` is a prebuilt React bundle committed to the
repository — it is not built from source here — and it has
`http://127.0.0.1:7070` and `ws://127.0.0.1:7000/ws` compiled into it.

So the application is only usable when those exact ports answer on localhost.
`deployments/k8s/kind-cluster.yaml` maps them to fixed NodePorts through
`extraPortMappings`, which is why the UI works with no `kubectl port-forward` and
no Ingress. An Ingress would need the frontend rebuilt to take its endpoints from
configuration. That is a separate task, and until it happens TLS is out of reach
too.

## What is deployed

Two namespaces:

- **`gochat`** — the six application roles and the four dependencies
  (PostgreSQL, Redis, RabbitMQ, etcd) as single-replica StatefulSets with
  PersistentVolumeClaims.
- **`monitoring`** — Prometheus, Grafana, Jaeger and prometheus-adapter. Kept
  separate so the application namespace can be torn down and recreated without
  taking the metrics pipeline with it.

`metrics-server` goes to `kube-system`, from its upstream release manifest.

The application needs **no configuration change at all** to run here. The
Services are named `etcd`, `redis`, `postgres` and `rabbitmq`, which is exactly
what `config/dev/common.toml` already asks for. `jaeger` is an ExternalName
alias in the `gochat` namespace pointing at the real Jaeger in `monitoring`, for
the same reason — the config says `jaeger:4318` and it should keep working.

Inter-service RPC does **not** go through those Services. logic and connect
register their pod IPs in etcd and callers resolve through the registry, which is
what lets `task` address one specific connect instance (`docs/adr/0002`). Pod IPs
are routable cluster-wide, so this works unchanged from Compose — the code
already substitutes its own non-loopback address for `0.0.0.0` before
registering (`tools/network.go`).

## Readiness actually means something here

Every role serves `/health` and `/ready` on its metrics port (9091–9096).

- **`/health`** is the liveness probe and answers 200 unconditionally. That is
  deliberate: a dependency being down is not a reason to restart anything, and a
  liveness probe that checks dependencies turns an outage into a cluster-wide
  restart loop.
- **`/ready`** runs the checks that role actually needs and returns 503 naming
  what failed.

See `docs/adr/0011`. You can watch it work:

```bash
kubectl scale statefulset/postgres -n gochat --replicas=0
kubectl get pods -n gochat -w
```

logic goes `0/1` within a few seconds, leaves the Service endpoints, and is
**not** restarted — its restart count stays where it was. Scale PostgreSQL back
to 1 and it returns to `1/1` on its own.

```bash
kubectl exec -n gochat deploy/logic -- curl -s localhost:9091/ready
# {"failures":{"db":"..."},"status":"not ready"}
```

## Autoscaling

Two HorizontalPodAutoscalers, and they deliberately disagree about what to watch.

**connect-ws scales on `gochat_connections_active`**, 200 per pod. A WebSocket
server's cost is the connections it holds, not the CPU it burns — the code says
so itself, with a hardcoded `maxConnections = 10000` that has nothing to do with
CPU. Scaling it on CPU means the autoscaler sleeps until well past the point
where clients start being refused.

It scales up immediately and scales down only after a five-minute stabilisation
window, one pod per minute. Removing a connect pod kills live sessions and every
one of those clients reconnects onto the remaining pods, so an eager scale-down
is how you get a reconnect storm.

**api scales on `gochat_admission_in_flight`**, 300 per pod against the
admission limit of 512. Capacity is added before load shedding starts. Shedding
still happens first under a sharp ramp — it reacts in 50ms and autoscaling in
tens of seconds — which is the point of having both (`docs/adr/0009`,
`docs/adr/0012`).

`metrics-server` is installed alongside, so CPU-based scaling is available to
compare against.

### How the metric gets to the HPA

```
pod /metrics  ->  Prometheus (pod discovery)  ->  prometheus-adapter
              ->  custom.metrics.k8s.io  ->  HorizontalPodAutoscaler
```

Two things in that chain are easy to get wrong and worth knowing about:

**Prometheus discovers pods, it does not list targets.** A static target scrapes
one replica out of N. More importantly, pod discovery is what attaches
`namespace` and `pod` labels to every series, and those labels are the join key
prometheus-adapter uses to answer a question about a pod. Without them the custom
metrics API has nothing to key on (`docs/adr/0013`). A new workload must carry
`prometheus.io/scrape` and `prometheus.io/port` annotations or it is invisible.

**prometheus-adapter serves `custom.metrics.k8s.io` only.** Its upstream
manifests register `v1beta1.metrics.k8s.io` — the *resource* metrics API — which
would displace metrics-server and break CPU autoscaling. The RBAC here is
modelled on upstream rather than copied, with distinct object names, because
upstream's names collide with metrics-server's own.

Checking the chain:

```bash
kubectl get apiservices | grep metrics          # both should be True
kubectl top pods -n gochat                      # metrics-server
kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/namespaces/gochat/pods/*/gochat_connections_active" | jq
kubectl get hpa -n gochat                       # a number, not <unknown>
```

The HPA controller is granted `metrics.k8s.io` out of the box but **not**
`custom.metrics.k8s.io`; the ClusterRole that fixes that is in
`prometheus-adapter/rbac.yaml`. Without it every HPA reads `<unknown>` for ever
and the events blame the adapter, which is not at fault.

## What the first real run showed

Measured on one laptop against the cluster this file describes. Numbers are
illustrative of behaviour, not capacity figures — `docs/benchmarks.md` is where
capacity lives, and it describes the Compose deployment.

**connect-ws scaled the way it was meant to.** 450 WebSocket connections against
a 200-per-pod target: the HPA read `450/200`, went 1 → 2 → 3 within about 40
seconds, and settled at `150/200`. When the load stopped, the metric fell to 0
and replicas stayed at 3 for a further 300 seconds before dropping to 2, one pod
at a time — exactly the `stabilizationWindowSeconds` and the one-pod-per-minute
policy, doing what they were written to do.

**But scaling out does not relieve the pod that is already loaded.** All 450
connections stayed on the original pod; the two new pods held zero. Kubernetes
balances per *connection*, not per request, and a WebSocket connection lasts
until the client goes away. So a scale-out only helps connections that have not
been made yet.

That has a sharp consequence for the metric: the HPA averages across pods, so it
read a comfortable `150/200` while one pod actually held 450 and two held
nothing. The autoscaler was satisfied and the hot pod was still hot. Anyone
relying on this in earnest needs either connection draining, a client that
reconnects periodically, or a maximum-per-pod signal rather than an average.

The same effect is milder for `api`, because HTTP connections are shorter-lived:
connections opened *before* a scale-out stayed pinned, but new ones spread evenly
(233 and 266 across two pods in a later run).

**api scaled before it shed.** 500 concurrent logins against a 300-per-pod
target and a 512 admission limit: the HPA read `500/300`, scaled 1 → 2, settled
at `250/300`, and over 75,000 requests exactly **one** was shed with a 429. That
is the intended relationship between ADR 0009 and ADR 0012 — autoscaling absorbs
sustained load, shedding is the floor underneath it.

**Login, not the connection layer, is the first thing to saturate.** Opening a
few hundred connections at once fails at the *login* step, not the WebSocket
step: bcrypt at the default cost on a logic pod limited to 0.5 CPU blows the 2s
RPC deadline. Any load harness pointed at this has to obtain its tokens with
bounded concurrency first, or it measures the KDF. `GOCHAT_BCRYPT_COST` exists
for load tests for this reason and must not be lowered anywhere else.

## What is deliberately not production-grade

- **Single-node cluster.** Nothing here exercises scheduling across nodes, node
  failure, or a real load balancer.
- **Credentials are committed** in `base/secret.yaml`. They are the same
  development values already in `docker-compose.yml`. The application reads
  `DB_PASSWORD` from the environment precisely so a real deployment can inject it
  from a secret store.
- **The autoscaler targets are guesses.** 200 connections and 300 in-flight
  requests were picked to be demonstrable on one laptop. `docs/benchmarks.md` has
  measured numbers for Compose; nothing equivalent has been run here, and any
  real use should recalibrate them the way `maxInFlight` was.
- **`--kubelet-insecure-tls` on metrics-server.** kind's kubelet serves a
  self-signed certificate with no IP SAN. The clean fix is `serverTLSBootstrap`
  plus manual CSR approval, which would stop this being one command.
- **Single-replica dependencies, no backups, no NetworkPolicy, no
  PodDisruptionBudget, no TLS.**
- **No drain on shutdown for connect.** A rolling update or a scale-down drops
  live WebSocket connections; clients reconnect. The connect layer has no drain
  logic — that is its own outstanding item.

## Things that will bite you

**`kubectl apply -k deployments/k8s/base` does not work.** The base kustomization
generates the PostgreSQL migrations ConfigMap from `db/migrations/`, which is
outside its root, and reading it needs `--load-restrictor LoadRestrictionsNone` —
a flag `kubectl apply -k` does not have. The Makefile renders with `kubectl
kustomize` and pipes into `kubectl apply -f -`. Keeping one copy of the schema
was judged worth the indirection.

**Every role waits for etcd, including `site`.** Not because site uses etcd - it
serves static files - but because `connect/server_tcp.go` has a package-level
`init()` calling `rpc.InitLogicRpcClient()`, and `main.go` imports
`gochat/connect` to offer the module switch, so that `init()` runs in all six
processes. It calls `logrus.Fatalf` when etcd is unreachable. Without an init
container, site crash-loops on a cold cluster for a dependency it never uses.
Every workload therefore has a `wait-for-deps` init container; Kubernetes has no
`depends_on`, and this is the idiomatic replacement.

Without those init containers, logic and task crash-loop until RabbitMQ is ready
and the backoff reaches five minutes, which makes `make k8s-up` look broken when
it is only waiting.

**Changing the Prometheus config needs a pod restart.** There is no reload
sidecar, and Prometheus does not watch its config file, so editing
`monitoring/prometheus/configmap.yaml` and re-applying leaves the old scrape
config running — with stale series from the previous labelling still visible,
which is a convincing way to conclude the change did not work.

```bash
kubectl -n monitoring rollout restart deployment/prometheus
```

**RabbitMQ's probes need a long `timeoutSeconds`.** `rabbitmq-diagnostics` is an
Erlang CLI that takes several seconds to attach to the node, and Kubernetes
defaults `timeoutSeconds` to 1 — which fails every probe and, through the
liveness probe, restarts a RabbitMQ that is working perfectly well. Compose hides
this by allowing its healthchecks 5s.
