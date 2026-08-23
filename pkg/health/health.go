// Package health holds the readiness checks a service registers for itself.
//
// Readiness and liveness answer different questions. Liveness asks whether the
// process is wedged and should be restarted; readiness asks whether it can serve
// right now. Conflating them turns a dependency outage into a restart loop across
// every replica, so only readiness consults this registry - the liveness endpoint
// stays deliberately unconditional. See docs/adr/0011.
package health

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// Check reports whether one dependency of a service is usable. It must return
// promptly and must not block indefinitely: every registered check runs on every
// readiness probe.
type Check func(context.Context) error

type entry struct {
	name  string
	check Check
}

var (
	mu      sync.RWMutex
	entries []entry
)

// Register adds a check under a name that will appear in the readiness response
// when it fails. Registering the same name twice replaces the earlier check,
// which keeps a restarted subsystem from accumulating duplicates.
func Register(name string, check Check) {
	mu.Lock()
	defer mu.Unlock()
	for i := range entries {
		if entries[i].name == name {
			entries[i].check = check
			return
		}
	}
	entries = append(entries, entry{name: name, check: check})
}

// RegisterGate registers a check that fails until the returned function has been
// called `required` times, and passes from then on.
//
// This is for facts the process already knows but currently throws away - that
// its RPC server finished registering in etcd, that its buckets are built. Those
// happen inside goroutines that outlive the call which started them, so a gate is
// the honest way to report them: not ready until something says so.
//
// `required` is greater than one where a role advertises several RPC addresses:
// registering one of them is not the same as being reachable, so the gate stays
// shut until every one has reported.
func RegisterGate(name string, required int) (done func()) {
	if required < 1 {
		required = 1
	}
	var count atomic.Int64
	Register(name, func(context.Context) error {
		if got := count.Load(); got < int64(required) {
			return fmt.Errorf("%d of %d complete", got, required)
		}
		return nil
	})
	return func() { count.Add(1) }
}

// Run executes every registered check and reports whether all of them passed,
// along with the error text of those that did not, keyed by name.
//
// Checks run concurrently: they are independent, and running them in sequence
// would let the slowest one set the floor for the probe's response time.
//
// Run returns when every check has finished OR when ctx expires, whichever comes
// first. The second half matters: a check is ordinary code that talks to the
// network, and one that ignores its context and blocks would otherwise hang the
// readiness endpoint for ever and leak a goroutine per probe - turning the one
// endpoint that is supposed to report trouble into trouble of its own. A check
// that has not finished by the deadline is reported as a failure, which is the
// honest answer: the service could not establish that it is ready.
//
// The goroutine of a check that is still blocked is left running. There is no
// way to interrupt it, and it will end when whatever it is waiting on gives up.
func Run(ctx context.Context) (bool, map[string]string) {
	mu.RLock()
	current := make([]entry, len(entries))
	copy(current, entries)
	mu.RUnlock()

	if len(current) == 0 {
		return true, nil
	}

	type result struct {
		name string
		err  error
	}
	results := make(chan result, len(current))

	for _, e := range current {
		go func(e entry) {
			// A check that panics is a failed check, not a dead process. The
			// readiness endpoint is the last thing that should take a service
			// down.
			defer func() {
				if r := recover(); r != nil {
					results <- result{e.name, errors.New("check panicked")}
				}
			}()
			results <- result{e.name, e.check(ctx)}
		}(e)
	}

	failures := make(map[string]string)
	// Tracked separately from `failures`, because a check that passed is absent
	// from failures but has very much reported.
	reported := make(map[string]bool, len(current))

	record := func(r result) {
		reported[r.name] = true
		if r.err != nil {
			failures[r.name] = r.err.Error()
		}
	}

	for len(reported) < len(current) {
		select {
		case r := <-results:
			record(r)
		case <-ctx.Done():
			// Drain anything that landed in the same instant, so a check that
			// finished microseconds before the deadline is not misreported.
			for drained := true; drained; {
				select {
				case r := <-results:
					record(r)
				default:
					drained = false
				}
			}
			// Name the ones still outstanding rather than reporting a bare
			// "timeout": which check is stuck is the whole diagnostic value.
			for _, e := range current {
				if !reported[e.name] {
					failures[e.name] = "timed out"
				}
			}
			return false, failures
		}
	}

	if len(failures) == 0 {
		return true, nil
	}
	return false, failures
}

// Names lists the registered check names, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.name)
	}
	sort.Strings(names)
	return names
}

// reset clears the registry. Test-only.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
}
