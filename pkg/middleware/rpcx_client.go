package middleware

import (
	"context"
	"time"

	"gochat/pkg/metrics"

	"github.com/smallnest/rpcx/client"
)

// InstrumentedCall wraps an RPC call and records client-side metrics.
// It tracks request count, duration, and error rate by source service,
// target service, and method name.
func InstrumentedCall(
	ctx context.Context,
	xc client.XClient,
	sourceService, targetService, method string,
	args, reply interface{},
) error {
	start := time.Now()

	err := xc.Call(ctx, method, args, reply)

	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
	}

	metrics.RPCClientDuration.WithLabelValues(sourceService, targetService, method).Observe(duration)
	metrics.RPCClientRequestsTotal.WithLabelValues(sourceService, method, status).Inc()

	return err
}

// InstrumentedCallWithTargetLabel is like InstrumentedCall but includes target_service
// in the requests_total metric for more granular error tracking.
func InstrumentedCallWithTargetLabel(
	ctx context.Context,
	xc client.XClient,
	sourceService, targetService, method string,
	args, reply interface{},
) error {
	start := time.Now()

	err := xc.Call(ctx, method, args, reply)

	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
	}

	metrics.RPCClientDuration.WithLabelValues(sourceService, targetService, method).Observe(duration)
	// Use target_service as the "method" label for granular tracking
	metrics.RPCClientRequestsTotal.WithLabelValues(sourceService, targetService+"_"+method, status).Inc()

	return err
}
