package connect

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"gochat/pkg/metrics"
)

// Budgets for the steps that could otherwise consume the whole shutdown.
//
// rpcx's Shutdown unregisters and then polls for in-flight requests until its
// context expires, so handing it the whole budget would starve every step after
// it. The listener shutdown is bounded for the same reason.
//
// They add up to less than lifecycle.ShutdownTimeout on purpose:
// 750ms + 500ms + 500ms + drain + 1500ms + 250ms, with the drain taking
// whatever the others leave. Stop has to fit inside the cap rather than merely
// aim at it — the process returns the moment the cap expires, so a step that
// overran would simply not happen.
const (
	deregisterBudget = 750 * time.Millisecond
	listenerBudget   = 500 * time.Millisecond
	closeFrameWait   = 250 * time.Millisecond

	// epilogueBudget bounds the metrics and tracer teardown, out of what is left.
	epilogueBudget = 250 * time.Millisecond

	// forcedTeardownGrace is held back from the drain for the connections the
	// cap forces. Closing a connection is what makes its read loop run its
	// teardown — bucket removal and the DisConnect RPC that clears Redis — so
	// without this the process exits while those calls are still in flight and
	// a forced connection leaves exactly the state this shutdown exists to
	// clean up.
	//
	// It matters more than it looks: a client that is not actively reading never
	// answers a close frame, because gorilla only replies from inside a read
	// call. Those clients are always forced.
	forcedTeardownGrace = 1500 * time.Millisecond
)

// Stop takes this instance out of the cluster and closes what it holds.
//
// The order is the design, and each step is only correct once the previous one
// has taken effect: deregister before closing connections, or task keeps routing
// here; refuse new connections before closing the old ones, or the drain never
// converges.
//
// See docs/adr/0010-a-departing-connect-instance-deregisters-before-it-closes-connections.md.
func (c *Connect) Stop(ctx context.Context) error {
	start := time.Now()
	role := c.roleName()

	// 1. Stop taking new work.
	if c.server != nil {
		c.server.SetDraining()
	}
	metrics.SetDraining()

	// 2. Deregister from etcd, so task stops routing here before anything is
	// closed. rpcx's Shutdown is what performs the unregister; the
	// RegisterOnShutdown hooks this code used to install never ran.
	c.shutdownRpcServers(ctx)

	// 3. Stop the listeners.
	c.closeListeners(ctx)

	// 4-6. Close the connections, and report how they ended.
	answered, forced := c.closeConnections(ctx)
	metrics.ShutdownConnectionsClosedTotal.WithLabelValues(role, "close_frame").Add(float64(answered))
	metrics.ShutdownConnectionsClosedTotal.WithLabelValues(role, "forced").Add(float64(forced))

	// 7. Metrics server, then tracer, so that a failure during shutdown is still
	// scraped and still traced.
	//
	// Both take what is left of the caller's budget rather than a private one.
	// The process returns as soon as the budget expires, so a private timeout
	// here would not buy the extra time it asks for — it would only make Stop
	// claim a budget it does not have.
	var err error
	if c.metricsSrv != nil {
		stopCtx, cancel := withBudget(ctx, epilogueBudget)
		if e := metrics.ShutdownMetricsServer(stopCtx, c.metricsSrv); e != nil {
			logrus.Warnf("metrics server shutdown: %v", e)
			err = e
		}
		cancel()
	}

	metrics.ShutdownDurationSeconds.WithLabelValues(role).Observe(time.Since(start).Seconds())

	if c.tracerShutdown != nil {
		stopCtx, cancel := withBudget(ctx, epilogueBudget)
		if e := c.tracerShutdown(stopCtx); e != nil {
			logrus.Warnf("tracer shutdown: %v", e)
			if err == nil {
				err = e
			}
		}
		cancel()
	}

	logrus.Infof("%s stopped: %d connections answered the close frame, %d were forced, took %v",
		role, answered, forced, time.Since(start))
	return err
}

func (c *Connect) roleName() string {
	if c.metricsRoleName != "" {
		return c.metricsRoleName
	}
	return "connect"
}

func (c *Connect) shutdownRpcServers(ctx context.Context) {
	for _, s := range c.rpcServers {
		if s == nil {
			continue
		}
		stopCtx, cancel := withBudget(ctx, deregisterBudget)
		if err := s.Shutdown(stopCtx); err != nil {
			logrus.Warnf("rpc server shutdown: %v", err)
		}
		cancel()
	}
}

func (c *Connect) closeListeners(ctx context.Context) {
	if c.httpSrv != nil {
		// Upgraded connections are hijacked and not tracked by net/http, so this
		// returns as soon as the listener is closed rather than waiting on them.
		stopCtx, cancel := withBudget(ctx, listenerBudget)
		if err := c.httpSrv.Shutdown(stopCtx); err != nil {
			logrus.Warnf("websocket listener shutdown: %v", err)
		}
		cancel()
	}
	for _, ln := range c.tcpListeners {
		if ln == nil {
			continue
		}
		// Closing the listener is what unblocks the acceptTcp goroutines.
		if err := ln.Close(); err != nil {
			logrus.Warnf("tcp listener close: %v", err)
		}
	}
}

// closeConnections writes a close frame to every live connection and waits for
// them to go, bounded by ctx. It reports how many answered the close handshake
// and how many the budget forced.
func (c *Connect) closeConnections(ctx context.Context) (answered, forced int) {
	if c.server == nil || c.server.Registry == nil {
		return 0, 0
	}

	open := c.server.Registry.Snapshot()
	if len(open) == 0 {
		return 0, 0
	}

	// Written concurrently, and bounded. Serially, a per-connection write
	// deadline multiplies by the number of connections — at ten thousand
	// connections that is not a bound at all — and a blocked socket would eat
	// the budget before the wait step started. Gorilla documents Close and
	// WriteControl as callable concurrently with all other methods, which is
	// what makes both the fan-out and the running writePump safe.
	frameCtx, cancelFrames := withBudget(ctx, closeFrameWait*2)
	var wg sync.WaitGroup
	for _, ch := range open {
		if ch.conn == nil {
			// TCP has no close frame; those connections are closed below.
			continue
		}
		wg.Add(1)
		go func(ch *Channel) {
			defer wg.Done()
			if err := ch.conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutting down"),
				time.Now().Add(closeFrameWait),
			); err != nil {
				logrus.Debugf("write close frame: %v", err)
			}
		}(ch)
	}

	// Do not wait past the budget for a socket that will not take a write; the
	// force-close below covers whatever did not get one.
	frames := make(chan struct{})
	go func() { wg.Wait(); close(frames) }()
	select {
	case <-frames:
	case <-frameCtx.Done():
		logrus.Warnf("close frames did not all go out within %v", closeFrameWait*2)
	}
	cancelFrames()

	// Each client's answering close frame fails its ReadMessage, so the read
	// loop's existing teardown runs — bucket removal and the DisConnect RPC that
	// clears Redis. A channel leaves the registry only once that has finished.
	drainCtx, cancelDrain := withBudget(ctx, budgetMinus(ctx, forcedTeardownGrace))
	_ = c.server.Registry.WaitEmpty(drainCtx)
	cancelDrain()

	remaining := c.server.Registry.Snapshot()
	for _, ch := range remaining {
		if ch.conn != nil {
			_ = ch.conn.Close()
		}
		if ch.connTcp != nil {
			_ = ch.connTcp.Close()
		}
	}

	// Closing them is what starts their teardown, so give it the grace the drain
	// held back. Bounded by what is left of ctx as well as by the grace: this is
	// the step budgetMinus reserved for, so on the normal path the reserve is
	// there, and when an earlier step overran there is nothing to spend anyway.
	if len(remaining) > 0 {
		graceCtx, cancelGrace := withBudget(ctx, forcedTeardownGrace)
		_ = c.server.Registry.WaitEmpty(graceCtx)
		cancelGrace()
	}

	forced = len(remaining)
	answered = len(open) - forced
	if answered < 0 {
		answered = 0
	}
	return answered, forced
}

// budgetMinus is what is left of ctx after holding back the reserve.
func budgetMinus(ctx context.Context, reserve time.Duration) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return reserve
	}
	remaining := time.Until(deadline) - reserve
	if remaining < 100*time.Millisecond {
		return 100 * time.Millisecond
	}
	return remaining
}

// withBudget derives a context that expires at the earlier of the caller's
// deadline and the budget given, so no single step can consume the whole
// shutdown.
func withBudget(ctx context.Context, budget time.Duration) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < budget {
			budget = remaining
		}
	}
	if budget <= 0 {
		c, cancel := context.WithCancel(ctx)
		cancel()
		return c, func() {}
	}
	return context.WithTimeout(ctx, budget)
}
