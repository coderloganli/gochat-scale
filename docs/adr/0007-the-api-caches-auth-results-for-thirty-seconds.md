# The API caches auth results for thirty seconds

summary: Each api process caches token lookups in memory for 30 s to remove an RPC hop per request, accepting that a logged-out token stays valid for up to 30 s on the instances that did not serve the logout.

status: reconstructed from the implementation; the toggle and this record were added later.

## Context

Every authenticated request ran `CheckAuth` as an RPC to logic, which read the
session from Redis. Two network hops on the hot path of every push, room fetch
and count, to answer a question whose answer almost never changes within the life
of a request burst.

## Decision

`api/cache/auth_cache.go` keeps a process-local map from token to
`(userId, userName)` with a 30 s TTL and a background sweeper. A hit skips the
RPC entirely. Logout deletes the entry on the instance that served it.

`AUTH_CACHE_ENABLED` turns the cache off and `AUTH_CACHE_TTL` changes the window,
so one build can be measured both ways.

## Why

The cache is process-local rather than shared because a shared cache would be
Redis, which is what the RPC was reading in the first place. The point is to
remove the hop, not to move it.

30 s is the trade. The cache is only useful while a token is being used
repeatedly, which happens on the scale of seconds; pushing the TTL higher buys
almost no additional hit rate while lengthening the window in which a revoked
token still works.

## The consistency cost, stated plainly

**A logged-out token keeps working for up to 30 s on every api instance except
the one that handled the logout.** Logout deletes the local entry and the Redis
session; it has no way to reach the other instances' maps.

That is acceptable here — the sessions are chat sessions, and nothing in the
system is destructive enough for a 30 s stale credential to matter. It would not
be acceptable for a system where logout is a security control, and in that case
the right fix is a revocation broadcast, not a shorter TTL.

## Alternatives

**No cache.** What this replaced. Correct, and measurably more expensive.

**Publish revocations to all api instances** over the message bus. Removes the
staleness window at the cost of another subscription path in api. The right
answer if the window ever stops being acceptable.

**Move auth into a signed token (JWT)** so no lookup is needed at all. Trades the
lookup for a harder revocation problem — the same staleness question, with the
window set by token lifetime instead of cache TTL.

## Consequences

- Hit and miss counters are exported (`metrics.AuthCacheHits` / `Misses`), so the
  hit rate is observable rather than assumed.
- The commit that introduced the cache carried no before/after measurement. The
  toggle exists so that number can be produced; it is an open item in
  `docs/benchmarks.md`.
