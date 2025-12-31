package middleware

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gochat/pkg/metrics"
)

// PrometheusMiddleware creates a Gin middleware for Prometheus metrics
func PrometheusMiddleware(serviceName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}

		// Track in-flight requests
		metrics.HTTPRequestsInFlight.WithLabelValues(serviceName).Inc()
		defer metrics.HTTPRequestsInFlight.WithLabelValues(serviceName).Dec()

		// Process request
		c.Next()

		// Record metrics
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())
		method := c.Request.Method

		metrics.HTTPRequestsTotal.WithLabelValues(serviceName, method, path, status).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(serviceName, method, path).Observe(duration)
	}
}
