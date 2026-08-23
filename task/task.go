/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 18:22
 */
package task

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"gochat/config"
	"gochat/pkg/lifecycle"
	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/sirupsen/logrus"
)

type Task struct {
	metricsSrv     *http.Server
	tracerShutdown func(context.Context) error
}

func New() *Task {
	return new(Task)
}

// Start brings task up and returns the way to stop it. It does not block.
func (task *Task) Start() (lifecycle.Stopper, error) {
	//read config
	taskConfig := config.Conf.Task
	runtime.GOMAXPROCS(taskConfig.TaskBase.CpuNum)

	// Initialize tracer. Its shutdown belongs in the stopper, not in a defer:
	// Start returns while the process keeps running, so a defer here shut the
	// tracer down a few milliseconds after startup.
	tracingCfg := tracing.Config{
		Enabled:      config.Conf.Common.CommonTracing.Enabled,
		Endpoint:     config.Conf.Common.CommonTracing.Endpoint,
		SamplingRate: config.Conf.Common.CommonTracing.SamplingRate,
	}
	shutdown, err := tracing.InitTracer("task", tracingCfg)
	if err != nil {
		logrus.Errorf("Failed to initialize tracer: %v", err)
	} else {
		task.tracerShutdown = shutdown
	}

	//declare what this process must be able to do before it is ready, before the
	//listener starts: an empty registry reports ready
	registerHealthChecks()

	//init metrics server
	task.metricsSrv = metrics.StartMetricsServer(9094)

	//init RabbitMQ consumer
	if err := task.InitRabbitMQConsumer(); err != nil {
		return nil, fmt.Errorf("task init RabbitMQ consumer: %w", err)
	}
	//rpc call connect layer send msg
	if err := task.InitConnectRpcClient(); err != nil {
		return nil, fmt.Errorf("task init InitConnectRpcClient: %w", err)
	}
	//GoPush
	task.GoPush()

	return task.Stop, nil
}

// Stop takes task out of service. It has no inbound connections of its own, so
// there is nothing to drain beyond marking itself unready and closing what it
// started.
func (task *Task) Stop(ctx context.Context) error {
	metrics.SetDraining()

	var firstErr error
	if task.metricsSrv != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := metrics.ShutdownMetricsServer(stopCtx, task.metricsSrv); err != nil {
			logrus.Warnf("task metrics server shutdown: %v", err)
			firstErr = err
		}
		cancel()
	}
	if task.tracerShutdown != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := task.tracerShutdown(stopCtx); err != nil {
			logrus.Warnf("task tracer shutdown: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		cancel()
	}
	return firstErr
}
