package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync/atomic"
	"time"

	"gochat/pkg/health"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// draining is set once a shutdown has begun.
var draining atomic.Bool

// SetDraining marks this process as shutting down. Once set, /ready reports that
// the process should stop being sent work, while /metrics keeps serving so that
// the shutdown itself stays observable, and /health keeps saying the process is
// alive - which it is.
func SetDraining() {
	draining.Store(true)
}

// Draining reports whether a shutdown has begun.
func Draining() bool {
	return draining.Load()
}

// newMux builds the handler served on the metrics port.
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	// Liveness. Unconditional, and deliberately so - including while draining.
	//
	// Draining is the one moment when the difference between the two probes
	// really bites. A pod on its way out must stop receiving traffic, which is
	// readiness; it must emphatically not be restarted, which is what a liveness
	// probe failing would ask the kubelet to do, in the middle of the very
	// shutdown that is trying to close connections cleanly.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Readiness: the registered dependency checks, plus the drain state.
	mux.HandleFunc("/ready", readyHandler)

	// Register pprof endpoints for profiling
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
	mux.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	mux.Handle("/debug/pprof/threadcreate", pprof.Handler("threadcreate"))
	mux.Handle("/debug/pprof/block", pprof.Handler("block"))
	mux.Handle("/debug/pprof/mutex", pprof.Handler("mutex"))
	mux.Handle("/debug/pprof/allocs", pprof.Handler("allocs"))

	return mux
}

// readyCheckTimeout bounds the whole set of readiness checks. A check that
// hangs must not hang the probe: kubelet would see a timeout rather than a 503,
// which reports the same thing far less clearly.
const readyCheckTimeout = 2 * time.Second

func readyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Checked before the dependency checks, and short-circuiting them. Once the
	// process is leaving, whether Redis answers is beside the point, and there
	// is no reason to spend a probe's budget asking.
	if draining.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"status": "draining"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), readyCheckTimeout)
	defer cancel()

	ok, failures := health.Run(ctx)

	if ok {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"status": "ready"})
		return
	}

	// 503 rather than 500: the service is not broken, it is not usable yet. The
	// failing check names are in the body because "not ready" on its own sends
	// whoever is debugging it to the logs of every dependency in turn.
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "not ready",
		"failures": failures,
	})
}

// StartMetricsServer starts an HTTP server for Prometheus metrics and pprof endpoints
func StartMetricsServer(port int) *http.Server {
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: newMux(),
	}

	go func() {
		logrus.Infof("Metrics server listening on :%d", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("Metrics server error: %v", err)
		}
	}()

	return srv
}

// ShutdownMetricsServer gracefully shuts down the metrics server within the
// caller's budget. It must not invent a deadline of its own: the whole shutdown
// is capped by lifecycle.ShutdownTimeout, and a private timeout here would be
// added on top of that cap rather than fitting inside it.
func ShutdownMetricsServer(ctx context.Context, srv *http.Server) error {
	return srv.Shutdown(ctx)
}
