package middleware

import (
	"gochat/pkg/tracing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware creates a Gin middleware that adds distributed tracing
// to HTTP requests. It extracts incoming trace context and creates spans
// for each request.
func TracingMiddleware(serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract trace context from incoming request headers
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))

		// Start a new span for this HTTP request
		ctx, span := tracing.StartSpan(ctx, c.Request.Method+" "+c.FullPath(),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", c.Request.Method),
				attribute.String("http.url", c.Request.URL.String()),
				attribute.String("http.path", c.FullPath()),
				attribute.String("http.host", c.Request.Host),
				attribute.String("http.user_agent", c.Request.UserAgent()),
				attribute.String("http.client_ip", c.ClientIP()),
				attribute.String("service.name", serviceName),
			),
		)
		defer span.End()

		// Update request context with span
		c.Request = c.Request.WithContext(ctx)

		// Process request
		c.Next()

		// Record response status
		statusCode := c.Writer.Status()
		span.SetAttributes(attribute.Int("http.status_code", statusCode))

		// Set span status based on HTTP status code
		if statusCode >= 400 && statusCode < 500 {
			span.SetStatus(codes.Error, "client error")
		} else if statusCode >= 500 {
			span.SetStatus(codes.Error, "server error")
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// Record any errors
		if len(c.Errors) > 0 {
			for _, err := range c.Errors {
				span.RecordError(err.Err)
			}
		}
	}
}
