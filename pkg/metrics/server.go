package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// draining is set once a shutdown has begun. It gives /health two meanings —
// alive, and ready — which is the distinction a load balancer or a Kubernetes
// readiness probe needs to stop sending work to an instance that is on its way
// out.
var draining atomic.Bool

// SetDraining marks this process as shutting down. Once set, /health reports
// that the process is no longer ready to take work, while /metrics keeps
// serving so that the shutdown itself stays observable.
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
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if draining.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("draining"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

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
