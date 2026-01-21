package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// StartMetricsServer starts an HTTP server for Prometheus metrics and pprof endpoints
func StartMetricsServer(port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
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

// ShutdownMetricsServer gracefully shuts down the metrics server
func ShutdownMetricsServer(srv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}
