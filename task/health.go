package task

import (
	"context"
	"errors"
	"fmt"

	"gochat/pkg/health"
)

// Readiness for the task role.
//
// task consumes from RabbitMQ and delivers each message to the connect instance
// holding the recipient. Both halves have to work: a task with no RabbitMQ
// channel consumes nothing, and a task that knows of no connect instance
// consumes messages and drops them. The second is the quieter failure and the
// reason the instance map is now seeded at startup rather than waiting for the
// first watch event. See docs/adr/0011.

func registerHealthChecks() {
	health.Register("rabbitmq", func(ctx context.Context) error {
		if RabbitMQClient == nil {
			return errors.New("rabbitmq client not initialised")
		}
		// Both, and both by IsClosed rather than by nil. The client caches its
		// channel, so the pointer stays non-nil after the broker goes away -
		// checking only for nil reports a service as ready when it cannot
		// publish a single message, which is the exact half-working state
		// readiness is here to hide.
		conn := RabbitMQClient.Connection()
		if conn == nil || conn.IsClosed() {
			return errors.New("rabbitmq connection is closed")
		}
		ch := RabbitMQClient.Channel()
		if ch == nil || ch.IsClosed() {
			return errors.New("rabbitmq channel is closed")
		}
		return nil
	})

	// "Connected to RabbitMQ" is not the same as "consuming from RabbitMQ", and
	// the difference is not academic: a consumer's delivery channel closes on
	// every broker restart, and until this was fixed the goroutine behind it
	// exited for good. The connection reconnected underneath, so the pod stayed
	// Ready and quietly delivered nothing for the rest of its life.
	health.Register("consumers", func(ctx context.Context) error {
		want, got := wantedConsumers.Load(), consumersRunning.Load()
		if want == 0 {
			return errors.New("consumers not started")
		}
		if got < want {
			return fmt.Errorf("%d of %d consumers attached", got, want)
		}
		return nil
	})

	health.Register("connect-discovered", func(ctx context.Context) error {
		RClient.lock.RLock()
		n := len(RClient.ServerInsMap)
		RClient.lock.RUnlock()
		if n == 0 {
			return errors.New("no connect instance discovered")
		}
		return nil
	})
}
