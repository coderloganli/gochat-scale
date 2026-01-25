package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP Metrics (shared by API and Site)
var (
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_http_requests_total",
			Help: "Total HTTP requests by service, method, path, status",
		},
		[]string{"service", "method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gochat_http_request_duration_seconds",
			Help:    "HTTP request latency distributions",
			Buckets: prometheus.DefBuckets, // 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
		},
		[]string{"service", "method", "path"},
	)

	HTTPRequestsInFlight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gochat_http_requests_in_flight",
			Help: "Current in-flight HTTP requests",
		},
		[]string{"service"},
	)
)

// RPC Server Metrics (shared by Logic and Connect)
var (
	RPCServerRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_rpc_server_requests_total",
			Help: "Total RPC server requests",
		},
		[]string{"service", "method", "status"},
	)

	RPCServerDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gochat_rpc_server_duration_seconds",
			Help:    "RPC server request duration",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service", "method"},
	)

	RPCServerRequestsInFlight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gochat_rpc_server_requests_in_flight",
			Help: "Current in-flight RPC server requests",
		},
		[]string{"service"},
	)
)

// RPC Client Metrics
var (
	RPCClientRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_rpc_client_requests_total",
			Help: "Total RPC client requests",
		},
		[]string{"service", "method", "status"},
	)

	RPCClientDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gochat_rpc_client_duration_seconds",
			Help:    "RPC client request duration",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service", "target_service", "method"},
	)
)

// WebSocket/TCP Connection Metrics
var (
	ConnectionsActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gochat_connections_active",
			Help: "Current active connections",
		},
		[]string{"service", "type"}, // type: websocket/tcp
	)

	ConnectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_connections_total",
			Help: "Total connection attempts",
		},
		[]string{"service", "type", "status"},
	)

	MessagesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_messages_total",
			Help: "Total messages sent/received",
		},
		[]string{"service", "direction"}, // direction: sent/received
	)
)

// Business Metrics
var (
	UserOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_user_operations_total",
			Help: "Total user operations",
		},
		[]string{"operation", "status"}, // operation: login/register/logout
	)

	RoomOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_room_operations_total",
			Help: "Total room operations",
		},
		[]string{"operation"}, // operation: push_single/push_room/count/room_info
	)

	QueueMessagesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_queue_messages_total",
			Help: "Total queue messages processed",
		},
		[]string{"operation", "status"},
	)
)

// Redis Metrics
var (
	RedisOperationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_redis_operations_total",
			Help: "Total Redis operations",
		},
		[]string{"service", "command", "status"},
	)

	RedisOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gochat_redis_operation_duration_seconds",
			Help:    "Redis operation latency distributions",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service", "command"},
	)
)

// Database Metrics
var (
	DBQueryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_db_query_total",
			Help: "Total database queries",
		},
		[]string{"service", "operation", "table", "status"},
	)

	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gochat_db_query_duration_seconds",
			Help:    "Database query latency distributions",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
		},
		[]string{"service", "operation", "table"},
	)
)

// Auth Cache Metrics
var (
	AuthCacheHits = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "gochat_auth_cache_hits_total",
			Help: "Total auth cache hits",
		},
	)

	AuthCacheMisses = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "gochat_auth_cache_misses_total",
			Help: "Total auth cache misses",
		},
	)

	AuthCacheSize = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "gochat_auth_cache_size",
			Help: "Current number of entries in auth cache",
		},
	)
)
