package tracing

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	tracer         trace.Tracer
	tracerProvider *sdktrace.TracerProvider
)

// InitTracer initializes the OpenTelemetry tracer with OTLP HTTP exporter.
// It returns a shutdown function that should be called when the application exits.
func InitTracer(serviceName string, cfg Config) (func(context.Context) error, error) {
	if !cfg.Enabled {
		logrus.Info("Tracing is disabled")
		// Return a no-op tracer
		tracer = otel.Tracer(serviceName)
		return func(ctx context.Context) error { return nil }, nil
	}

	ctx := context.Background()

	// Create OTLP HTTP exporter
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(cfg.Endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			attribute.String("environment", "dev"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create sampler based on sampling rate
	var sampler sdktrace.Sampler
	if cfg.SamplingRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SamplingRate <= 0 {
		sampler = sdktrace.NeverSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(cfg.SamplingRate)
	}

	// Create TracerProvider
	tracerProvider = sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global TracerProvider
	otel.SetTracerProvider(tracerProvider)

	// Set global propagator for context propagation across services
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer = tracerProvider.Tracer(serviceName)

	logrus.Infof("Tracing initialized for service %s, endpoint: %s, sampling rate: %.2f",
		serviceName, cfg.Endpoint, cfg.SamplingRate)

	return tracerProvider.Shutdown, nil
}

// GetTracer returns the global tracer instance.
func GetTracer() trace.Tracer {
	if tracer == nil {
		return otel.Tracer("gochat")
	}
	return tracer
}

// StartSpan starts a new span with the given name.
func StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return GetTracer().Start(ctx, name, opts...)
}

// SpanFromContext returns the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// AddSpanAttributes adds attributes to the current span.
func AddSpanAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// RecordError records an error on the current span.
func RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() && err != nil {
		span.RecordError(err)
	}
}
