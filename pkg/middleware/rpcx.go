package middleware

import (
	"context"
	"runtime"
	"sync"
	"time"

	"gochat/pkg/metrics"
)

// PrometheusRPCPlugin implements RPCX plugin interface for metrics
type PrometheusRPCPlugin struct {
	serviceName string
	timings     sync.Map // map[uint64]time.Time - goroutine ID -> start time
}

func NewPrometheusRPCPlugin(serviceName string) *PrometheusRPCPlugin {
	return &PrometheusRPCPlugin{
		serviceName: serviceName,
	}
}

// getGoroutineID returns the current goroutine ID
func getGoroutineID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	// Parse "goroutine 123 [running]:"
	var id uint64
	for i := 10; i < len(b); i++ {
		if b[i] >= '0' && b[i] <= '9' {
			id = id*10 + uint64(b[i]-'0')
		} else {
			break
		}
	}
	return id
}

func (p *PrometheusRPCPlugin) PreCall(ctx context.Context, serviceName, methodName string, args interface{}) (interface{}, error) {
	metrics.RPCServerRequestsInFlight.WithLabelValues(p.serviceName).Inc()
	// Store start time keyed by goroutine ID
	gid := getGoroutineID()
	p.timings.Store(gid, time.Now())
	// Return the original args unchanged
	return args, nil
}

func (p *PrometheusRPCPlugin) PostCall(ctx context.Context, serviceName, methodName string, args, reply interface{}, err error) error {
	metrics.RPCServerRequestsInFlight.WithLabelValues(p.serviceName).Dec()

	// Retrieve and delete start time
	gid := getGoroutineID()
	if startTime, ok := p.timings.LoadAndDelete(gid); ok {
		if start, ok := startTime.(time.Time); ok {
			duration := time.Since(start).Seconds()
			metrics.RPCServerDuration.WithLabelValues(p.serviceName, methodName).Observe(duration)
		}
	}

	status := "success"
	if err != nil {
		status = "error"
	}
	metrics.RPCServerRequestsTotal.WithLabelValues(p.serviceName, methodName, status).Inc()

	return nil
}
