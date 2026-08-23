package connect

import (
	"context"
	"errors"

	"gochat/pkg/health"

	"github.com/smallnest/rpcx/client"
)

// Readiness for the connect roles.
//
// connect holds no database and no Redis client of its own: everything it needs
// from either goes through a logic RPC. So what it has to be able to do before it
// can serve is reach logic, have its own buckets built, and - the one that is easy
// to miss - have registered itself in etcd.
//
// That last one matters more here than anywhere else. task resolves a recipient's
// serverId through the registry (docs/adr/0002) and calls that specific instance.
// A connect pod that is accepting WebSocket connections but has not yet registered
// is a black hole: it looks healthy from outside and silently loses every message
// addressed to the users it is holding. See docs/adr/0011.

var (
	logicDiscovery     client.ServiceDiscovery
	markEtcdRegistered func()
	markBucketsReady   func()
)

// registerHealthChecks declares what this connect process must be able to do
// before it is ready. rpcAddresses is how many RPC addresses it advertises: the
// etcd gate stays shut until all of them have registered, because being reachable
// on one of two addresses is not being reachable.
func registerHealthChecks(rpcAddresses int) {
	markEtcdRegistered = health.RegisterGate("etcd-registered", rpcAddresses)
	markBucketsReady = health.RegisterGate("buckets-initialised", 1)

	health.Register("logic-rpc", func(ctx context.Context) error {
		if logicDiscovery == nil {
			return errors.New("logic discovery not initialised")
		}
		if len(logicDiscovery.GetServices()) == 0 {
			return errors.New("no logic instance registered in etcd")
		}
		return nil
	})
}

// etcdRegistered is called once per advertised RPC address, from inside the
// goroutine that serves it, after registration has actually happened.
func etcdRegistered() {
	if markEtcdRegistered != nil {
		markEtcdRegistered()
	}
}

func bucketsReady() {
	if markBucketsReady != nil {
		markBucketsReady()
	}
}
