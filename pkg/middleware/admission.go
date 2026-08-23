package middleware

import (
	"time"

	"gochat/pkg/metrics"
	"gochat/tools"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// AdmissionOptions configures load shedding.
type AdmissionOptions struct {
	// MaxInFlight is the number of requests allowed to be in the handler chain
	// at once. Past this, the service is doing more work than it finishes, and
	// the excess only lengthens the queue.
	MaxInFlight int
	// AcquireTimeout is how long a request waits for a slot before it is shed.
	// It is the overload signal: while the service keeps up, slots free quickly
	// and nothing waits; once it falls behind, the wait grows past this and the
	// excess is refused instead of queued.
	AcquireTimeout time.Duration
}

// DefaultAdmissionOptions are deliberately conservative. MaxInFlight has to be
// calibrated against a real load test — see docs/benchmarks.md — because the
// right value depends on the request mix and the hardware, not on anything that
// can be reasoned out in advance.
var DefaultAdmissionOptions = AdmissionOptions{
	MaxInFlight:    512,
	AcquireTimeout: 50 * time.Millisecond,
}

// Admission limits in-flight requests and sheds the excess.
//
// The service has no queue of its own to measure: requests wait in the Go
// runtime and in the kernel's accept backlog, where a handler cannot see them.
// This creates a queue that can be seen. A slot is taken on the way in and
// returned on the way out, and a request that cannot get one within
// AcquireTimeout is refused with 429 before it touches any business logic.
//
// This does not raise throughput. Past saturation the service was already
// finishing less work than it accepted; the difference is that the excess is now
// told so immediately, instead of waiting behind a queue that outgrows the
// client's patience. Refusing in milliseconds keeps the requests that were
// admitted being served, which is what stops a system that is merely overloaded
// from becoming one that has stopped working.
func Admission(serviceName string, opts AdmissionOptions) gin.HandlerFunc {
	if opts.MaxInFlight <= 0 {
		opts.MaxInFlight = DefaultAdmissionOptions.MaxInFlight
	}
	if opts.AcquireTimeout <= 0 {
		opts.AcquireTimeout = DefaultAdmissionOptions.AcquireTimeout
	}
	logrus.Infof("admission control enabled: maxInFlight=%d acquireTimeout=%s",
		opts.MaxInFlight, opts.AcquireTimeout)

	// Create both series now, at zero, rather than on the first request.
	//
	// A GaugeVec with no observed label values exports nothing at all, and "no
	// series" is not the same as "zero" to anything reading the metric: an idle
	// api pod would be absent from custom.metrics.k8s.io entirely, and its
	// HorizontalPodAutoscaler would read <unknown> until the first request
	// happened to arrive. An autoscaler that only works once there is load is of
	// no use for deciding whether there is load.
	metrics.AdmissionInFlight.WithLabelValues(serviceName).Set(0)
	metrics.AdmissionShedTotal.WithLabelValues(serviceName).Add(0)

	slots := make(chan struct{}, opts.MaxInFlight)

	return func(c *gin.Context) {
		timer := time.NewTimer(opts.AcquireTimeout)
		defer timer.Stop()

		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			metrics.AdmissionInFlight.WithLabelValues(serviceName).Inc()
			defer metrics.AdmissionInFlight.WithLabelValues(serviceName).Dec()
			c.Next()

		case <-timer.C:
			metrics.AdmissionShedTotal.WithLabelValues(serviceName).Inc()
			c.Abort()
			tools.ResponseWithCode(c, tools.CodeOverloaded, nil, nil)

		case <-c.Request.Context().Done():
			// The client gave up while waiting. Nothing to serve, and nothing
			// gained by admitting it.
			metrics.AdmissionShedTotal.WithLabelValues(serviceName).Inc()
			c.Abort()
			tools.ResponseWithCode(c, tools.CodeOverloaded, nil, nil)
		}
	}
}
