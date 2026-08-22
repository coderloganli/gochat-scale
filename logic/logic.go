/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 18:25
 */
package logic

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"gochat/config"
	"gochat/db"
	"gochat/pkg/lifecycle"
	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	rpcxserver "github.com/smallnest/rpcx/server"
)

type Logic struct {
	ServerId string

	rpcServers     []*rpcxserver.Server
	metricsSrv     *http.Server
	tracerShutdown func(context.Context) error
}

func New() *Logic {
	return new(Logic)
}

// Start brings logic up and returns the way to stop it. It does not block.
func (logic *Logic) Start() (lifecycle.Stopper, error) {
	//read config
	logicConfig := config.Conf.Logic

	runtime.GOMAXPROCS(logicConfig.LogicBase.CpuNum)
	logic.ServerId = fmt.Sprintf("logic-%s", uuid.New().String())

	// Initialize tracer. Its shutdown belongs in the stopper, not in a defer:
	// Start returns while the process keeps running, so a defer here shut the
	// tracer down a few milliseconds after startup and left logic untraced.
	tracingCfg := tracing.Config{
		Enabled:      config.Conf.Common.CommonTracing.Enabled,
		Endpoint:     config.Conf.Common.CommonTracing.Endpoint,
		SamplingRate: config.Conf.Common.CommonTracing.SamplingRate,
	}
	shutdown, err := tracing.InitTracer("logic", tracingCfg)
	if err != nil {
		logrus.Errorf("Failed to initialize tracer: %v", err)
	} else {
		logic.tracerShutdown = shutdown
	}

	//init metrics server
	logic.metricsSrv = metrics.StartMetricsServer(9091)

	//init database connection pool, shared by all logic replicas
	if err := db.Init(); err != nil {
		return nil, fmt.Errorf("logic init db: %w", err)
	}
	db.InitInstrumentedDB(db.DefaultDbName, "logic")

	//init publish redis
	if err := logic.InitPublishRedisClient(); err != nil {
		return nil, fmt.Errorf("logic init publishRedisClient: %w", err)
	}

	//init RabbitMQ client
	if err := logic.InitRabbitMQClient(); err != nil {
		return nil, fmt.Errorf("logic init RabbitMQ client: %w", err)
	}

	//init rpc server
	if err := logic.InitRpcServer(); err != nil {
		return nil, fmt.Errorf("logic init rpc server: %w", err)
	}

	return logic.Stop, nil
}

// Stop deregisters logic from etcd before it goes, so that callers stop being
// handed an address that no longer answers.
func (logic *Logic) Stop(ctx context.Context) error {
	metrics.SetDraining()

	for _, s := range logic.rpcServers {
		if s == nil {
			continue
		}
		// rpcx's Shutdown is what unregisters; it also polls for in-flight
		// requests until its context expires, so it gets its own slice of the
		// budget rather than all of it.
		stopCtx, cancel := context.WithTimeout(ctx, time.Second)
		if err := s.Shutdown(stopCtx); err != nil {
			logrus.Warnf("logic rpc server shutdown: %v", err)
		}
		cancel()
	}

	var firstErr error
	if logic.metricsSrv != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := metrics.ShutdownMetricsServer(stopCtx, logic.metricsSrv); err != nil {
			logrus.Warnf("logic metrics server shutdown: %v", err)
			firstErr = err
		}
		cancel()
	}
	if logic.tracerShutdown != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := logic.tracerShutdown(stopCtx); err != nil {
			logrus.Warnf("logic tracer shutdown: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		cancel()
	}
	return firstErr
}
