package metrics

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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

// Test case 10: /health after SetDraining() -> 503 with body draining.
func TestHealthReportsDraining(t *testing.T) {
	SetDraining()

	w := get(t, "/health")

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("/health while draining: got %d, want 503", w.Code)
	}
	if got := w.Body.String(); got != "draining" {
		t.Fatalf("/health body while draining: got %q, want %q", got, "draining")
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
	// to wait for and cannot return immediately on its own.
	busy := make(chan struct{})
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
