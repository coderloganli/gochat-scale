package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"gochat/pkg/health"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// StartMetricsServer starts an HTTP server for Prometheus metrics and pprof endpoints
func StartMetricsServer(port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	// Liveness. Deliberately unconditional: it answers "is this process wedged",
	// and a dependency being down is not a reason to restart anything. Checking
	// dependencies here is how a database outage becomes a cluster-wide crash
	// loop. See docs/adr/0011.
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Readiness. Answers "should I be sent traffic", which is a different
	// question, and the only one that consults the registered checks.
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

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		logrus.Infof("Metrics server listening on :%d", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("Metrics server error: %v", err)
		}
	}()

	return srv
}

// readyCheckTimeout bounds the whole set of readiness checks. A check that
// hangs must not hang the probe: kubelet would see a timeout rather than a 503,
// which reports the same thing far less clearly.
const readyCheckTimeout = 2 * time.Second

func readyHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyCheckTimeout)
	defer cancel()

	ok, failures := health.Run(ctx)

	w.Header().Set("Content-Type", "application/json")
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

// ShutdownMetricsServer gracefully shuts down the metrics server
func ShutdownMetricsServer(srv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
