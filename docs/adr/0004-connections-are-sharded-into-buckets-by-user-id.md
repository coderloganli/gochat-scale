# Connections are sharded into buckets by user id

summary: Each connect process splits its connections across one bucket per CPU, chosen by hashing the user id, so that the connection map is not a single contended lock.

status: reconstructed from the implementation; inherited from upstream.

## Context

A connect process holds every live connection in a map from user id to channel.
Every message delivery reads that map; every connect and disconnect writes it. At
the concurrency this service is built for, one mutex over one map serialises the
entire delivery path.

## Decision

Each process creates `cpuNum` buckets (`connect/bucket.go`). A connection is
assigned by `CityHash32(userId) % bucketCount` (`connect/server.go`). Each bucket
owns its own lock, its own channel map, and its own room map.

Room broadcasts get a second layer: each bucket runs `routineAmount` goroutines
reading from buffered channels, so a broadcast is handed off rather than
delivered on the caller's goroutine.

## Why

Sharding turns one contended lock into N mostly-uncontended ones, and the shard
key is available at every call site without a lookup. Hashing rather than
round-robin means the same user always lands in the same bucket, so connect,
deliver and disconnect all touch one bucket and never need to search.

CityHash is fast and distributes short numeric strings evenly, which is what user
ids are. The specific function matters less than the property.

Sizing buckets to CPU count is a heuristic: enough shards that cores are not
waiting on each other, few enough that per-bucket overhead stays small.

## Alternatives

**One lock.** Simplest, and the thing being replaced.

**`sync.Map`.** Removes the explicit lock but not the contention, and gives no
place to hang the per-shard room map or the broadcast goroutines.

**A goroutine per connection owning its own state.** Idiomatic Go, but delivery
needs to find an arbitrary user's connection, which puts a shared index back in
the middle regardless.

## Consequences

- Bucket count is fixed at startup and derives from `cpuNum` in the connect
  config; changing it requires a restart.
- Nothing rebalances. A skewed user id distribution would produce hot buckets.
  Not observed, and not measured for either.
- Room state is per bucket, so a room's members are spread across buckets and a
  room broadcast touches all of them. That is why broadcast is queued to
  per-bucket goroutines rather than done inline.
