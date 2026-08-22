package connect

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// registryPollInterval is how often WaitEmpty re-checks. Shutdown is a rare
// event measured in seconds, so a poll is cheaper to be sure of than a
// condition variable that has to be signalled from every exit path a read loop
// has.
const registryPollInterval = 20 * time.Millisecond

// Registry holds every accepted connection, from the moment its Channel is
// created until its read loop exits.
//
// The buckets cannot serve this purpose: Bucket.Put runs in readPump only after
// authentication succeeds, so a connection that has been accepted but has not
// authenticated belongs to no bucket. Draining from the buckets would skip
// exactly the connections most likely to be present during a restart, when
// clients are reconnecting. See
// docs/adr/0010-a-departing-connect-instance-deregisters-before-it-closes-connections.md.
//
// It is sharded so that accept and disconnect do not contend on one lock at ten
// thousand connections.
type Registry struct {
	shards []*registryShard
	live   int64
}

type registryShard struct {
	mu  sync.Mutex
	chs map[*Channel]struct{}
}

// NewRegistry returns a Registry with the given number of shards.
func NewRegistry(shards int) *Registry {
	if shards < 1 {
		shards = 1
	}
	r := &Registry{shards: make([]*registryShard, shards)}
	for i := range r.shards {
		r.shards[i] = &registryShard{chs: make(map[*Channel]struct{})}
	}
	return r
}

// shardFor returns the shard a channel belongs to. The shard comes from the
// channel itself, fixed when it was created, so that Add and Remove always
// agree without either of them writing to the channel.
func (r *Registry) shardFor(ch *Channel) *registryShard {
	return r.shards[ch.shard%uint32(len(r.shards))]
}

// Add records a connection as live.
func (r *Registry) Add(ch *Channel) {
	s := r.shardFor(ch)
	s.mu.Lock()
	if _, ok := s.chs[ch]; !ok {
		s.chs[ch] = struct{}{}
		atomic.AddInt64(&r.live, 1)
	}
	s.mu.Unlock()
}

// Remove forgets a connection. It is safe to call for a channel that is not
// registered, because a read loop can exit more than one way.
func (r *Registry) Remove(ch *Channel) {
	s := r.shardFor(ch)
	s.mu.Lock()
	if _, ok := s.chs[ch]; ok {
		delete(s.chs, ch)
		atomic.AddInt64(&r.live, -1)
	}
	s.mu.Unlock()
}

// Len reports how many connections are live.
func (r *Registry) Len() int {
	return int(atomic.LoadInt64(&r.live))
}

// Snapshot returns the live connections at one moment. Shutdown walks it to
// write close frames; it is a copy, so a connection may exit while it is walked.
func (r *Registry) Snapshot() []*Channel {
	out := make([]*Channel, 0, r.Len())
	for _, s := range r.shards {
		s.mu.Lock()
		for ch := range s.chs {
			out = append(out, ch)
		}
		s.mu.Unlock()
	}
	return out
}

// WaitEmpty blocks until no connections are left, or until the context is done.
// It returns the context error if connections remain at the deadline.
func (r *Registry) WaitEmpty(ctx context.Context) error {
	ticker := time.NewTicker(registryPollInterval)
	defer ticker.Stop()

	for {
		if r.Len() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			if r.Len() == 0 {
				return nil
			}
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// SetDraining marks the server as shutting down. While draining, serveWs
// refuses upgrades with 503 and acceptTcp closes what it accepts, so that
// closing connections can converge instead of racing new arrivals.
func (s *Server) SetDraining() {
	atomic.StoreInt32(&s.draining, 1)
}

// Draining reports whether the server has begun shutting down.
func (s *Server) Draining() bool {
	return atomic.LoadInt32(&s.draining) == 1
}
