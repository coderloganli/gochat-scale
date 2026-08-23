package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNoChecksIsReady(t *testing.T) {
	reset()
	ok, failures := Run(context.Background())
	if !ok || len(failures) != 0 {
		t.Fatalf("a service with no registered checks should be ready, got ok=%v failures=%v", ok, failures)
	}
}

func TestFailingCheckIsNamed(t *testing.T) {
	reset()
	Register("db", func(context.Context) error { return errors.New("connection refused") })
	Register("redis", func(context.Context) error { return nil })

	ok, failures := Run(context.Background())
	if ok {
		t.Fatal("expected not ready when a check fails")
	}
	if got := failures["db"]; got != "connection refused" {
		t.Fatalf("failing check should be named with its error, got %q", got)
	}
	if _, present := failures["redis"]; present {
		t.Fatal("a passing check must not appear in failures")
	}
}

func TestGateStaysShutUntilAllReport(t *testing.T) {
	reset()
	done := RegisterGate("etcd-registered", 2)

	if ok, _ := Run(context.Background()); ok {
		t.Fatal("gate should be shut before anything reports")
	}
	done()
	if ok, failures := Run(context.Background()); ok {
		t.Fatalf("gate should stay shut after 1 of 2, got failures=%v", failures)
	}
	done()
	if ok, failures := Run(context.Background()); !ok {
		t.Fatalf("gate should open after 2 of 2, got failures=%v", failures)
	}
}

func TestPanickingCheckFailsRatherThanCrashing(t *testing.T) {
	reset()
	Register("exploding", func(context.Context) error { panic("boom") })

	ok, failures := Run(context.Background())
	if ok {
		t.Fatal("a panicking check must not report ready")
	}
	if failures["exploding"] != "check panicked" {
		t.Fatalf("expected the panic to be reported as a failure, got %v", failures)
	}
}

// A check is ordinary code that talks to the network. One that ignores its
// context and blocks must not hang the readiness endpoint - that would turn the
// one endpoint meant to report trouble into trouble of its own.
func TestBlockingCheckDoesNotHangTheProbe(t *testing.T) {
	reset()
	release := make(chan struct{})
	defer close(release)

	Register("wedged", func(context.Context) error {
		<-release // deliberately ignores ctx, as a careless check would
		return nil
	})
	Register("fine", func(context.Context) error { return nil })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	ok, failures := Run(ctx)
	elapsed := time.Since(start)

	if elapsed > time.Second {
		t.Fatalf("Run should return at the deadline, took %s", elapsed)
	}
	if ok {
		t.Fatal("expected not ready when a check has not finished")
	}
	if failures["wedged"] != "timed out" {
		t.Fatalf("the stuck check should be named as timed out, got %v", failures)
	}
	if _, present := failures["fine"]; present {
		t.Fatalf("a check that passed before the deadline must not be reported, got %v", failures)
	}
}

// The deadline path must not mistake "finished just in time" for "stuck".
func TestChecksFinishingBeforeDeadlineAreNotReportedAsTimedOut(t *testing.T) {
	reset()
	Register("slow-but-ok", func(ctx context.Context) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if ok, failures := Run(ctx); !ok {
		t.Fatalf("expected ready, got failures=%v", failures)
	}
}

func TestRegisterReplacesSameName(t *testing.T) {
	reset()
	Register("db", func(context.Context) error { return errors.New("first") })
	Register("db", func(context.Context) error { return nil })

	if ok, failures := Run(context.Background()); !ok {
		t.Fatalf("re-registering a name should replace it, got failures=%v", failures)
	}
	if names := Names(); len(names) != 1 {
		t.Fatalf("expected one registered check, got %v", names)
	}
}
