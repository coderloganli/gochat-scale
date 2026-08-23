# Prometheus discovers pods instead of listing targets

summary: Under Kubernetes, Prometheus finds its targets with pod service discovery and annotations rather than a static list, because a static target sees one replica out of many and because the resulting per-pod labels are what makes custom-metric autoscaling possible at all.

## Context

`deployments/prometheus/prometheus.yml` lists six static targets, one per
service. `docs/architecture.md` already recorded the consequence as a known gap:
with several `logic` replicas, the static target scrapes whichever replica DNS
resolves to, and the metrics describe that one replica as though it were the
service.

Under Kubernetes this stops being a reporting inaccuracy and becomes a blocker.
The autoscaler in ADR 0012 scales `connect-ws` on connections *per pod*. For the
Kubernetes metrics API to attach a Prometheus series to a pod, the series must
carry `namespace` and `pod` labels. A statically scraped target has neither —
prometheus-adapter would have nothing to key on, and the HPA would read
`<unknown>` forever.

## Decision

The Kubernetes deployment ships its own Prometheus configuration
(`deployments/k8s/monitoring/prometheus/`) using `kubernetes_sd_configs` with
`role: pod`, driven by annotations on the pod templates:

```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "<the role's metrics port>"
```

Relabelling keeps annotated pods, builds `__address__` from
`__meta_kubernetes_pod_ip` and the annotated port, and promotes
`__meta_kubernetes_namespace` to `namespace`, `__meta_kubernetes_pod_name` to
`pod`, and `__meta_kubernetes_pod_label_app` to `service`.

Prometheus gets a ServiceAccount and a ClusterRole with `get`, `list` and `watch`
on pods, services, endpoints and nodes.

**The Compose configuration keeps its static list**, and the known gap in
`docs/architecture.md` is amended to say it applies to Compose only, rather than
deleted.

## Why

**Because a replica that is not scraped does not exist.** Scale `connect-ws` to
four and a static target reports one of them. Any conclusion drawn from those
metrics — a dashboard, a capacity number, an autoscaling decision — is drawn from
a quarter of the system.

**Because the labels are the interface.** `namespace` and `pod` are not
decoration on the way to a nicer dashboard; they are the join key between a
Prometheus series and a Kubernetes object. Without them, `custom.metrics.k8s.io`
cannot answer a question about a pod, and the entire autoscaling design in
ADR 0012 has no foundation. This is the least visible dependency in the whole
deployment and the one most likely to be discovered by an HPA that silently
refuses to work.

**Because annotations put the decision next to the workload.** Each Deployment
declares that it should be scraped and on which port, in the same file that
declares the port. Adding a seventh role means adding two annotations, not
editing a central list — which is the same reason services find each other
through etcd rather than a configured address list (ADR 0002).

**Compose keeps the static list** because Compose cannot do better. There is no
equivalent discovery mechanism for `--scale logic=3`, and rewriting the Compose
Prometheus config to chase something it cannot express would add churn without
closing the gap. The gap is real there and stays recorded.

## Alternatives

**`role: endpoints` service discovery.** The more common configuration, and it
would work for the services that have one. It discovers through Service
endpoints, which means readiness gates visibility — a pod that is not ready is
not scraped. For debugging a pod that is failing readiness, that is exactly
backwards, and readiness now has real checks behind it (ADR 0011). `role: pod`
scrapes everything annotated, ready or not.

**The Prometheus Operator and ServiceMonitor CRDs.** The standard answer at
scale, and genuinely better once there are many teams and many services. It also
means installing an operator and its CRDs, and expressing scrape configuration in
a custom resource rather than in the Prometheus configuration format the
repository already contains. Too much machinery for six services, and it would
obscure the mechanism rather than demonstrate it.

**One static target per pod.** Not possible: pod names are not knowable ahead of
time.

**Changing Compose to match.** Nothing to change it to.

## Consequences

- Two Prometheus configurations now exist, one per deployment target, and they
  can drift. They are both short, and both are in `deployments/`.
- Prometheus needs cluster-wide RBAC to list pods. That is more privilege than
  the Compose deployment gives it, and it is the minimum pod discovery requires.
- Every new workload must carry the two annotations or it is invisible. Silent
  when forgotten — the target simply is not there.
- `service` is derived from the pod's `app` label so that the existing Grafana
  dashboards, which group by `service`, keep working against both deployments
  without being rewritten.
