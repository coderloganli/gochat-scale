package middleware

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"gochat/config"
	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/sirupsen/logrus"
	"github.com/smallnest/rpcx/client"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// DefaultCallTimeout bounds an outbound RPC when nothing else does.
const DefaultCallTimeout = 2 * time.Second

var (
	callTimeoutOnce sync.Once
	callTimeout     time.Duration
)

// CallTimeout resolves the per-call deadline: RPC_TIMEOUT if set, otherwise
// [common-rpc] timeout from the config, otherwise DefaultCallTimeout.
func CallTimeout() time.Duration {
	callTimeoutOnce.Do(func() {
		callTimeout = DefaultCallTimeout
		raw := os.Getenv("RPC_TIMEOUT")
		if raw == "" && config.Conf != nil {
			raw = config.Conf.Common.CommonRPC.Timeout
		}
		if raw == "" {
			return
		}
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			logrus.Warnf("invalid rpc timeout %q, using %s", raw, DefaultCallTimeout)
			return
		}
		callTimeout = parsed
	})
	return callTimeout
}

// withCallDeadline bounds a call. rpcx exposes no per-call timeout option, so
// the deadline has to travel on the context; rpcx then forwards the remaining
// time to the server as request metadata, and the server derives a cancellable
// context from it. Without this, a slow callee blocks the caller's goroutine
// indefinitely, which is how an overloaded system stops recovering.
//
// An existing deadline is never relaxed, only tightened.
func withCallDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := CallTimeout()
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

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

	ctx, cancel := withCallDeadline(ctx)
	defer cancel()

	err := xc.Call(ctx, method, args, reply)

	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
		if errors.Is(err, context.DeadlineExceeded) {
			status = "timeout"
		}
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

	ctx, cancel := withCallDeadline(ctx)
	defer cancel()

	err := xc.Call(ctx, method, args, reply)

	duration := time.Since(start).Seconds()
	status := "success"
	if err != nil {
		status = "error"
		if errors.Is(err, context.DeadlineExceeded) {
			status = "timeout"
		}
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
