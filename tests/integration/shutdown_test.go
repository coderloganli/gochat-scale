package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"gochat/tests/helpers"
	"gochat/tests/testdata"
)

const (
	connectService     = "connect-ws"
	connectHealthURL   = "http://localhost:9092/health"
	connectReadyURL    = "http://localhost:9092/ready"
	connectMetricsURL  = "http://localhost:9092/metrics"
	jaegerServicesURL  = "http://localhost:16686/api/services"
	shutdownBudget     = 5 * time.Second
	shutdownBudgetSlop = 10 * time.Second
)

// gracefulOnly skips a test unless the stack is running with graceful shutdown
// on, which is the default. The control arm runs the same stack with
// GOCHAT_GRACEFUL_SHUTDOWN=false and TEST_GRACEFUL=false.
func gracefulOnly(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if strings.EqualFold(helpersGetEnv("TEST_GRACEFUL", "true"), "false") {
		t.Skip("stack is running the control arm; this case describes the graceful arm")
	}
}

// controlArmOnly is the mirror of gracefulOnly.
func controlArmOnly(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	if !strings.EqualFold(helpersGetEnv("TEST_GRACEFUL", "true"), "false") {
		t.Skip("stack is running the graceful arm; this case describes the control arm")
	}
}

func helpersGetEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// waitForConnect blocks until connect-ws is serving again.
//
// These cases stop and restart the service, and docker compose returns before
// the process inside is ready. Without this, a case that follows a restart
// fails on its first dial and reports a defect in the wrong place.
//
// /ready is the readiness signal: 200 means the instance is up, has its
// dependencies, and is not draining. /health only means the process is alive,
// which is true well before it can serve.
func waitForConnect(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if code, _ := getStatus(connectReadyURL); code == http.StatusOK {
			// Healthy, but the rpc registration and the logic client may still
			// be settling.
			time.Sleep(2 * time.Second)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("connect-ws did not become ready within 90s")
}

// restoreConnect starts connect-ws again and waits for it, so the case that
// runs next does not inherit a stopped service.
func restoreConnect(t *testing.T) {
	t.Helper()
	if err := helpers.StartService(context.Background(), connectService); err != nil {
		t.Logf("restarting %s: %v", connectService, err)
	}
	waitForConnect(t)
}

// restoreLogic starts logic again and waits until it has re-registered in etcd.
//
// Readiness for logic is "callers can find it", not "the container is up": a
// connect instance that authenticates against a logic which has not registered
// yet closes the connection, which shows up as a failure in whichever case runs
// next rather than here.
func restoreLogic(t *testing.T) {
	t.Helper()

	if err := helpers.StartService(context.Background(), "logic"); err != nil {
		t.Logf("restarting logic: %v", err)
	}

	cfg := helpers.DefaultTestConfig()
	cli, err := helpers.EtcdClient(cfg)
	if err != nil {
		t.Fatalf("etcd client: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for ctx.Err() == nil {
		keys, err := helpers.EtcdKeys(ctx, cli, helpers.LogicServicePath)
		if err == nil && len(keys) > 0 {
			// Registered, but the connect and api clients have to notice.
			time.Sleep(3 * time.Second)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("logic did not re-register in etcd within 90s")
}

// connectedUser registers a user, opens a WebSocket, and authenticates it.
func connectedUser(t *testing.T) (userId int, conn *websocket.Conn) {
	t.Helper()

	waitForConnect(t)

	cfg := helpers.DefaultTestConfig()
	api := helpers.NewAPIClient(cfg.APIBaseURL)

	name := testdata.GenerateTestUserName()
	reg, err := api.Register(name, testdata.TestPassword)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	token := reg.GetDataAsString()

	conn, _, err = websocket.DefaultDialer.Dial(cfg.WSBaseURL, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", cfg.WSBaseURL, err)
	}

	auth := map[string]interface{}{"authToken": token, "roomId": testdata.DefaultRoomID}
	if err := conn.WriteJSON(auth); err != nil {
		t.Fatalf("send auth: %v", err)
	}

	// The user id comes back through the session, not the socket; read it from
	// the API so the test can look the routing key up in Redis.
	check, err := api.CheckAuth(token)
	if err != nil {
		t.Fatalf("CheckAuth: %v", err)
	}
	data := check.GetDataAsMap()
	raw, ok := data["userId"]
	if !ok {
		t.Fatalf("CheckAuth returned no userId: %v", data)
	}
	switch v := raw.(type) {
	case float64:
		userId = int(v)
	case json.Number:
		n, _ := v.Int64()
		userId = int(n)
	default:
		t.Fatalf("CheckAuth userId has unexpected type %T", raw)
	}

	// Give the connect layer a moment to finish the Connect RPC that writes the
	// routing key, so the test is not racing registration.
	time.Sleep(time.Second)
	return userId, conn
}

func routingKey(userId int) string {
	return fmt.Sprintf("gochat_%d", userId)
}

// Test case 26: on a restart, a connected client's read fails with a
// CloseError whose Code is 1001, not an abnormal closure.
func TestShutdownSendsGoingAway(t *testing.T) {
	gracefulOnly(t)

	_, conn := connectedUser(t)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := helpers.RestartService(ctx, connectService); err != nil {
		t.Fatalf("restart %s: %v", connectService, err)
	}

	conn.SetReadDeadline(time.Now().Add(shutdownBudgetSlop))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("the connection survived the restart, want it closed")
	}

	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("read after restart returned %v (%T), want a *websocket.CloseError — "+
			"a client cannot tell a planned shutdown from a network failure without one", err, err)
	}
	if closeErr.Code != websocket.CloseGoingAway {
		t.Fatalf("close code is %d, want %d (going away)", closeErr.Code, websocket.CloseGoingAway)
	}
}

// Test case 27: after the shutdown, gochat_<userId> is absent from Redis.
func TestShutdownClearsTheRoutingKey(t *testing.T) {
	gracefulOnly(t)

	userId, conn := connectedUser(t)
	defer conn.Close()

	cfg := helpers.DefaultTestConfig()
	rh := helpers.NewRedisHelper(cfg.RedisAddress)
	defer rh.Close()

	if exists, err := rh.KeyExists(routingKey(userId)); err != nil {
		t.Fatalf("KeyExists before: %v", err)
	} else if !exists {
		t.Fatalf("%s is missing before the shutdown; the test is not exercising what it thinks", routingKey(userId))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := helpers.RestartService(ctx, connectService); err != nil {
		t.Fatalf("restart %s: %v", connectService, err)
	}
	time.Sleep(shutdownBudget)

	exists, err := rh.KeyExists(routingKey(userId))
	if err != nil {
		t.Fatalf("KeyExists after: %v", err)
	}
	if exists {
		t.Fatalf("%s survived the shutdown; messages for that user keep being routed "+
			"to an instance that has gone", routingKey(userId))
	}
}

// Test case 28: after the shutdown, the room's online count no longer includes
// the disconnected user, and the room membership hash no longer has their field.
func TestShutdownLeavesTheRoom(t *testing.T) {
	gracefulOnly(t)

	userId, conn := connectedUser(t)
	defer conn.Close()

	cfg := helpers.DefaultTestConfig()
	rh := helpers.NewRedisHelper(cfg.RedisAddress)
	defer rh.Close()

	before, err := rh.GetRoomUserCount(testdata.DefaultRoomID)
	if err != nil {
		t.Fatalf("room count before: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := helpers.RestartService(ctx, connectService); err != nil {
		t.Fatalf("restart %s: %v", connectService, err)
	}
	time.Sleep(shutdownBudget)

	after, err := rh.GetRoomUserCount(testdata.DefaultRoomID)
	if err != nil {
		t.Fatalf("room count after: %v", err)
	}
	if after >= before {
		t.Errorf("room online count went from %d to %d, want it to fall", before, after)
	}

	members, err := rh.GetSessionData(fmt.Sprintf("gochat_room_%d", testdata.DefaultRoomID))
	if err != nil {
		t.Fatalf("room membership: %v", err)
	}
	if _, ok := members[fmt.Sprintf("%d", userId)]; ok {
		t.Errorf("user %d is still a room member after the shutdown", userId)
	}
}

// Test case 29: the restarted instance's etcd node disappears within the
// shutdown budget, not after the two-minute TTL.
func TestShutdownDeregistersFromEtcd(t *testing.T) {
	gracefulOnly(t)

	cfg := helpers.DefaultTestConfig()
	cli, err := helpers.EtcdClient(cfg)
	if err != nil {
		t.Fatalf("etcd client: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	before, err := helpers.EtcdKeys(ctx, cli, helpers.ConnectServicePath)
	if err != nil {
		t.Fatalf("etcd keys before: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("no connect instance is registered in etcd; the test is not exercising what it thinks")
	}

	if err := helpers.StopService(ctx, connectService); err != nil {
		t.Fatalf("stop %s: %v", connectService, err)
	}
	defer restoreConnect(t)

	deadline := time.Now().Add(shutdownBudget + 2*time.Second)
	for time.Now().Before(deadline) {
		after, err := helpers.EtcdKeys(ctx, cli, helpers.ConnectServicePath)
		if err != nil {
			t.Fatalf("etcd keys after: %v", err)
		}
		if len(after) < len(before) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("the connect registration outlived the process by more than %v; task keeps "+
		"routing to an instance that has gone until the TTL expires", shutdownBudget)
}

// Test case 30: a draining instance stops being ready before it stops being
// alive, and stays observable throughout.
//
// Revised where this branch met the Kubernetes work: the drain signal is on
// /ready, not /health. /health is the liveness probe, and a liveness probe that
// fails asks the kubelet to restart the container - in the middle of the
// shutdown that is trying to close connections cleanly. Asserting both halves is
// what this case was always describing; it just had one endpoint to say it with.
func TestDrainingWithdrawsReadinessWhileStillAlive(t *testing.T) {
	gracefulOnly(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Hold a connection that never answers, so the drain lasts the whole budget
	// and the test has a window to observe.
	_, conn := connectedUser(t)
	defer conn.Close()

	stopped := make(chan error, 1)
	go func() { stopped <- helpers.StopService(context.Background(), connectService) }()
	defer func() {
		<-stopped
		restoreConnect(t)
	}()

	var sawDraining bool
	deadline := time.Now().Add(shutdownBudget)
	for time.Now().Before(deadline) {
		code, body := getStatus(connectReadyURL)
		if code == http.StatusServiceUnavailable {
			sawDraining = true
			if !strings.Contains(body, "draining") {
				t.Errorf("/ready body while draining is %q, want it to say draining", body)
			}
			// The other half, and the reason the signal moved here: the process
			// is leaving, not broken, so liveness must keep saying it is alive.
			if code, _ := getStatus(connectHealthURL); code != http.StatusOK {
				t.Errorf("/health returned %d while draining, want 200 - a liveness "+
					"probe failing here asks the kubelet to restart a pod that is "+
					"already shutting down cleanly", code)
			}
			if code, _ := getStatus(connectMetricsURL); code != http.StatusOK {
				t.Errorf("/metrics returned %d while draining, want 200 — a shutdown "+
					"that cannot be scraped cannot be measured", code)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !sawDraining {
		t.Fatal("/ready never reported 503 during the shutdown; nothing can tell that " +
			"this instance stopped being ready before it stopped being alive")
	}
	_ = ctx
}

// Test case 31: a client that never reads — so it never processes the close
// control frame and never replies — is still closed by the cap, and the
// container reaches a stopped state within the budget plus slack.
func TestShutdownForcesAClientThatNeverAnswers(t *testing.T) {
	gracefulOnly(t)

	_, conn := connectedUser(t)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	start := time.Now()
	if err := helpers.StopService(ctx, connectService); err != nil {
		t.Fatalf("stop %s: %v", connectService, err)
	}
	took := time.Since(start)
	defer restoreConnect(t)

	if took > shutdownBudget+shutdownBudgetSlop {
		t.Fatalf("stopping took %v; a client that never answers must not hold the "+
			"process open past its cap of %v", took, shutdownBudget)
	}

	conn.SetReadDeadline(time.Now().Add(shutdownBudgetSlop))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("the connection survived a completed shutdown, want it closed")
	}
}

// Test case 32: a new WebSocket connection attempted during the drain gets HTTP
// 503 from the upgrade request rather than an accepted connection that is then
// dropped.
//
// The window this case observes is short by design, and the design is right.
// Step 1 of the shutdown makes serveWs answer 503; step 3 closes the listener,
// after which an attempt is refused at the TCP level instead — also correct, and
// it supersedes the 503. So the 503 is only reachable between the two, which is
// at most deregisterBudget and often far less: a shutdown where the client
// answers its close frame has been seen to finish in 849 ms end to end.
//
// This case therefore reports one of three things, and never guesses:
//   - an upgrade that succeeded during the drain    -> failure, a real defect
//   - an upgrade refused with 503                   -> pass
//   - the listener already closed                   -> skip, window missed
//
// The behaviour itself is covered deterministically by unit case 18, which
// drives serveWs directly. What this case adds is that it holds in the real
// process; what it cannot do is promise to catch it every run.
func TestUpgradeIsRefusedWhileDraining(t *testing.T) {
	gracefulOnly(t)

	cfg := helpers.DefaultTestConfig()

	_, held := connectedUser(t)
	defer held.Close()

	stopped := make(chan error, 1)
	go func() { stopped <- helpers.StopService(context.Background(), connectService) }()
	defer func() {
		<-stopped
		restoreConnect(t)
	}()

	// Wait for the drain to have started rather than racing docker's delivery of
	// the signal: /ready flipping to 503 means the process is draining and
	// still alive, which is exactly the window this case is about.
	drainStarted := false
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if code, _ := getStatus(connectReadyURL); code == http.StatusServiceUnavailable {
			drainStarted = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !drainStarted {
		t.Fatal("the shutdown never reported itself as draining, so this case had no window to observe")
	}

	for i := 0; i < 100; i++ {
		conn, resp, err := websocket.DefaultDialer.Dial(cfg.WSBaseURL, nil)
		if err == nil {
			conn.Close()
			t.Fatal("an upgrade succeeded while the instance was draining; a departing " +
				"instance that keeps accepting connections never finishes draining")
		}
		if resp != nil && resp.StatusCode == http.StatusServiceUnavailable {
			return // the case it exists to prove
		}
		if resp != nil {
			t.Fatalf("upgrade during the drain answered %d, want 503", resp.StatusCode)
		}
		// No HTTP response at all: the listener has closed, so attempts are now
		// refused at the TCP level. That is the correct later state, not a
		// defect — but it is past the window this case can observe.
		t.Skipf("the listener closed before an upgrade could be attempted (%v); "+
			"the 503 path is covered deterministically by the serveWs unit case", err)
	}

	t.Skip("the drain ended before an upgrade could be attempted")
}

// Test case 33: sending the signal to logic removes its etcd node before the
// process exits.
func TestLogicDeregistersOnShutdown(t *testing.T) {
	gracefulOnly(t)

	cfg := helpers.DefaultTestConfig()
	cli, err := helpers.EtcdClient(cfg)
	if err != nil {
		t.Fatalf("etcd client: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	before, err := helpers.EtcdKeys(ctx, cli, helpers.LogicServicePath)
	if err != nil {
		t.Fatalf("etcd keys before: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("no logic instance is registered in etcd; the test is not exercising what it thinks")
	}

	if err := helpers.StopService(ctx, "logic"); err != nil {
		t.Fatalf("stop logic: %v", err)
	}
	defer restoreLogic(t)

	deadline := time.Now().Add(shutdownBudget + 2*time.Second)
	for time.Now().Before(deadline) {
		after, err := helpers.EtcdKeys(ctx, cli, helpers.LogicServicePath)
		if err != nil {
			t.Fatalf("etcd keys after: %v", err)
		}
		if len(after) < len(before) {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("the logic registration outlived the process by more than %v", shutdownBudget)
}

// Test case 34: with TRACING_SAMPLING_RATE=1.0, Jaeger reports traces for the
// logic service after the suite has driven traffic. Today it never does,
// because the tracer is shut down at startup.
func TestJaegerHasTracesForLogic(t *testing.T) {
	gracefulOnly(t)

	cfg := helpers.DefaultTestConfig()
	api := helpers.NewAPIClient(cfg.APIBaseURL)

	// Drive traffic that fans into logic, so there is something to trace.
	for i := 0; i < 5; i++ {
		name := testdata.GenerateTestUserName()
		if _, err := api.Register(name, testdata.TestPassword); err != nil {
			t.Fatalf("Register: %v", err)
		}
	}

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if jaegerHasService(t, "logic") {
			return
		}
		time.Sleep(time.Second)
	}

	t.Fatal("Jaeger lists no traces for the logic service; the tracer is shut down " +
		"immediately after startup, so the process runs untraced")
}

// Test case 35: with the control arm, the same restart gives the client a
// closure that is not 1001.
func TestControlArmDoesNotSendGoingAway(t *testing.T) {
	controlArmOnly(t)

	_, conn := connectedUser(t)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := helpers.RestartService(ctx, connectService); err != nil {
		t.Fatalf("restart %s: %v", connectService, err)
	}

	conn.SetReadDeadline(time.Now().Add(shutdownBudgetSlop))
	_, _, err := conn.ReadMessage()
	if err == nil {
		t.Fatal("the connection survived the restart, want it closed")
	}

	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) && closeErr.Code == websocket.CloseGoingAway {
		t.Fatal("the control arm sent a going-away close frame; GOCHAT_GRACEFUL_SHUTDOWN " +
			"has stopped switching the behaviour off, and the A/B is measuring one thing twice")
	}
}

// Test case 36: with the control arm, gochat_<userId> survives the restart.
func TestControlArmLeavesTheRoutingKeyBehind(t *testing.T) {
	controlArmOnly(t)

	userId, conn := connectedUser(t)
	defer conn.Close()

	cfg := helpers.DefaultTestConfig()
	rh := helpers.NewRedisHelper(cfg.RedisAddress)
	defer rh.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := helpers.RestartService(ctx, connectService); err != nil {
		t.Fatalf("restart %s: %v", connectService, err)
	}
	time.Sleep(shutdownBudget)

	exists, err := rh.KeyExists(routingKey(userId))
	if err != nil {
		t.Fatalf("KeyExists: %v", err)
	}
	if !exists {
		t.Fatal("the control arm cleared the routing key; GOCHAT_GRACEFUL_SHUTDOWN has " +
			"stopped switching the behaviour off")
	}
}

// Test case 37: with the control arm, the etcd node outlives the process by
// more than the shutdown budget.
func TestControlArmLeavesTheEtcdNodeBehind(t *testing.T) {
	controlArmOnly(t)

	cfg := helpers.DefaultTestConfig()
	cli, err := helpers.EtcdClient(cfg)
	if err != nil {
		t.Fatalf("etcd client: %v", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	before, err := helpers.EtcdKeys(ctx, cli, helpers.ConnectServicePath)
	if err != nil {
		t.Fatalf("etcd keys before: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("no connect instance is registered in etcd")
	}

	if err := helpers.StopService(ctx, connectService); err != nil {
		t.Fatalf("stop %s: %v", connectService, err)
	}
	defer restoreConnect(t)

	time.Sleep(shutdownBudget + 2*time.Second)

	after, err := helpers.EtcdKeys(ctx, cli, helpers.ConnectServicePath)
	if err != nil {
		t.Fatalf("etcd keys after: %v", err)
	}
	if len(after) < len(before) {
		t.Fatal("the control arm deregistered from etcd; GOCHAT_GRACEFUL_SHUTDOWN has " +
			"stopped switching the behaviour off")
	}
}

func getStatus(url string) (int, string) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return 0, ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func jaegerHasService(t *testing.T, service string) bool {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(jaegerServicesURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	var payload struct {
		Data []string `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	for _, s := range payload.Data {
		if s == service {
			return true
		}
	}
	return false
}
