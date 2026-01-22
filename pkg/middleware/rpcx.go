package middleware

import (
	"context"
	"runtime"
	"sync"
	"time"

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
	rpcxStartKey struct{}
	rpcxSpanKey  struct{}
)

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

	// Extract trace context from incoming request
	if carrier := tracing.TraceMetadataFromContext(ctx); carrier != nil {
		ctx = tracing.ExtractContext(ctx, carrier)
	}

	// Start a new span for this RPC call
	_, span := tracing.StartSpan(ctx, "rpc.server/"+methodName,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("rpc.system", "rpcx"),
			attribute.String("rpc.service", p.serviceName),
			attribute.String("rpc.method", methodName),
		),
	)

	// Store start time and span keyed by goroutine ID
	if sc, ok := ctx.(*share.Context); ok {
		sc.SetValue(rpcxStartKey{}, time.Now())
		sc.SetValue(rpcxSpanKey{}, span)
	} else {
		gid := getGoroutineID()
		p.timings.Store(gid, time.Now())
		p.spans.Store(gid, span)
	}

	// Return the original args unchanged
	return args, nil
}

func (p *PrometheusRPCPlugin) PostCall(ctx context.Context, serviceName, methodName string, args, reply interface{}, err error) error {
	metrics.RPCServerRequestsInFlight.WithLabelValues(p.serviceName).Dec()

	var duration float64
	var span trace.Span
	if sc, ok := ctx.(*share.Context); ok {
		if startTime := sc.Value(rpcxStartKey{}); startTime != nil {
			if start, ok := startTime.(time.Time); ok {
				duration = time.Since(start).Seconds()
				metrics.RPCServerDuration.WithLabelValues(p.serviceName, methodName).Observe(duration)
			}
		}
		if spanVal := sc.Value(rpcxSpanKey{}); spanVal != nil {
			if s, ok := spanVal.(trace.Span); ok {
				span = s
			}
		}
		sc.DeleteKey(rpcxStartKey{})
		sc.DeleteKey(rpcxSpanKey{})
	} else {
		gid := getGoroutineID()
		// Retrieve and delete start time
		if startTime, ok := p.timings.LoadAndDelete(gid); ok {
			if start, ok := startTime.(time.Time); ok {
				duration = time.Since(start).Seconds()
				metrics.RPCServerDuration.WithLabelValues(p.serviceName, methodName).Observe(duration)
			}
		}
		if spanVal, ok := p.spans.LoadAndDelete(gid); ok {
			if s, ok := spanVal.(trace.Span); ok {
				span = s
			}
		}
	}

	status := "success"
	if err != nil {
		status = "error"
	}
	metrics.RPCServerRequestsTotal.WithLabelValues(p.serviceName, methodName, status).Inc()

	// End the span
	if span != nil {
		span.SetAttributes(attribute.Float64("rpc.duration_seconds", duration))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()
	}

	return nil
}
