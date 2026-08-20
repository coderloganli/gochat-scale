# RabbitMQ carries messages from logic to task

summary: Outbound messages go through a durable RabbitMQ exchange instead of Redis pub/sub, so a restarting task does not silently drop traffic.

status: reconstructed from the implementation and the commit that introduced it.

## Context

logic decides where a message goes; task delivers it. Upstream connected the two
with Redis pub/sub. Redis pub/sub is fire-and-forget: a message published while
no subscriber is connected is discarded, with no error and no record. Restarting
task, or having it briefly disconnected, loses every message sent in that window.

There was also no way to apply backpressure. Pub/sub delivers as fast as it can
and a slow consumer just falls behind.

## Decision

logic publishes to a durable direct exchange, `gochat.direct`. Three durable
queues bind to it by message kind:

| Queue | Routing keys |
|---|---|
| `gochat.single` | `single.send` |
| `gochat.room` | `room.send` |
| `gochat.meta` | `room.count`, `room.info` |

task consumes with a prefetch limit (`prefetchCount`, default 10). Redis stays,
but only for what it is good at: sessions, the `userId -> serverId` routing map,
and room membership.

## Why

Durable queues mean a message survives task being down; it is delivered when task
comes back. Prefetch gives a bound on in-flight work per consumer, which is the
backpressure Redis pub/sub could not express. Splitting by kind means a flood of
room broadcasts cannot starve single-user sends, and the three can be scaled or
paused independently.

## Alternatives

**Keep Redis pub/sub.** One fewer component, and Redis was already there. Rejected
because losing messages during a deploy is not a tradeoff worth taking to avoid
running a queue.

**Redis Streams.** Durable, and no new dependency. It would have worked. RabbitMQ
was chosen for routing keys and per-queue consumer semantics, which Streams would
have required building by hand.

**Kafka.** Retention and replay this system has no use for, at a significant
operational cost.

## Consequences

- RabbitMQ is a hard startup dependency for logic and task.
- Durability is per-hop, not end-to-end. A message survives task restarting; it
  does not survive the target connect instance disappearing, because task drops
  it at that point rather than redelivering.
- Queue depth is now a meaningful signal, and worth alerting on.
