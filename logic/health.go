package logic

import (
	"context"
	"errors"

	"gochat/db"
	"gochat/pkg/health"
)

// Readiness for the logic role.
//
// logic is the only service that touches the database, and it is the one that
// publishes every outbound message to RabbitMQ. It can authenticate a user with
// PostgreSQL and Redis alone, but a replica that cannot publish is half working:
// logins succeed and nothing is ever delivered. Readiness covers all four
// dependencies for that reason. See docs/adr/0011.

var markEtcdRegistered func()

func registerHealthChecks(rpcAddresses int) {
	markEtcdRegistered = health.RegisterGate("etcd-registered", rpcAddresses)

	health.Register("db", func(ctx context.Context) error {
		conn := db.GetDb(db.DefaultDbName)
		if conn == nil {
			return errors.New("database pool not initialised")
		}
		sqlDB := conn.DB()
		if sqlDB == nil {
			return errors.New("database pool has no underlying connection")
		}
		return sqlDB.PingContext(ctx)
	})

	health.Register("redis", func(ctx context.Context) error {
		if RedisClient == nil {
			return errors.New("redis client not initialised")
		}
		return RedisClient.Ping().Err()
	})

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
}

// etcdRegistered is called from inside each RPC server goroutine once that
// address has actually been registered, which is the only point at which the
// process is reachable by the services that discover it.
func etcdRegistered() {
	if markEtcdRegistered != nil {
		markEtcdRegistered()
	}
}
