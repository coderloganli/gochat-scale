# GoChat Observability Guide

This document describes the observability stack for GoChat, covering metrics, distributed tracing, and profiling.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                      Jaeger UI (:16686)                     │
└─────────────────────────────────────────────────────────────┘
                              ▲
                              │ OTLP HTTP (:4318)
                              │
┌──────────┐    ┌──────────┐  │  ┌──────────┐    ┌──────────┐
│   API    │───▶│  Logic   │──┼─▶│   Task   │───▶│ Connect  │
│ (trace)  │    │ (trace)  │  │  │ (trace)  │    │ (trace)  │
└──────────┘    └──────────┘  │  └──────────┘    └──────────┘
     │               │        │       │               │
     └───────────────┴────────┴───────┴───────────────┘
                    Context Propagation
```

## Components

### 1. Metrics (Prometheus + Grafana)

- **Prometheus**: Scrapes metrics from all services at `/metrics` endpoints
- **Grafana**: Visualizes metrics with pre-configured dashboards

**Access Points:**
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000 (admin/admin)

**Service Metrics Ports:**
| Service | Metrics Port |
|---------|-------------|
| logic | 9091 |
| connect-ws | 9092 |
| connect-tcp | 9093 |
| task | 9094 |
| api | 7070 (via /metrics) |

### 2. Distributed Tracing (OpenTelemetry + Jaeger)

All services are instrumented with OpenTelemetry and export traces to Jaeger via OTLP HTTP.

**Access Point:**
- Jaeger UI: http://localhost:16686

**Services Traced:**
- `api` - HTTP API gateway
- `logic` - Business logic service
- `task` - Async message processor
- `connect-ws` - WebSocket connection handler
- `connect-tcp` - TCP connection handler

### 3. Profiling (pprof)

Go pprof is available for performance profiling on the connect services.

## Configuration

### Tracing Configuration

Tracing is configured in `config/{env}/common.toml`:

```toml
[common-tracing]
enabled = true
endpoint = "jaeger:4318"
samplingRate = 1.0  # 100% sampling for dev
```

**Configuration Options:**

| Option | Description | Default |
|--------|-------------|---------|
| enabled | Enable/disable tracing | true |
| endpoint | Jaeger OTLP HTTP endpoint | jaeger:4318 |
| samplingRate | Trace sampling rate (0.0-1.0) | 1.0 (dev), 0.1 (prod) |

### Environment-Specific Settings

| Environment | Sampling Rate | Description |
|-------------|---------------|-------------|
| dev | 1.0 (100%) | Capture all traces for debugging |
| staging | 0.5 (50%) | Moderate sampling for testing |
| prod | 0.1 (10%) | Reduced sampling for performance |

## Quick Start

### 1. Start All Services

```bash
make compose-dev
```

This starts all application services plus observability infrastructure:
- Prometheus
- Grafana
- Jaeger

### 2. Access Jaeger UI

Open http://localhost:16686 in your browser.

### 3. Generate Traces

Run a load test to generate traces:

```bash
make loadtest-smoke
```

Or manually trigger API calls:

```bash
# Register a user
curl -X POST http://localhost:7070/user/register \
  -H "Content-Type: application/json" \
  -d '{"userName":"testuser","passWord":"password123"}'

# Login
curl -X POST http://localhost:7070/user/login \
  -H "Content-Type: application/json" \
  -d '{"userName":"testuser","passWord":"password123"}'
```

### 4. View Traces

1. Go to Jaeger UI (http://localhost:16686)
2. Select a service from the dropdown (e.g., "api")
3. Click "Find Traces"
4. Click on a trace to see the full distributed trace across services

## Understanding Traces

### Trace Structure

A typical request trace shows the flow:

```
API (HTTP) → Logic (RPC) → Task (async) → Connect (RPC)
```

### Span Attributes

Each span includes useful attributes:

**HTTP Spans:**
- `http.method` - HTTP method (GET, POST, etc.)
- `http.url` - Request URL
- `http.status_code` - Response status code
- `http.client_ip` - Client IP address

**RPC Spans:**
- `rpc.system` - RPC framework (rpcx)
- `rpc.service` - Target service name
- `rpc.method` - Method being called
- `rpc.duration_seconds` - Call duration

## Troubleshooting

### No Traces Appearing

1. Check if Jaeger is running:
   ```bash
   docker ps | grep jaeger
   ```

2. Verify tracing is enabled in config:
   ```bash
   cat config/dev/common.toml | grep -A3 tracing
   ```

3. Check service logs for tracer initialization:
   ```bash
   docker compose logs api | grep -i trac
   ```

### Missing Service in Jaeger

1. Ensure the service has started successfully:
   ```bash
   docker compose ps
   ```

2. Check if spans are being created by looking at service logs

### Incomplete Traces

Context propagation may be interrupted. Check:
1. All RPC calls pass context properly
2. HTTP middleware is registered correctly

## Best Practices

### 1. Sampling in Production

Use lower sampling rates in production to reduce overhead:

```toml
[common-tracing]
samplingRate = 0.1  # 10% of traces
```

### 2. Adding Custom Spans

To add custom spans in your code:

```go
import "gochat/pkg/tracing"

func MyFunction(ctx context.Context) {
    ctx, span := tracing.StartSpan(ctx, "my-operation")
    defer span.End()

    // Add attributes
    tracing.AddSpanAttributes(ctx,
        attribute.String("custom.key", "value"),
    )

    // Record errors
    if err != nil {
        tracing.RecordError(ctx, err)
    }
}
```

### 3. Correlating with Logs

Include trace IDs in your logs for correlation:

```go
span := trace.SpanFromContext(ctx)
traceID := span.SpanContext().TraceID().String()
logrus.WithField("trace_id", traceID).Info("Processing request")
```

## Metrics Reference

### RPC Metrics

| Metric | Type | Description |
|--------|------|-------------|
| rpc_server_requests_total | Counter | Total RPC requests received |
| rpc_server_duration_seconds | Histogram | RPC request duration |
| rpc_server_requests_in_flight | Gauge | Current in-flight requests |
| rpc_client_requests_total | Counter | Total RPC requests sent |
| rpc_client_duration_seconds | Histogram | RPC client call duration |

### HTTP Metrics

| Metric | Type | Description |
|--------|------|-------------|
| http_requests_total | Counter | Total HTTP requests |
| http_request_duration_seconds | Histogram | HTTP request duration |
| http_requests_in_flight | Gauge | Current in-flight HTTP requests |

## Further Reading

- [OpenTelemetry Go Documentation](https://opentelemetry.io/docs/languages/go/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
