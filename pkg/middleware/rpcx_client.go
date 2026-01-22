package middleware

import (
	"context"
	"time"

	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/smallnest/rpcx/client"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentedCall wraps an RPC call and records client-side metrics and tracing.
// It tracks request count, duration, error rate, and creates trace spans
// for distributed tracing across services.
func InstrumentedCall(
	ctx context.Context,
	xc client.XClient,
	sourceService, targetService, method string,
	args, reply interface{},
) error {
	// Start a new span for this RPC call
	ctx, span := tracing.StartSpan(ctx, "rpc.client/"+method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("rpc.system", "rpcx"),
			attribute.String("rpc.service", targetService),
			attribute.String("rpc.method", method),
			attribute.String("source.service", sourceService),
			attribute.String("target.service", targetService),
		),
	)
	defer span.End()

	start := time.Now()

	// Inject trace context into the RPC call context
	ctx = tracing.ContextWithTraceMetadata(ctx)

	err := xc.Call(ctx, method, args, reply)

	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	// Record metrics
	metrics.RPCClientDuration.WithLabelValues(sourceService, targetService, method).Observe(duration)
	metrics.RPCClientRequestsTotal.WithLabelValues(sourceService, method, status).Inc()

	// Add duration to span
	span.SetAttributes(attribute.Float64("rpc.duration_seconds", duration))

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
	// Start a new span for this RPC call
	ctx, span := tracing.StartSpan(ctx, "rpc.client/"+method,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("rpc.system", "rpcx"),
			attribute.String("rpc.service", targetService),
			attribute.String("rpc.method", method),
			attribute.String("source.service", sourceService),
			attribute.String("target.service", targetService),
		),
	)
	defer span.End()

	start := time.Now()

	// Inject trace context into the RPC call context
	ctx = tracing.ContextWithTraceMetadata(ctx)

	err := xc.Call(ctx, method, args, reply)

	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	// Record metrics
	metrics.RPCClientDuration.WithLabelValues(sourceService, targetService, method).Observe(duration)
	// Use target_service as the "method" label for granular tracking
	metrics.RPCClientRequestsTotal.WithLabelValues(sourceService, targetService+"_"+method, status).Inc()

	// Add duration to span
	span.SetAttributes(attribute.Float64("rpc.duration_seconds", duration))

	return err
}
