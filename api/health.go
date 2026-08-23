package api

import (
	"context"
	"errors"

	"gochat/api/rpc"
	"gochat/pkg/health"
)

// Readiness for the api role.
//
// api terminates client HTTP and does no work of its own: every request it
// serves becomes a logic RPC. So the one thing it must be able to do before it
// is worth sending traffic to is resolve a logic instance. An api pod that is
// listening but cannot reach logic answers every request with an upstream
// failure, which readiness exists to prevent. See docs/adr/0011.

func registerHealthChecks() {
	health.Register("logic-rpc", func(ctx context.Context) error {
		if rpc.LogicDiscovery == nil {
			return errors.New("logic discovery not initialised")
		}
		if len(rpc.LogicDiscovery.GetServices()) == 0 {
			return errors.New("no logic instance registered in etcd")
		}
		return nil
	})
}
