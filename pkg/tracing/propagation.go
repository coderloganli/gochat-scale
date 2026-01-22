package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
)

// MetadataCarrier implements propagation.TextMapCarrier for RPC metadata.
// It allows trace context to be injected into and extracted from RPC calls.
type MetadataCarrier map[string]string

// Get returns the value for the given key.
func (m MetadataCarrier) Get(key string) string {
	return m[key]
}

// Set stores the key-value pair.
func (m MetadataCarrier) Set(key, value string) {
	m[key] = value
}

// Keys returns all keys in the carrier.
func (m MetadataCarrier) Keys() []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// InjectContext injects the trace context from ctx into the carrier.
// Use this on the client side before making an RPC call.
func InjectContext(ctx context.Context, carrier MetadataCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractContext extracts the trace context from the carrier into a new context.
// Use this on the server side when receiving an RPC call.
func ExtractContext(ctx context.Context, carrier MetadataCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// NewMetadataCarrier creates a new empty metadata carrier.
func NewMetadataCarrier() MetadataCarrier {
	return make(MetadataCarrier)
}

// HeaderCarrier wraps http.Header for trace propagation in HTTP requests.
type HeaderCarrier struct {
	headers map[string][]string
}

// NewHeaderCarrier creates a new HeaderCarrier.
func NewHeaderCarrier(headers map[string][]string) *HeaderCarrier {
	return &HeaderCarrier{headers: headers}
}

// Get returns the value for the given key.
func (h *HeaderCarrier) Get(key string) string {
	vals := h.headers[key]
	if len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// Set stores the key-value pair.
func (h *HeaderCarrier) Set(key, value string) {
	h.headers[key] = []string{value}
}

// Keys returns all keys in the carrier.
func (h *HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(h.headers))
	for k := range h.headers {
		keys = append(keys, k)
	}
	return keys
}

// TraceContextKey is the context key for trace metadata in RPC calls.
type TraceContextKey struct{}

// ContextWithTraceMetadata adds trace metadata to context for RPC calls.
func ContextWithTraceMetadata(ctx context.Context) context.Context {
	carrier := NewMetadataCarrier()
	InjectContext(ctx, carrier)
	return context.WithValue(ctx, TraceContextKey{}, carrier)
}

// TraceMetadataFromContext retrieves trace metadata from context.
func TraceMetadataFromContext(ctx context.Context) MetadataCarrier {
	if carrier, ok := ctx.Value(TraceContextKey{}).(MetadataCarrier); ok {
		return carrier
	}
	return nil
}
