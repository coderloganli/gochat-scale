# Product design

## What this product is

GoChat is a chat service: people join rooms, send messages to a room or to one
other person, and see them arrive immediately without refreshing anything. A
small web frontend ships with it so the system can be used, not just described.

It is also, honestly, a reference implementation. The audience that matters most
is an engineer reading the repository to judge whether the design holds up. That
shapes what gets built: the hard parts are the ones that are hard at scale —
connection handling, message routing across replicas, measuring where it breaks —
not the ones that make a chat app pleasant to use.

## Who it is for

**Engineers evaluating the design.** They want to know how connection state is
kept separate from business logic, what happens when a service is replicated,
where the bottleneck is, and whether the numbers in the README are backed by
anything. They read `docs/architecture.md`, `docs/benchmarks.md` and the ADRs
before they read any handler.

**Anyone running it.** One command brings the whole system up, one flag scales a
service, and the dashboards show what it is doing. If that takes more than a few
minutes, the project has failed at its main job.

## What it does

- Register and log in.
- Hold a live connection — WebSocket for browsers, TCP for other clients — and
  receive messages pushed down it.
- Send a message to one user, or to a room.
- See who is in a room and how many are online.
- Run replicated: any number of logic, api, connect and task instances, with a
  user's messages reaching them wherever their connection happens to live.
- Report on itself: Prometheus metrics, Grafana dashboards, Jaeger traces, and a
  load test model that produces per-step capacity data.

## What it deliberately does not do

- **Message history.** Messages are delivered, not stored. Anyone offline when a
  message is sent does not get it later.
- **Delivery guarantees.** If the connect instance holding a recipient dies while
  a message is queued for it, that message is dropped.
- **Anything social.** No friends, presence beyond room counts, typing
  indicators, read receipts, reactions, or attachments.
- **Multi-tenancy, moderation, or accounts beyond a name and a password.**
- **Transport security.** TLS is a deployment concern and is not terminated here.

The boundary is deliberate. Each of those features would add surface without
adding anything to the question the project exists to answer — how a chat backend
behaves when it is scaled and put under load.

## Principles

**Every claim is checkable.** If the README says a number, a tracked file
produces it. If it says a service scales, there is a way to run it scaled. A
claim with nothing behind it is worse than no claim.

**The interesting part is the failure.** Knowing that the system serves 5,800
requests a second matters less than knowing what happens at 6,000, why, and what
would change it. Capacity work that stops at the happy path has stopped too early.

**State lives in exactly one place.** User data in PostgreSQL, sessions and
routing in Redis, connections in the connect process. When a piece of state ends
up in two places, replicas start disagreeing, and the scaling story quietly stops
being true.

**Prefer a boring dependency to a clever one.** The stack is PostgreSQL, Redis,
RabbitMQ and etcd because each is doing the obvious thing it is good at. A
component earns its place by removing a problem, not by being interesting.

**Nothing is a special case in production.** The same binary, the same config
mechanism and the same migrations run in dev and in prod; only the values differ.
