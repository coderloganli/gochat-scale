package connect

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	buckets := make([]*Bucket, 2)
	for i := range buckets {
		buckets[i] = NewBucket(BucketOptions{
			ChannelSize:   16,
			RoomSize:      4,
			RoutineAmount: 2,
			RoutineSize:   4,
		})
	}
	// PongWait and PingPeriod match what connect.Run configures. With a short
	// PongWait the server's own read deadline tears connections down mid-test,
	// which makes a connection that never answered look like one that did.
	return NewServer(buckets, new(DefaultOperator), ServerOptions{
		WriteWait:       10 * time.Second,
		PongWait:        60 * time.Second,
		PingPeriod:      54 * time.Second,
		MaxMessageSize:  512,
		ReadBufferSize:  512,
		WriteBufferSize: 512,
		BroadcastSize:   8,
	})
}

// Test case 13: Registry.Add then Snapshot contains the channel; Len is 1.
func TestRegistryAddMakesAChannelVisible(t *testing.T) {
	r := NewRegistry(4)
	ch := NewChannel(8)

	r.Add(ch)

	if got := r.Len(); got != 1 {
		t.Fatalf("Len after one Add: got %d, want 1", got)
	}
	if !containsChannel(r.Snapshot(), ch) {
		t.Fatal("Snapshot does not contain the added channel")
	}
}

// Test case 14: Registry.Remove then Snapshot does not contain it; Len is 0.
func TestRegistryRemoveForgetsAChannel(t *testing.T) {
	r := NewRegistry(4)
	ch := NewChannel(8)

	r.Add(ch)
	r.Remove(ch)

	if got := r.Len(); got != 0 {
		t.Fatalf("Len after Add then Remove: got %d, want 0", got)
	}
	if containsChannel(r.Snapshot(), ch) {
		t.Fatal("Snapshot still contains a removed channel")
	}
}

// Test case 15: adding more channels than there are shards leaves every one of
// them present in Snapshot, and Len equals the number added.
func TestRegistrySpreadsAcrossShardsWithoutLosingAnyone(t *testing.T) {
	const shards = 4
	const channels = 4*shards + 3

	r := NewRegistry(shards)

	added := make([]*Channel, 0, channels)
	for i := 0; i < channels; i++ {
		ch := NewChannel(8)
		r.Add(ch)
		added = append(added, ch)
	}

	if got := r.Len(); got != channels {
		t.Fatalf("Len: got %d, want %d", got, channels)
	}

	snapshot := r.Snapshot()
	for i, ch := range added {
		if !containsChannel(snapshot, ch) {
			t.Fatalf("channel %d is missing from Snapshot; a connection that shutdown "+
				"cannot see is a connection it cannot close", i)
		}
	}
}

// Test case 16: WaitEmpty returns nil promptly once the last channel is removed.
func TestWaitEmptyReturnsWhenTheLastChannelGoes(t *testing.T) {
	r := NewRegistry(4)
	ch := NewChannel(8)
	r.Add(ch)

	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { done <- r.WaitEmpty(ctx) }()

	time.Sleep(50 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("WaitEmpty returned while a channel was still registered")
	default:
	}

	r.Remove(ch)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WaitEmpty returned %v, want nil once the registry emptied", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitEmpty did not return after the last channel was removed")
	}
}

// Test case 17: WaitEmpty returns the context error when the registry is still
// occupied at the deadline.
func TestWaitEmptyGivesUpAtTheDeadline(t *testing.T) {
	r := NewRegistry(4)
	r.Add(NewChannel(8))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := r.WaitEmpty(ctx)
	took := time.Since(start)

	if err == nil {
		t.Fatal("WaitEmpty returned nil with a channel still registered, want the context error")
	}
	if took > 2*time.Second {
		t.Fatalf("WaitEmpty took %v to give up, want about the deadline", took)
	}
}

// Test case 18: serveWs while draining -> 503, no upgrade, and the registry
// stays empty.
func TestServeWsRefusesWhileDraining(t *testing.T) {
	c := New()
	server := testServer(t)
	server.SetDraining()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.serveWs(server, w, r)
	}))
	defer ts.Close()

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(ts.URL), nil)
	if err == nil {
		conn.Close()
		t.Fatal("the upgrade succeeded while draining; a departing instance must not take new connections")
	}
	if resp == nil {
		t.Fatalf("dial failed without a response: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("upgrade while draining: got %d, want 503", resp.StatusCode)
	}
	if got := server.Registry.Len(); got != 0 {
		t.Fatalf("registry holds %d connections after a refused upgrade, want 0", got)
	}
}

// Test case 19: serveWs while not draining -> upgrade succeeds and the
// connection appears in the registry.
func TestServeWsRegistersAnAcceptedConnection(t *testing.T) {
	c := New()
	server := testServer(t)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.serveWs(server, w, r)
	}))
	defer ts.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(ts.URL), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if !eventually(2*time.Second, func() bool { return server.Registry.Len() == 1 }) {
		t.Fatalf("registry holds %d connections after an accepted upgrade, want 1 — "+
			"an unauthenticated connection still has to be drained", server.Registry.Len())
	}
}

// Test case 20: acceptTcp while draining -> the accepted connection is closed
// and never registered.
func TestAcceptTcpRefusesWhileDraining(t *testing.T) {
	c := New()
	DefaultServer = testServer(t)
	DefaultServer.SetDraining()

	ln, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	go c.acceptTcp(ln)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err == nil {
		t.Fatal("the connection stayed open while draining, want it closed")
	}

	if got := DefaultServer.Registry.Len(); got != 0 {
		t.Fatalf("registry holds %d connections after a refused accept, want 0", got)
	}
}

func containsChannel(chs []*Channel, want *Channel) bool {
	for _, ch := range chs {
		if ch == want {
			return true
		}
	}
	return false
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/ws"
}

func eventually(within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}
