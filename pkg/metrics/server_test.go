package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// Test case 9: /health before SetDraining() -> 200 with body OK.
func TestHealthIsOkBeforeDraining(t *testing.T) {
	w := get(t, "/health")

	if w.Code != http.StatusOK {
		t.Fatalf("/health: got %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != "OK" {
		t.Fatalf("/health body: got %q, want %q", got, "OK")
	}
}

// Test case 10, revised where this branch met the Kubernetes work on master.
//
// It originally asserted that /health returns 503 while draining. That was the
// right call when /health was the only endpoint there was: something had to
// carry the "stop sending me work" signal. master added /ready, and with both
// present the signal belongs there, for two reasons.
//
// The first is that it otherwise does not work. The readiness probe in
// deployments/k8s reads /ready, so a 503 on /health would not take the pod out
// of the Service endpoints at all - the exact thing draining is for.
//
// The second is that it would do harm. The liveness probe reads /health, and a
// failing liveness probe asks the kubelet to restart the container - in the
// middle of the shutdown that is trying to close connections cleanly.
func TestDrainingWithdrawsReadinessButNotLiveness(t *testing.T) {
	SetDraining()

	if w := get(t, "/ready"); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ready while draining: got %d, want 503 - a departing instance "+
			"must leave the Service endpoints", w.Code)
	}

	w := get(t, "/health")
	if w.Code != http.StatusOK {
		t.Fatalf("/health while draining: got %d, want 200 - a draining pod is "+
			"alive and must not be restarted mid-shutdown", w.Code)
	}
	if got := w.Body.String(); got != "OK" {
		t.Fatalf("/health body while draining: got %q, want %q", got, "OK")
	}
}

// Test case 11: /metrics still serves after SetDraining() — a draining process
// must stay observable.
func TestMetricsStillServesWhileDraining(t *testing.T) {
	SetDraining()

	w := get(t, "/metrics")

	if w.Code != http.StatusOK {
		t.Fatalf("/metrics while draining: got %d, want 200 — a shutdown that "+
			"cannot be scraped cannot be measured", w.Code)
	}
}

// Test case 12: ShutdownMetricsServer with an already-cancelled context returns
// promptly and returns that context's error rather than waiting out a timeout of
// its own.
func TestShutdownMetricsServerRespectsTheCallersContext(t *testing.T) {
	srv := &http.Server{Handler: newMux()}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// A connection the server is still handling, so that Shutdown has something
	// to wait for and cannot return immediately on its own. Shutdown only waits
	// for connections it sees as active, so the test must not race ahead of the
	// server: writing the request is not enough, because the connection stays
	// idle until the server has read it and entered the handler. inHandler is
	// what makes that ordering certain.
	busy := make(chan struct{})
	inHandler := make(chan struct{})
	var once sync.Once
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(inHandler) })
		<-busy
	})
	go srv.Serve(ln)
	defer close(busy)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("GET /health HTTP/1.1\r\nHost: x\r\n\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	select {
	case <-inHandler:
	case <-time.After(10 * time.Second):
		t.Fatal("the server never began handling the request; Shutdown would have " +
			"had no active connection to wait for and the test would prove nothing")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	err = ShutdownMetricsServer(ctx, srv)
	took := time.Since(start)

	if took > time.Second {
		t.Fatalf("ShutdownMetricsServer took %v with a cancelled context; it must "+
			"fit inside the caller's budget rather than adding a timeout of its own", took)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ShutdownMetricsServer returned %v, want the caller's context error", err)
	}
}
