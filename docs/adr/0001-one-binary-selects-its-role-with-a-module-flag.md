# One binary selects its role with a module flag

summary: All six services build into a single binary that picks its role from `-module`, rather than one binary per service.

status: reconstructed from the implementation; inherited from upstream and kept deliberately.

## Context

The system is six services — logic, connect-ws, connect-tcp, task, api, site —
that share wire types, configuration loading, Redis and RabbitMQ clients, metrics
setup and tracing setup. They are developed together, released together, and
never version-skewed against each other.

## Decision

`main.go` dispatches on a `-module` flag. One binary, one image, six commands:

```
gochat -module logic
gochat -module connect_websocket
```

## Why

Six binaries would mean six build targets, six images to push, and six places for
the shared setup code to drift. Because the services are always deployed as a
matched set, there is nothing to gain from versioning them separately, and a
single image keeps CI to one build and one push.

The cost is a larger image than any one service needs, and a process that links
code it will not run. Neither matters at this size: the binary is tens of
megabytes and starts in milliseconds.

## Alternatives

**One binary per service.** The conventional answer, and the right one once
services are owned by different people or released on different cadences. Neither
is true here.

**A plugin or config-driven role.** More machinery than a switch statement for no
benefit.

## Consequences

- `docker-compose.yml` overrides `command` per service; the image default is api.
- A change to a shared package rebuilds and redeploys everything. Acceptable
  while the services release together; it would not be if they stopped.
