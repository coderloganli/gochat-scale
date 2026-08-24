package connect

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	dto "github.com/prometheus/client_model/go"

	"gochat/pkg/metrics"
)

const shutdownTestService = "connect-ws"

// drainFixture stands up serveWs behind an httptest server and opens n client
// connections against it. The clients never authenticate, which is deliberate:
// an unauthenticated connection belongs to no bucket, and those are the ones a
// drain must not miss.
type drainFixture struct {
	connect *Connect
	server  *Server
	ts      *httptest.Server
	clients []*websocket.Conn
}

func newDrainFixture(t *testing.T, n int) *drainFixture {
	t.Helper()

	c := New()
	s := testServer(t)
	c.server = s

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.serveWs(s, w, r)
	}))

	f := &drainFixture{connect: c, server: s, ts: ts}
	for i := 0; i < n; i++ {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL), nil)
		if err != nil {
			t.Fatalf("dial %d: %v", i, err)
		}
		f.clients = append(f.clients, conn)
	}

	if !eventually(3*time.Second, func() bool { return s.Registry.Len() == n }) {
		t.Fatalf("registry holds %d connections, want %d", s.Registry.Len(), n)
	}
	return f
}

func (f *drainFixture) close() {
	for _, c := range f.clients {
		c.Close()
	}
	f.ts.Close()
}

// answerCloseFrames makes each client read, which is what drives gorilla's
// default close handler into replying to a close frame.
func (f *drainFixture) answerCloseFrames() {
	for _, c := range f.clients {
		go func(c *websocket.Conn) {
			for {
				if _, _, err := c.ReadMessage(); err != nil {
					return
				}
			}
		}(c)
	}
}

func counter(t *testing.T, reason string) float64 {
	t.Helper()
	m := &dto.Metric{}
	c, err := metrics.ShutdownConnectionsClosedTotal.GetMetricWithLabelValues(shutdownTestService, reason)
	if err != nil {
		t.Fatalf("shutdown counter %q: %v", reason, err)
	}
	if err := c.Write(m); err != nil {
		t.Fatalf("read shutdown counter %q: %v", reason, err)
	}
	return m.GetCounter().GetValue()
}

func histogramCount(t *testing.T) uint64 {
	t.Helper()
	o, err := metrics.ShutdownDurationSeconds.GetMetricWithLabelValues(shutdownTestService)
	if err != nil {
		t.Fatalf("shutdown duration metric: %v", err)
	}
	m := &dto.Metric{}
	if err := o.(interface{ Write(*dto.Metric) error }).Write(m); err != nil {
		t.Fatalf("read shutdown duration metric: %v", err)
	}
	return m.GetHistogram().GetSampleCount()
}

// Test case 23: after a shutdown in which every connection answered the close
// frame, the close_frame counter equals the number of connections and forced is
// zero.
func TestShutdownCountsConnectionsThatAnswered(t *testing.T) {
	const n = 3

	f := newDrainFixture(t, n)
	defer f.close()
	f.answerCloseFrames()

	beforeAnswered := counter(t, "close_frame")
	beforeForced := counter(t, "forced")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.connect.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := counter(t, "close_frame") - beforeAnswered; got != n {
		t.Errorf("close_frame counter rose by %v, want %d", got, n)
	}
	if got := counter(t, "forced") - beforeForced; got != 0 {
		t.Errorf("forced counter rose by %v, want 0 — every client answered", got)
	}
}

// Test case 24: after a shutdown in which no connection answered, the same
// counters are reversed.
func TestShutdownCountsConnectionsItHadToForce(t *testing.T) {
	const n = 3

	// No answerCloseFrames: these clients never read, so they never process the
	// close control frame and never reply.
	f := newDrainFixture(t, n)
	defer f.close()

	beforeAnswered := counter(t, "close_frame")
	beforeForced := counter(t, "forced")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.connect.Stop(ctx); err != nil && err != context.DeadlineExceeded {
		t.Fatalf("Stop: %v", err)
	}

	if got := counter(t, "forced") - beforeForced; got != n {
		t.Errorf("forced counter rose by %v, want %d — a shutdown that hits its cap "+
			"must say so rather than looking clean", got, n)
	}
	if got := counter(t, "close_frame") - beforeAnswered; got != 0 {
		t.Errorf("close_frame counter rose by %v, want 0 — nobody answered", got)
	}
}

// Test case 25: gochat_shutdown_duration_seconds is observed exactly once per
// shutdown.
func TestShutdownRecordsItsDurationOnce(t *testing.T) {
	f := newDrainFixture(t, 1)
	defer f.close()
	f.answerCloseFrames()

	before := histogramCount(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := f.connect.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if got := histogramCount(t) - before; got != 1 {
		t.Fatalf("shutdown duration observed %d times, want exactly 1", got)
	}
}
