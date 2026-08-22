// Command drainbench measures what a connect instance's departure costs its
// clients.
//
// It is a Go program rather than a k6 script because the measurement is per
// connection and turns on close codes, which shell cannot see and k6's
// WebSocket API does not expose usefully.
//
// One run measures one arm. scripts/drain-demo.sh runs it twice against the
// same image, with GOCHAT_GRACEFUL_SHUTDOWN true and then false, and prints the
// comparison. See docs/benchmarks.md.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	clientv3 "go.etcd.io/etcd/client/v3"
)

var (
	connections  = flag.Int("connections", 50, "websocket connections to hold open, split across the replicas")
	replicas     = flag.Int("replicas", 2, "connect-ws replicas; replica 1 is the one restarted")
	roomId       = flag.Int("room", 1, "room the connections join")
	apiURL       = flag.String("api", "http://localhost:7070", "api base url")
	etcdAddr     = flag.String("etcd", "localhost:2379", "etcd client address")
	composeFiles = flag.String("compose-files", "docker-compose.yml,deployments/docker-compose.drain.yml", "comma separated compose files")
	project      = flag.String("project", "gochat-drain", "compose project name")
	sendRate     = flag.Duration("send-interval", 200*time.Millisecond, "interval between room messages during the window")
	window       = flag.Duration("window", 20*time.Second, "how long to keep measuring after the restart begins")
	arm          = flag.String("arm", "graceful", "name of this arm, for the report")
	jsonOut      = flag.String("json", "", "also write the result as JSON to this path")
)

const connectService = "connect-ws"

// result is one arm's measurement.
type result struct {
	Arm string `json:"arm"`

	Restarted int `json:"restartedConnections"`
	Surviving int `json:"survivingConnections"`

	GoingAway   int `json:"closedWithGoingAway"`
	OtherClose  int `json:"closedWithOtherCloseCode"`
	NoCloseCode int `json:"closedWithoutACloseCode"`
	StillOpen   int `json:"stillOpenAtEndOfWindow"`

	NoticeP50 time.Duration `json:"noticeP50"`
	NoticeP95 time.Duration `json:"noticeP95"`
	NoticeMax time.Duration `json:"noticeMax"`

	EtcdResidency time.Duration `json:"etcdResidencyAfterStop"`
	EtcdStillup   bool          `json:"etcdNodeStillPresentAtEndOfWindow"`

	MessagesSent           int `json:"roomMessagesSent"`
	SurvivorFramesReceived int `json:"framesReceivedOnSurvivingReplica"`
	SurvivorClosed         int `json:"survivingConnectionsClosedByTheRestart"`
}

func main() {
	flag.Parse()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "drainbench: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	addrs, err := replicaAddrs(ctx)
	if err != nil {
		return err
	}
	if len(addrs) < 2 {
		return fmt.Errorf("need at least 2 connect-ws replicas, found %d", len(addrs))
	}
	fmt.Fprintf(os.Stderr, "connect-ws replicas: %s\n", strings.Join(addrs, ", "))

	clients, err := openConnections(addrs)
	if err != nil {
		return err
	}
	defer func() {
		for _, c := range clients {
			c.conn.Close()
		}
	}()

	res := result{Arm: *arm}
	for _, c := range clients {
		if c.onRestarted {
			res.Restarted++
		} else {
			res.Surviving++
		}
	}
	fmt.Fprintf(os.Stderr, "holding %d connections (%d on the replica to be restarted)\n",
		len(clients), res.Restarted)

	// Read from every connection for the whole window, so that a close code is
	// observed rather than inferred.
	var wg sync.WaitGroup
	deadline := time.Now().Add(*window)
	for _, c := range clients {
		wg.Add(1)
		go func(c *client) { defer wg.Done(); c.read(deadline) }(c)
	}

	senderDone := make(chan int, 1)
	go func() { senderDone <- sendRoomMessages(deadline) }()

	etcdBefore, err := connectRegistrations(ctx)
	if err != nil {
		return err
	}

	restartStart := time.Now()
	for _, c := range clients {
		c.restartAt = restartStart
	}
	fmt.Fprintf(os.Stderr, "restarting %s replica 1\n", connectService)
	if err := restartReplica(ctx, 1); err != nil {
		return err
	}

	residency, stillUp := etcdResidency(ctx, restartStart, etcdBefore, deadline)
	res.EtcdResidency = residency
	res.EtcdStillup = stillUp

	wg.Wait()
	res.MessagesSent = <-senderDone

	var notices []time.Duration
	for _, c := range clients {
		if c.onRestarted {
			switch {
			case !c.closed:
				res.StillOpen++
			case c.closeCode == websocket.CloseGoingAway:
				res.GoingAway++
			case c.closeCode == 0:
				res.NoCloseCode++
			default:
				res.OtherClose++
			}
			if c.closed {
				notices = append(notices, c.noticedAfter)
			}
			continue
		}
		res.SurvivorFramesReceived += c.received
		if c.closed {
			res.SurvivorClosed++
		}
	}
	res.NoticeP50, res.NoticeP95, res.NoticeMax = percentiles(notices)

	printReport(res)

	if *jsonOut != "" {
		b, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonOut, append(b, '\n'), 0o644); err != nil {
			return err
		}
	}
	return nil
}

type client struct {
	conn        *websocket.Conn
	onRestarted bool
	restartAt   time.Time

	closed       bool
	closeCode    int
	noticedAfter time.Duration
	received     int
}

func (c *client) read(until time.Time) {
	for {
		c.conn.SetReadDeadline(until)
		_, _, err := c.conn.ReadMessage()
		if err == nil {
			c.received++
			if time.Now().After(until) {
				return
			}
			continue
		}
		if time.Now().After(until) && !isCloseError(err) {
			// The window ended with the connection still open.
			return
		}
		c.closed = true
		if !c.restartAt.IsZero() {
			c.noticedAfter = time.Since(c.restartAt)
		}
		var ce *websocket.CloseError
		if ok := asCloseError(err, &ce); ok {
			c.closeCode = ce.Code
		}
		return
	}
}

func isCloseError(err error) bool {
	var ce *websocket.CloseError
	return asCloseError(err, &ce)
}

func asCloseError(err error, target **websocket.CloseError) bool {
	if ce, ok := err.(*websocket.CloseError); ok {
		*target = ce
		return true
	}
	return false
}

// openConnections registers a user per connection and holds it open, spread
// across the replicas. Replica index 1 is the one that gets restarted.
func openConnections(addrs []string) ([]*client, error) {
	clients := make([]*client, 0, *connections)
	for i := 0; i < *connections; i++ {
		addr := addrs[i%len(addrs)]

		token, err := registerUser(fmt.Sprintf("drain_%d_%d", time.Now().UnixNano(), i))
		if err != nil {
			return clients, err
		}

		url := fmt.Sprintf("ws://%s/ws", addr)
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			return clients, fmt.Errorf("dial %s: %w", url, err)
		}
		auth := map[string]interface{}{"authToken": token, "roomId": *roomId}
		if err := conn.WriteJSON(auth); err != nil {
			return clients, fmt.Errorf("auth: %w", err)
		}
		clients = append(clients, &client{conn: conn, onRestarted: addr == addrs[0]})
	}
	// Let the Connect RPCs settle before anything is restarted.
	time.Sleep(2 * time.Second)
	return clients, nil
}

func registerUser(name string) (string, error) {
	body, _ := json.Marshal(map[string]string{"userName": name, "passWord": "drainbench"})
	resp, err := http.Post(*apiURL+"/user/register", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("register %s: %s", name, raw)
	}
	if payload.Data == "" {
		return "", fmt.Errorf("register %s returned no token: %s", name, raw)
	}
	return payload.Data, nil
}

// sendRoomMessages pushes to the room for the whole window and reports how many
// went out, so a receive count has something to be compared against.
func sendRoomMessages(until time.Time) int {
	token, err := registerUser(fmt.Sprintf("drain_sender_%d", time.Now().UnixNano()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "sender: %v\n", err)
		return 0
	}

	sent := 0
	for time.Now().Before(until) {
		body, _ := json.Marshal(map[string]interface{}{
			"authToken": token,
			"msg":       fmt.Sprintf("drain %d", sent),
			"roomId":    *roomId,
		})
		resp, err := http.Post(*apiURL+"/push/pushRoom", "application/json", bytes.NewReader(body))
		if err == nil {
			// The api answers HTTP 200 to almost everything and puts the real
			// status in the body, so counting responses would count failures as
			// sends and make every received-versus-sent comparison meaningless.
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			var payload struct {
				Code int `json:"code"`
			}
			if json.Unmarshal(raw, &payload) == nil && payload.Code == 0 {
				sent++
			} else {
				fmt.Fprintf(os.Stderr, "push rejected: %s\n", raw)
			}
		}
		time.Sleep(*sendRate)
	}
	return sent
}

// replicaAddrs asks compose where each replica's websocket port landed.
func replicaAddrs(ctx context.Context) ([]string, error) {
	var addrs []string
	for i := 1; i <= *replicas; i++ {
		out, err := compose(ctx, "port", "--index", fmt.Sprint(i), connectService, "7000")
		if err != nil {
			return nil, err
		}
		addr := strings.TrimSpace(out)
		if addr == "" {
			return nil, fmt.Errorf("no published port for %s replica %d", connectService, i)
		}
		// compose prints 0.0.0.0:PORT; dial localhost.
		if idx := strings.LastIndex(addr, ":"); idx >= 0 {
			addr = "localhost" + addr[idx:]
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

// connectRegistrations lists the connect instances currently in etcd.
func connectRegistrations(ctx context.Context) (map[string]bool, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{*etcdAddr},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	resp, err := cli.Get(ctx, "/gochat_srv/ConnectRpc", clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return nil, err
	}
	keys := make(map[string]bool, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		keys[string(kv.Key)] = true
	}
	return keys, nil
}

// etcdResidency reports how long the departing instance's registration outlived
// the stop, which is how long task kept routing messages into a hole.
func etcdResidency(ctx context.Context, from time.Time, before map[string]bool, until time.Time) (time.Duration, bool) {
	for time.Now().Before(until) {
		now, err := connectRegistrations(ctx)
		if err == nil {
			gone := false
			for key := range before {
				if !now[key] {
					gone = true
					break
				}
			}
			if gone {
				return time.Since(from), false
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return time.Since(from), true
}

// restartReplica restarts one replica by container name.
//
// `docker compose restart` has no --index, so this goes through `docker
// restart` on the name compose gives the container. No -t: without one, docker
// uses the container's configured stop timeout, which is the stop_grace_period
// the compose file sets — the same budget a real deployment would give it.
func restartReplica(ctx context.Context, index int) error {
	name := fmt.Sprintf("%s-%s-%d", *project, connectService, index)
	cmd := exec.CommandContext(ctx, "docker", "restart", name)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker restart %s: %w: %s", name, err, stderr.String())
	}
	return nil
}

func compose(ctx context.Context, args ...string) (string, error) {
	full := []string{"compose", "-p", *project}
	for _, f := range strings.Split(*composeFiles, ",") {
		if f = strings.TrimSpace(f); f != "" {
			full = append(full, "-f", f)
		}
	}
	full = append(full, args...)

	cmd := exec.CommandContext(ctx, "docker", full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(full, " "), err, stderr.String())
	}
	return stdout.String(), nil
}

func percentiles(ds []time.Duration) (p50, p95, max time.Duration) {
	if len(ds) == 0 {
		return 0, 0, 0
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	at := func(q float64) time.Duration {
		idx := int(q * float64(len(ds)-1))
		return ds[idx]
	}
	return at(0.50), at(0.95), ds[len(ds)-1]
}

func printReport(r result) {
	fmt.Printf("\n## arm: %s\n\n", r.Arm)
	fmt.Printf("%-46s %s\n", "connections on the restarted replica", fmt.Sprint(r.Restarted))
	fmt.Printf("%-46s %s\n", "  closed with 1001 going away", share(r.GoingAway, r.Restarted))
	fmt.Printf("%-46s %s\n", "  closed with another close code", share(r.OtherClose, r.Restarted))
	fmt.Printf("%-46s %s\n", "  closed with no close code at all", share(r.NoCloseCode, r.Restarted))
	fmt.Printf("%-46s %s\n", "  still open when the window ended", share(r.StillOpen, r.Restarted))
	fmt.Printf("%-46s %v / %v / %v\n", "  time to notice p50 / p95 / max", r.NoticeP50, r.NoticeP95, r.NoticeMax)
	fmt.Println()
	fmt.Printf("%-46s %v%s\n", "etcd registration outlived the stop by", r.EtcdResidency, stillUp(r.EtcdStillup))
	fmt.Println()
	fmt.Printf("%-46s %d\n", "room messages sent during the window", r.MessagesSent)
	fmt.Printf("%-46s %d\n", "connections on the surviving replica", r.Surviving)
	fmt.Printf("%-46s %d\n", "  frames they received in total", r.SurvivorFramesReceived)
	fmt.Printf("%-46s %d\n", "  that were closed by the restart", r.SurvivorClosed)
	fmt.Println()
}

func share(n, total int) string {
	if total == 0 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%d (%.0f%%)", n, 100*float64(n)/float64(total))
}

func stillUp(v bool) string {
	if v {
		return " (still present when the window ended)"
	}
	return ""
}
