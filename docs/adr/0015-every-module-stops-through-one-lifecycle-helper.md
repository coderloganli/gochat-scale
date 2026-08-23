# Every module stops through one lifecycle helper

summary: Each module's entry point starts its work and returns a stopper instead of blocking or handling signals itself, so one helper owns signal handling for all six roles — which is also what fixes the tracer being shut down at startup.

## Context

One binary selects its role from `-module`
([0001](./0001-one-binary-selects-its-role-with-a-module-flag.md)), but the six
roles did not agree on what happens after startup, and each disagreement caused a
different defect.

- `api.Run()` installed its own `signal.Notify`, shut its HTTP server down, and
  called `os.Exit(0)`.
- `logic.Run()` and `task.Run()` started goroutines and returned, so the
  `signal.Notify` in `main.go` was the one that ran.
- `connect.Run()` blocked in `ListenAndServe` and never returned, so `main.go`'s
  signal handling was unreachable for it — the defect
  [0014](./0014-a-departing-connect-instance-deregisters-before-it-closes-connections.md)
  is about.
- `site.Run()` ended in `logrus.Fatal(http.ListenAndServe(...))`.

The tracer bug follows directly from the same disagreement. `logic.Run()` and
`connect.RunTcp()` register `defer shutdown()` for the tracer and then return, so
the tracer was shut down a few milliseconds after startup and the process ran
untraced for the rest of its life. It was recorded as a known gap in
`docs/architecture.md` and could not be fixed locally: as long as `Run()` returns
while the process keeps running, any `defer` in it fires at the wrong time.

## Decision

**A module starts its work and hands back a way to stop it.** Every entry point
becomes `Start() (lifecycle.Stopper, error)` where
`Stopper = func(context.Context) error`. None of them block, and none of them
handle signals.

`main.go` dispatches on `-module` to the right `Start`, then calls
`lifecycle.WaitAndStop`, which installs the signal handler once, blocks, and runs
the stopper under a context bounded by `lifecycle.ShutdownTimeout`.

`lifecycle.Hooks` composes stoppers in order and continues past an error, so one
failing teardown cannot strand the ones after it.

## Why this rather than leaving each module to itself

Signal handling looks local and is not. Its correctness depends on whether the
function that installed it is still on the stack, which is a property of the
caller — which is exactly how `connect` ended up with a handler that could never
run and `logic` with a tracer that shut itself down. Making the shape uniform
turns "does this module handle SIGTERM" from something to check per module into
something the type signature answers.

The alternative — leaving `Run()` blocking and giving each module its own handler,
as `api` had — works, and was rejected because it puts the same twenty lines in
six places and makes the ordering of teardown steps invisible from the entry
point.

## Consequences

- **The tracer bug is fixed as a side effect, not as a patch.** The tracer's
  shutdown moves from a `defer` in `Run()` into the returned stopper. Affected:
  `logic` and `connect_tcp`. `connect_websocket` was never affected — it blocked,
  so its `defer` never ran at all. `docs/architecture.md` named the wrong pair and
  is corrected.
- **The metrics server is shut down.** `metrics.StartMetricsServer` always
  returned the `*http.Server`; every caller discarded it. The stopper now uses
  the `metrics.ShutdownMetricsServer` that already existed.
- **`api` loses `os.Exit(0)`.** Its HTTP shutdown is unchanged, but it now runs
  under the shared mechanism, so its teardown can be extended without
  reintroducing a private signal handler.
- **Teardown order is written down in one place per module** — the argument list
  of `lifecycle.Hooks` — rather than being implied by where a `defer` happens to
  sit.
- **`TRACING_SAMPLING_RATE` joins the environment overrides.** Dev samples at 1%,
  which is right for load and wrong for asserting that traces exist at all. The
  integration stack sets it to 1.0, so the tracer fix is verified rather than
  claimed — see the principle in `docs/product.md` that every claim is checkable.
