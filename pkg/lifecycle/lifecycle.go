// Package lifecycle owns what happens between a module starting and the process
// exiting. Every module's entry point starts its work, returns a Stopper, and
// does not block; this package installs the one signal handler and runs the
// stopper under a bounded context.
package lifecycle

import (
	"context"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

// ShutdownTimeout caps a whole shutdown. It is a constant by decision, not a
// configuration key: nothing measures what it should be, so a knob would only
// invite a number chosen without evidence. See
// docs/adr/0010-a-departing-connect-instance-deregisters-before-it-closes-connections.md.
const ShutdownTimeout = 5 * time.Second

// GracefulEnv switches graceful shutdown off, restoring the behaviour of a
// process that does not handle the signal at all. It exists to produce the
// control arm of the measurement in docs/benchmarks.md and for nothing else.
const GracefulEnv = "GOCHAT_GRACEFUL_SHUTDOWN"

// Stopper releases what a module started. It must respect the context it is
// given and must be safe to call exactly once.
type Stopper func(context.Context) error

// Enabled reports whether shutdown should be graceful. It defaults to true, and
// an unparseable value resolves to true rather than to the control arm: a
// setting nobody can read must not silently select the behaviour we are trying
// to get rid of.
func Enabled() bool {
	raw, ok := os.LookupEnv(GracefulEnv)
	if !ok || raw == "" {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		logrus.Warnf("%s=%q is not a boolean; keeping graceful shutdown on", GracefulEnv, raw)
		return true
	}
	return enabled
}

// Hooks composes stoppers into one, running them in the order given. It
// continues past an error so that one failing teardown cannot strand the ones
// after it, and returns the first error.
func Hooks(stoppers ...Stopper) Stopper {
	return func(ctx context.Context) error {
		var first error
		for _, stop := range stoppers {
			if stop == nil {
				continue
			}
			if err := stop(ctx); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
}

// WaitAndStop blocks until a shutdown signal arrives, then runs stop under a
// context bounded by ShutdownTimeout. When Enabled reports false it returns as
// soon as the signal arrives, without calling stop.
func WaitAndStop(module string, stop Stopper) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(sig)

	waitAndStop(module, stop, sig)
}

// waitAndStop is WaitAndStop with the signal source injected, so that a test can
// deliver a signal without sending a real one to the test process.
func waitAndStop(module string, stop Stopper, sig <-chan os.Signal) {
	received := <-sig
	logrus.Infof("%s received %s", module, received)

	if !Enabled() {
		logrus.Warnf("%s=false: exiting without a graceful shutdown", GracefulEnv)
		return
	}
	if stop == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
	defer cancel()

	// A stopper that ignores its context must not be able to hold the process
	// open, so wait on the budget rather than on the stopper.
	done := make(chan error, 1)
	go func() { done <- stop(ctx) }()

	select {
	case err := <-done:
		if err != nil {
			logrus.Errorf("%s shutdown: %v", module, err)
			return
		}
		logrus.Infof("%s stopped cleanly", module)
	case <-ctx.Done():
		logrus.Errorf("%s shutdown did not finish within %v", module, ShutdownTimeout)
	}
}
