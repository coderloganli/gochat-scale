package lifecycle

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Test case 1: Enabled() with GOCHAT_GRACEFUL_SHUTDOWN unset -> true.
func TestEnabledDefaultsToTrue(t *testing.T) {
	os.Unsetenv(GracefulEnv)

	if !Enabled() {
		t.Fatal("Enabled() with the variable unset: got false, want true")
	}
}

// Test case 2: Enabled() with the value "false" -> false; with "true" -> true.
func TestEnabledReadsTheVariable(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"false", false},
		{"true", true},
	} {
		t.Setenv(GracefulEnv, tc.value)

		if got := Enabled(); got != tc.want {
			t.Errorf("Enabled() with %q: got %v, want %v", tc.value, got, tc.want)
		}
	}
}

// Test case 3: Enabled() with an unparseable value -> true. The safe default is
// graceful, not the control arm.
func TestEnabledFallsBackToGracefulOnGarbage(t *testing.T) {
	t.Setenv(GracefulEnv, "yes-please")

	if !Enabled() {
		t.Fatal("Enabled() with an unparseable value: got false, want true — an " +
			"unreadable setting must not silently select the control arm")
	}
}

// Test case 4: WaitAndStop when enabled calls the stopper exactly once, with a
// context whose deadline is within a tolerance of ShutdownTimeout from now.
func TestWaitAndStopCallsTheStopperOnceWithABoundedContext(t *testing.T) {
	t.Setenv(GracefulEnv, "true")

	var (
		mu       sync.Mutex
		calls    int
		deadline time.Time
		hadDL    bool
	)

	sig := make(chan os.Signal, 1)
	sig <- syscall.SIGTERM

	before := time.Now()
	waitAndStop("test", func(ctx context.Context) error {
		mu.Lock()
		defer mu.Unlock()
		calls++
		deadline, hadDL = ctx.Deadline()
		return nil
	}, sig)

	mu.Lock()
	defer mu.Unlock()

	if calls != 1 {
		t.Fatalf("stopper called %d times, want exactly 1", calls)
	}
	if !hadDL {
		t.Fatal("stopper received a context with no deadline, want one bounded by ShutdownTimeout")
	}
	got := deadline.Sub(before)
	if got < ShutdownTimeout-time.Second || got > ShutdownTimeout+time.Second {
		t.Fatalf("context deadline is %v from the signal, want about %v", got, ShutdownTimeout)
	}
}

// Test case 5: WaitAndStop when disabled returns without calling the stopper.
func TestWaitAndStopSkipsTheStopperWhenDisabled(t *testing.T) {
	t.Setenv(GracefulEnv, "false")

	called := make(chan struct{}, 1)

	sig := make(chan os.Signal, 1)
	sig <- syscall.SIGTERM

	waitAndStop("test", func(ctx context.Context) error {
		called <- struct{}{}
		return nil
	}, sig)

	select {
	case <-called:
		t.Fatal("stopper was called with graceful shutdown disabled, want it skipped")
	default:
	}
}

// Test case 6: WaitAndStop with a stopper that blocks forever returns within
// ShutdownTimeout plus a tolerance, and the context passed to the stopper is
// done by then.
func TestWaitAndStopDoesNotOutlastItsBudget(t *testing.T) {
	t.Setenv(GracefulEnv, "true")

	gotCtx := make(chan context.Context, 1)

	sig := make(chan os.Signal, 1)
	sig <- syscall.SIGTERM

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		waitAndStop("test", func(ctx context.Context) error {
			gotCtx <- ctx
			<-ctx.Done()
			// Outlast the budget on purpose: a stopper that ignores its context
			// must not be able to hold the process open.
			time.Sleep(500 * time.Millisecond)
			return ctx.Err()
		}, sig)
		done <- time.Since(start)
	}()

	select {
	case took := <-done:
		if took > ShutdownTimeout+2*time.Second {
			t.Fatalf("waitAndStop took %v, want at most about %v", took, ShutdownTimeout)
		}
	case <-time.After(ShutdownTimeout + 5*time.Second):
		t.Fatal("waitAndStop did not return; a blocking stopper must not hold the process open")
	}

	select {
	case ctx := <-gotCtx:
		if ctx.Err() == nil {
			t.Fatal("the stopper's context was still live after the budget, want it cancelled")
		}
	default:
		t.Fatal("the stopper never received a context")
	}
}

// Test case 7: Hooks runs its stoppers in the order given.
func TestHooksRunsInOrder(t *testing.T) {
	var order []string

	stop := Hooks(
		func(context.Context) error { order = append(order, "first"); return nil },
		func(context.Context) error { order = append(order, "second"); return nil },
		func(context.Context) error { order = append(order, "third"); return nil },
	)

	if err := stop(context.Background()); err != nil {
		t.Fatalf("Hooks returned %v, want nil", err)
	}

	want := []string{"first", "second", "third"}
	if len(order) != len(want) {
		t.Fatalf("ran %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("ran %v, want %v", order, want)
		}
	}
}

// Test case 8: Hooks runs every stopper even when an earlier one returns an
// error, and returns the first error.
func TestHooksContinuesPastAnErrorAndReturnsTheFirst(t *testing.T) {
	first := errors.New("first failure")
	second := errors.New("second failure")

	var ran []string

	stop := Hooks(
		func(context.Context) error { ran = append(ran, "a"); return first },
		func(context.Context) error { ran = append(ran, "b"); return second },
		func(context.Context) error { ran = append(ran, "c"); return nil },
	)

	err := stop(context.Background())

	if len(ran) != 3 {
		t.Fatalf("ran %v, want all three — one failing teardown must not strand the rest", ran)
	}
	if !errors.Is(err, first) {
		t.Fatalf("Hooks returned %v, want the first error %v", err, first)
	}
}
