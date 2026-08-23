# Kubernetes manifests are Kustomize over a pinned kind cluster

summary: The Kubernetes deployment is plain manifests with a Kustomize base and overlay, run on a kind cluster whose configuration and node image digest are committed, so that the deployment is reproducible rather than described.

## Context

The project shipped one deployment target, Docker Compose. A second target was
wanted for the reason most backends want one: Kubernetes is where this kind of
system actually runs, and the questions it forces — what is a readiness probe
worth, which signal should drive autoscaling — are questions Compose never asks.

Two things had to be decided before any manifest could be written: what form the
manifests take, and what cluster they are proven against. The second matters more
than it looks. A repository full of YAML that nobody has applied is a claim with
nothing behind it, and this project's stated principle is that every claim is
checkable.

## Decision

**Kustomize**, with `deployments/k8s/base` and `deployments/k8s/overlays/dev`,
plus separate roots for `monitoring` and `autoscaling`.

**kind**, configured by a committed `deployments/k8s/kind-cluster.yaml` that pins
the node image by digest to
`kindest/node:v1.34.8@sha256:02722c2dedddcfc00febf5d27fbeb9b7b2c14294c82109ff4a85d89ac9ba3256`.
The image is loaded with `kind load docker-image`; there is no registry.

The cluster config declares `extraPortMappings` from host ports 8080, 7070, 7000,
3000 and 19090 to fixed NodePorts, so the application is reachable on the same
host ports Compose publishes.

## Why

**Kustomize because there is nothing to install.** `kubectl` v1.34 embeds
kustomize v5.7. The only new tool anyone needs is kind itself. And `base` +
`overlays/dev` is the same shape as the existing `docker-compose.yml` +
`deployments/docker-compose.dev.yml`, so a reader who understands one deployment
can navigate the other without learning a new idea.

**kind because its cluster is a file.** The configuration lives in the
repository, so the cluster someone else creates is the cluster this was verified
on. Pinning the node image by digest is the same instinct that pins every other
image here.

**The port mappings because of a constraint in the frontend.** `site/static/js/`
is a prebuilt React bundle committed to the repository and not built from source
here, and it hardcodes `http://127.0.0.1:7070` and `ws://127.0.0.1:7000/ws`.
Mapping those exact host ports into the cluster is what makes the UI work
untouched. An Ingress would not: it would need the frontend rebuilt to take its
endpoints from configuration, which is a different task.

## Alternatives

**Helm.** The obvious alternative and a common job requirement. Rejected because
ten workloads that differ in a module flag, a port and a probe path do not need a
templating language — they need a base and a patch, which is exactly what
Kustomize is. Writing them as Helm templates would make the manifests less
readable to gain parameterisation nobody is going to use, and would add a tool to
install before anything can be rendered.

**Docker Desktop's built-in Kubernetes.** Nothing to install, but the cluster is
a checkbox in a GUI rather than a file in the repository. It cannot be pinned, it
cannot be recreated from the repository, and it cannot be described in a way
someone else can follow exactly.

**minikube.** Equivalent for this purpose. kind builds its nodes from container
images that can be pinned by digest and is what the Kubernetes project itself
tests with; there was no second reason to prefer minikube.

**An Ingress controller instead of NodePorts.** More realistic, and dead on
arrival against a frontend bundle with hardcoded localhost endpoints.

## Consequences

- Compose and Kubernetes both bind host ports 8080, 7070 and 7000. They cannot
  run at the same time. `docs/kubernetes.md` says so.
- kind is a single-node cluster, so nothing here exercises scheduling across
  nodes, node failure, or a real load balancer. Replica counts still prove out
  the routing, which is the part that was actually in question.
- No Ingress and no TLS. Both wait on the frontend becoming configurable.
- The manifests are not covered by automated tests. Verification is a real
  cluster run, recorded in `docs/kubernetes.md`; a later edit to the YAML has
  nothing catching it but another run. This was a deliberate trade and it is the
  weakest point of the arrangement.
