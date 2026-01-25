package middleware

import (
	"context"
	"runtime"
	"sync"
	"time"
	"unicode/utf8"

	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/smallnest/rpcx/share"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// PrometheusRPCPlugin implements RPCX plugin interface for metrics and tracing
type PrometheusRPCPlugin struct {
	serviceName string
	timings     sync.Map // map[uint64]time.Time - goroutine ID -> start time
	spans       sync.Map // map[uint64]trace.Span - goroutine ID -> span
}

type (
	rpcxStartKey  struct{}
	rpcxSpanKey   struct{}
	rpcxMethodKey struct{}
)

// sanitizeMethodName ensures the method name is valid UTF-8
func sanitizeMethodName(name string) string {
	if utf8.ValidString(name) {
		return name
	}
	return "unknown"
}

func NewPrometheusRPCPlugin(serviceName string) *PrometheusRPCPlugin {
	return &PrometheusRPCPlugin{
		serviceName: serviceName,
	}
}

// goroutine ID buffer pool for performance
var gidBufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 64)
	},
}

// getGoroutineID returns the current goroutine ID
// Optimized with buffer pool to reduce allocations
func getGoroutineID() uint64 {
	b := gidBufPool.Get().([]byte)
	defer gidBufPool.Put(b)

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
	// Sanitize method name to ensure valid UTF-8 for Prometheus labels
	safeMethodName := sanitizeMethodName(methodName)

	metrics.RPCServerRequestsInFlight.WithLabelValues(p.serviceName).Inc()

	// Extract trace context from incoming request
	if carrier := tracing.TraceMetadataFromContext(ctx); carrier != nil {
		ctx = tracing.ExtractContext(ctx, carrier)
	}

	// Start a new span for this RPC call
	_, span := tracing.StartSpan(ctx, "rpc.server/"+safeMethodName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("rpc.system", "rpcx"),
			attribute.String("rpc.service", p.serviceName),
			attribute.String("rpc.method", safeMethodName),
		),
	)

	// Store start time, span, and method name in context
	if sc, ok := ctx.(*share.Context); ok {
		sc.SetValue(rpcxStartKey{}, time.Now())
		sc.SetValue(rpcxSpanKey{}, span)
		sc.SetValue(rpcxMethodKey{}, safeMethodName)
	} else {
		gid := getGoroutineID()
		p.timings.Store(gid, time.Now())
		p.spans.Store(gid, span)
	}

	// Return the original args unchanged
	return args, nil
}

// PostCall implements the rpcx PostCallPlugin interface.
// Note: rpcx does not pass error info to PostCall, so we always record status as "success".
// Error tracking should be done via tracing or separate error metrics.
func (p *PrometheusRPCPlugin) PostCall(ctx context.Context, serviceName, methodName string, args, reply interface{}) (interface{}, error) {
	metrics.RPCServerRequestsInFlight.WithLabelValues(p.serviceName).Dec()

	var duration float64
	var span trace.Span
	// Use sanitized method name from PreCall context, fallback to sanitizing current methodName
	safeMethodName := sanitizeMethodName(methodName)

	if sc, ok := ctx.(*share.Context); ok {
		// Get method name stored in PreCall
		if storedMethod := sc.Value(rpcxMethodKey{}); storedMethod != nil {
			if m, ok := storedMethod.(string); ok {
				safeMethodName = m
			}
		}
		if startTime := sc.Value(rpcxStartKey{}); startTime != nil {
			if start, ok := startTime.(time.Time); ok {
				duration = time.Since(start).Seconds()
				metrics.RPCServerDuration.WithLabelValues(p.serviceName, safeMethodName).Observe(duration)
			}
		}
		if spanVal := sc.Value(rpcxSpanKey{}); spanVal != nil {
			if s, ok := spanVal.(trace.Span); ok {
				span = s
			}
		}
		sc.DeleteKey(rpcxStartKey{})
		sc.DeleteKey(rpcxSpanKey{})
		sc.DeleteKey(rpcxMethodKey{})
	} else {
		gid := getGoroutineID()
		// Retrieve and delete start time
		if startTime, ok := p.timings.LoadAndDelete(gid); ok {
			if start, ok := startTime.(time.Time); ok {
				duration = time.Since(start).Seconds()
				metrics.RPCServerDuration.WithLabelValues(p.serviceName, safeMethodName).Observe(duration)
			}
		}
		if spanVal, ok := p.spans.LoadAndDelete(gid); ok {
			if s, ok := spanVal.(trace.Span); ok {
				span = s
			}
		}
	}

	// Note: rpcx PostCallPlugin doesn't receive error info, so we record all as success
	// For error tracking, use the tracing span or implement a separate error wrapper
	metrics.RPCServerRequestsTotal.WithLabelValues(p.serviceName, safeMethodName, "success").Inc()

	// End the span
	if span != nil {
		span.SetAttributes(attribute.Float64("rpc.duration_seconds", duration))
		span.SetStatus(codes.Ok, "")
		span.End()
	}

	return reply, nil
}
