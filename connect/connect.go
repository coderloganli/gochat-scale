/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 18:18
 */
package connect

import (
	"context"
	"fmt"
	"net"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"strings"
	"time"

	rpcxserver "github.com/smallnest/rpcx/server"

	"gochat/config"
	"gochat/pkg/lifecycle"
	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

var DefaultServer *Server

type Connect struct {
	ServerId string

	// What Stop has to take down. Each is optional: a role that never started
	// one leaves it nil, and Stop skips it.
	server          *Server
	httpSrv         *http.Server
	rpcServers      []*rpcxserver.Server
	tcpListeners    []*net.TCPListener
	metricsSrv      *http.Server
	tracerShutdown  func(context.Context) error
	metricsRoleName string
}

func New() *Connect {
	// The websocket role is the default; RunTcp renames itself. The names match
	// the tracer service names and the compose services, so a shutdown metric
	// can be told apart from the other role's.
	return &Connect{metricsRoleName: "connect-ws"}
}

// Start brings up the websocket role and returns the way to stop it. It does
// not block: main.go owns the signal, and pkg/lifecycle owns the budget.
func (c *Connect) Start() (lifecycle.Stopper, error) {
	// get Connect layer config
	connectConfig := config.Conf.Connect

	//set the maximum number of CPUs that can be executing
	runtime.GOMAXPROCS(connectConfig.ConnectBucket.CpuNum)

	// Initialize tracer. Its shutdown goes into the stopper, not into a defer:
	// Start returns while the process keeps running, so a defer here would tear
	// the tracer down a few milliseconds after startup.
	tracingCfg := tracing.Config{
		Enabled:      config.Conf.Common.CommonTracing.Enabled,
		Endpoint:     config.Conf.Common.CommonTracing.Endpoint,
		SamplingRate: config.Conf.Common.CommonTracing.SamplingRate,
	}
	shutdown, err := tracing.InitTracer("connect-ws", tracingCfg)
	if err != nil {
		logrus.Errorf("Failed to initialize tracer: %v", err)
	} else {
		c.tracerShutdown = shutdown
	}

	//declare what this process must be able to do before it is ready, before the
	//listener starts: an empty registry reports ready
	registerHealthChecks(len(strings.Split(config.Conf.Connect.ConnectRpcAddressWebSockts.Address, ",")))

	//init metrics server
	c.metricsSrv = metrics.StartMetricsServer(9092)
	//export the connection gauge from the start, so an idle pod reports zero
	//rather than reporting nothing - the autoscaler cannot tell those apart
	initConnectionMetrics(serviceWebsocket, connTypeWebsocket)

	//init logic layer rpc client, call logic layer rpc server
	if err := c.InitLogicRpcClient(); err != nil {
		return nil, fmt.Errorf("InitLogicRpcClient: %w", err)
	}
	//init Connect layer rpc server, logic client will call this
	c.server = newConnectServer(connectConfig)
	DefaultServer = c.server
	bucketsReady()
	c.ServerId = fmt.Sprintf("%s-%s", "ws", uuid.New().String())
	//init Connect layer rpc server ,task layer will call this
	if err := c.InitConnectWebsocketRpcServer(); err != nil {
		return nil, fmt.Errorf("InitConnectWebsocketRpcServer: %w", err)
	}

	//start Connect layer server handler persistent connection
	if err := c.InitWebsocket(); err != nil {
		return nil, fmt.Errorf("InitWebsocket: %w", err)
	}

	return c.Stop, nil
}

// StartTcp brings up the TCP role and returns the way to stop it.
func (c *Connect) StartTcp() (lifecycle.Stopper, error) {
	c.metricsRoleName = "connect-tcp"
	initTcpLogicRpc()

	// get Connect layer config
	connectConfig := config.Conf.Connect

	//set the maximum number of CPUs that can be executing
	runtime.GOMAXPROCS(connectConfig.ConnectBucket.CpuNum)

	// Initialize tracer; see the note in Start about why this is not a defer.
	tracingCfg := tracing.Config{
		Enabled:      config.Conf.Common.CommonTracing.Enabled,
		Endpoint:     config.Conf.Common.CommonTracing.Endpoint,
		SamplingRate: config.Conf.Common.CommonTracing.SamplingRate,
	}
	shutdown, err := tracing.InitTracer("connect-tcp", tracingCfg)
	if err != nil {
		logrus.Errorf("Failed to initialize tracer: %v", err)
	} else {
		c.tracerShutdown = shutdown
	}

	registerHealthChecks(len(strings.Split(config.Conf.Connect.ConnectRpcAddressTcp.Address, ",")))

	//init metrics server
	c.metricsSrv = metrics.StartMetricsServer(9093)
	initConnectionMetrics(serviceTcp, connTypeTcp)

	//init logic layer rpc client, call logic layer rpc server
	if err := c.InitLogicRpcClient(); err != nil {
		return nil, fmt.Errorf("InitLogicRpcClient: %w", err)
	}
	//init Connect layer rpc server, logic client will call this
	c.server = newConnectServer(connectConfig)
	DefaultServer = c.server
	bucketsReady()
	c.ServerId = fmt.Sprintf("%s-%s", "tcp", uuid.New().String())
	//init Connect layer rpc server ,task layer will call this
	if err := c.InitConnectTcpRpcServer(); err != nil {
		return nil, fmt.Errorf("InitConnectTcpRpcServer: %w", err)
	}
	//start Connect layer server handler persistent connection by tcp
	if err := c.InitTcpServer(); err != nil {
		return nil, fmt.Errorf("InitTcpServer: %w", err)
	}

	return c.Stop, nil
}

// newConnectServer builds the bucket set and the server both roles share.
func newConnectServer(connectConfig config.ConnectConfig) *Server {
	buckets := make([]*Bucket, connectConfig.ConnectBucket.CpuNum)
	for i := 0; i < connectConfig.ConnectBucket.CpuNum; i++ {
		buckets[i] = NewBucket(BucketOptions{
			ChannelSize:   connectConfig.ConnectBucket.Channel,
			RoomSize:      connectConfig.ConnectBucket.Room,
			RoutineAmount: connectConfig.ConnectBucket.RoutineAmount,
			RoutineSize:   connectConfig.ConnectBucket.RoutineSize,
		})
	}
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
