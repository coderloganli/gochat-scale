# A departing connect instance deregisters before it closes connections

summary: On a shutdown signal, connect removes itself from etcd and refuses new connections first, then closes each live connection with a 1001 close frame, letting the existing disconnect path clear Redis, bounded by a five second cap.

## Context

`connect` holds the long-lived connections, and until now it could not leave the
cluster without damage. `connect.Run()` blocked in `srv.ListenAndServe()` and
never returned, so the `signal.Notify` in `main.go` was never reached for that
module. SIGTERM therefore took Go's default path and killed the process outright.

Three pieces of state outlived the process, and each one causes a different
failure:

- **The connections.** The kernel tore down every open socket. A client cannot
  distinguish that from a network failure, so it has no reason to reconnect
  promptly rather than waiting out a timeout.
- **The etcd registration.** `rpcx-etcd` writes the service node with
  `TTL = UpdateInterval * 2`, two minutes here
  (`serverplugin/etcdv3.go:196`), and nothing deleted it. For up to two minutes
  `task` kept resolving `serverId` to an instance that no longer existed and
  dropping the messages — the `@todo` at `task/push.go:38`.
- **The Redis routing map.** `readPump`'s defer calls the `DisConnect` RPC, which
  a hard kill never runs. The design review found that this is worse than it
  looked: `RpcLogic.DisConnect` (`logic/rpc.go:354`) never cleared the
  `userId -> serverId` key at all. It decrements the room count and removes the
  room membership; the routing key is written by `Connect` (`logic/rpc.go:338`)
  and deleted only by `Logout` (`logic/rpc.go:187`). So the routing map outlived
  an *ordinary* disconnect too, until its TTL expired or a reconnect overwrote it.

The result was a window of up to two minutes after every restart in which
messages to the affected users vanished with nothing recording it. That matters
more here than it would elsewhere: the claim this project exists to demonstrate is
horizontal scaling, and instances leaving is half of scaling. Being able to add a
replica is not worth much if removing one costs a two-minute hole.

## Decision

**Deregister first, then close.** `(*Connect).Stop(ctx)` runs a fixed sequence,
and the order is the substance of the decision — each step is only correct once
the previous one has taken effect:

1. Set the draining flag. `/ws` upgrades are refused with 503, `acceptTcp` closes
   new connections, and `/health` on the metrics port answers 503.
2. Deregister from etcd, by calling `Shutdown(ctx)` on the rpcx servers. rpcx
   calls `Plugins.DoUnregister`, and `EtcdV3RegisterPlugin.Unregister` deletes the
   node *and* drops the name from `p.Services`, so the plugin's TTL-refresh ticker
   cannot re-create it (`rpcx-etcd@v0.1.0/serverplugin/etcdv3.go:217-262`).
3. Shut down the HTTP listener. Upgraded connections are hijacked and not tracked
   by `net/http`, so this returns without waiting on them.
4. Write a close frame with code 1001, Going Away, to every live connection.
5. Let the client's answering close frame fail `ReadMessage`, so `readPump`'s
   existing defer runs — `Bucket.DeleteChannel` and the `DisConnect` RPC.
6. Wait for the connection registry to empty, capped at five seconds. Whatever
   remains is closed outright and counted.

Steps 1 and 2 are before step 4 because closing connections while `task` is still
routing here, or while new clients are still being accepted, does not converge.

Each step takes a slice of the five second budget rather than sharing one
deadline. rpcx's `Shutdown` unregisters and then polls for in-flight requests
until its context expires (`smallnest/rpcx@v1.7.4/server/server.go:885-940`), so
handing it the whole budget would let step 2 starve every step after it.
Deregistration gets 750 ms, the listeners 500 ms, the close frames 500 ms, the
forced teardown 1500 ms, and the metrics and tracer 250 ms each; the drain takes
what those leave.

The slices add to less than the cap deliberately. `WaitAndStop` returns the
moment the cap expires, so a step that overran would not run late — it would not
run. A shutdown sequence that only aims at its budget has no budget.

**`DisConnect` had to be fixed for step 5 to mean anything.** It never deleted the
routing key. It now does, in a Lua script that compares before it deletes, so
that a departing instance's late `DisConnect` cannot delete the mapping of a user
who has already reconnected somewhere else. `proto.DisConnectRequest` carries the
`serverId` for the comparison.

The script reports what it found, and the rest of `DisConnect` turns on it,
because the same race applies to the room. The room count and membership were
being changed unconditionally, so a late disconnect removed a user who was online
on another instance — and graceful shutdown makes that likely rather than rare,
since every client reconnects while the departing instance is still tearing their
old connection down. Three outcomes:

- **the key names another instance** — the user has moved on, so nothing here is
  ours: return without touching the room;
- **the key named us** — delete it and do the room bookkeeping;
- **no key at all** — cleared already or expired, so do the room bookkeeping
  anyway rather than leaking membership. This is also what an old caller that
  sends no `serverId` gets, which keeps the previous behaviour for them.

## Why a registry rather than the buckets

The buckets cannot enumerate the live connections. `Bucket.Put` is called from
`readPump` only after authentication succeeds
([0004](./0004-connections-are-sharded-into-buckets-by-user-id.md)), so a
connection that has been accepted but has not yet authenticated belongs to no
bucket. Draining from the buckets would silently skip exactly the connections
that are most likely to be present during a restart, when clients are
reconnecting. `connect/registry.go` therefore tracks every connection from accept
to read-loop exit, sharded the same way the buckets are so that accept and
disconnect do not contend on one lock.

## Why five seconds, and why it is not configurable

The cap exists because a client that never answers must not hold the process
open. Five seconds is enough for a close handshake on any working connection and
short enough to sit inside a container stop grace period — which is why
`stop_grace_period: 15s` is set on the connect services, against Docker's default
of 10 s.

It is a constant rather than a configuration key deliberately. A knob invites
tuning, tuning invites a number chosen without evidence, and there is no
measurement that would tell anyone what to set it to. `maxInFlight` in
[0009](./0009-the-api-sheds-load-instead-of-queueing-it.md) is a calibrated
number and says so; this one is not, and should not pretend to be.

## What this does not do

**It does not drain queued messages.** Shutdown does not wait for each channel's
`broadcast` buffer to empty. The cap covers the close handshake only. A message
already queued for a connection when the signal arrives may not be written.

**It does not redeliver messages queued for a departing instance.** The
`task/push.go` `@todo` stays open: `task` still resolves `serverId` at delivery
time and drops the message if that instance has gone. This decision narrows the
window from two minutes to the length of a shutdown; it does not close it.

**It does not make clients reconnect.** Sending 1001 makes correct client
behaviour possible. The bundled frontend in `site/` is a prebuilt artifact with no
source in this repository, and implementing reconnection there is separate work.

## What it measured

`make drain-demo`, 50 connections split across two replicas, one restarted. Same
image both arms, one environment variable between them (`docs/benchmarks.md`,
evidence in `loadtest/reports/drain/`):

| | control | graceful |
|---|---|---|
| Closed with 1001 going away | 0 (0%) | 25 (100%) |
| Time to notice, p50 | 46.7 ms | 52.1 ms |
| etcd registration outlived the stop by | still present after 20 s | 683 ms |

**Time to notice is the same in both arms, and that is the honest reading.** The
TCP close reaches the client at the same moment either way. This decision does
not shorten the outage; it changes an event indistinguishable from a network
failure into one the client can act on. The control arm's connections close with
1006, abnormal closure.

**The etcd number is the large one.** Sub-second against a registration still
standing 20 seconds later, with a two-minute TTL as its real ceiling. That window
is `task` delivering into a hole.

**An unlooked-for consequence.** The surviving replica's clients received 625 more
frames in the graceful arm, which is exactly 25 disconnects × 25 survivors: each
`DisConnect` republishes room membership. In the control arm no disconnect runs,
so the remaining clients are never told the room emptied and their membership
view stays stale until something else updates it. Not extra chat throughput —
extra correctness.

## Consequences

- **Ordinary disconnects get cleaner too.** The `DisConnect` fix is not
  shutdown-specific: every disconnect now clears the routing key instead of
  leaving it to a TTL. That is a behaviour change outside the stated scope of this
  decision, kept because the decision cannot be honest without it.
- **`Bucket.DeleteChannel` gained an identity check.** It looked its entry up by
  `ch.userId` and deleted whatever was currently mapped
  (`connect/bucket.go:88-101`), so with two connections for one user the older
  one's teardown removed the newer one's entry. Pre-existing, but a drain is
  precisely when it fires: every client reconnects while the departing instance is
  still tearing their old connection down.
- `GOCHAT_GRACEFUL_SHUTDOWN` exists solely to produce the control arm, defaulting
  to true. It is a measurement switch, not an operational one, and an
  unparseable value resolves to true rather than to the control arm.
- Two `RegisterOnShutdown` hooks were deleted rather than fixed. In rpcx v1.7.4
  the `onShutdown` slice is appended to and never read
  (`smallnest/rpcx@v1.7.4/server/server.go:92,865`), so the `s.UnregisterAll()`
  registered in `connect/rpc.go` and `logic/publish.go` had never run. What
  actually deregisters is `Shutdown` itself. Code that looks like the mechanism
  but is not is worse than no code.
- `/health` now has two meanings — alive, and ready — which is what a Kubernetes
  readiness probe will want when that work happens.
- Shutdown reports on itself: `gochat_shutdown_connections_closed_total` splits
  connections into those that answered the close frame and those the cap forced,
  so a shutdown that is quietly hitting the cap is visible rather than inferred.
- See [0002](./0002-services-find-each-other-through-etcd.md) for the registration
  this deregisters from, and
  [0015](./0015-every-module-stops-through-one-lifecycle-helper.md) for the
  mechanism that delivers the signal.
