# Services find each other through etcd

summary: Inter-service calls use rpcx with an etcd registry, so replicas can be added and removed without reconfiguring their callers.

status: reconstructed from the implementation; inherited from upstream and load-bearing for the scaling claim.

## Context

api and connect call logic. task calls connect. Both callees are meant to be
replicated, and under Docker Compose a replica's address is assigned when it
starts. A caller cannot be told the address list ahead of time.

task has a harder requirement than round-robin: it must reach *a specific*
connect instance, the one holding the recipient's connection. Redis stores
`userId -> serverId`, and task must turn that `serverId` into a live address.

## Decision

Every logic and connect instance registers itself in etcd on startup, under
`/gochat_srv`, tagging its entry with its `serverId`. Callers use rpcx service
discovery against the same path and watch for changes. task keeps a map from
`serverId` to instances (`task/rpc.go`) and refreshes it as registrations change.

## Why

The `serverId` requirement is what settles it. DNS round-robin, a load balancer,
or compose's built-in service DNS can all spread load across replicas, but none
of them can route to one named instance. A registry that carries per-instance
metadata can, and etcd was already the discovery mechanism rpcx supports.

## Alternatives

**Compose DNS.** Works for logic, where any replica will do. Cannot express "the
connect instance whose serverId is X", which is the whole delivery path.

**Consul.** Equivalent capability. etcd was already a dependency through rpcx;
adding a second registry to gain nothing was not worth it.

**Sticky routing at a load balancer.** Moves the same problem into infrastructure
config and makes the routing rule invisible from the code.

## Consequences

- etcd is a hard dependency at startup. Services panic rather than start without it.
- Registrations refresh on an interval (`UpdateInterval: time.Minute`) and are
  written with `TTL = UpdateInterval * 2`, so a crashed instance stays in the
  registry for up to two minutes. task tolerates a failed call but does not
  redeliver — see the delivery gap in `docs/architecture.md`. An instance that is
  shut down rather than killed deletes its own node first; see
  [0010](./0010-a-departing-connect-instance-deregisters-before-it-closes-connections.md).
- Adding a replica requires no configuration change anywhere.
