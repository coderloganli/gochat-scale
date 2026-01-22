/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 18:22
 */
package task

import (
	"context"
	"runtime"

	"gochat/config"
	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/sirupsen/logrus"
)

type Task struct {
}

func New() *Task {
	return new(Task)
}

func (task *Task) Run() {
	//read config
	taskConfig := config.Conf.Task
	runtime.GOMAXPROCS(taskConfig.TaskBase.CpuNum)

	// Initialize tracer
	tracingCfg := tracing.Config{
		Enabled:      config.Conf.Common.CommonTracing.Enabled,
		Endpoint:     config.Conf.Common.CommonTracing.Endpoint,
		SamplingRate: config.Conf.Common.CommonTracing.SamplingRate,
	}
	shutdown, err := tracing.InitTracer("task", tracingCfg)
	if err != nil {
		logrus.Errorf("Failed to initialize tracer: %v", err)
	} else {
		defer func() {
			if err := shutdown(context.Background()); err != nil {
				logrus.Errorf("Failed to shutdown tracer: %v", err)
			}
		}()
	}

	//init metrics server
	metrics.StartMetricsServer(9094)

	//init RabbitMQ consumer
	if err := task.InitRabbitMQConsumer(); err != nil {
		logrus.Panicf("task init RabbitMQ consumer fail,err:%s", err.Error())
	}
	//rpc call connect layer send msg
	if err := task.InitConnectRpcClient(); err != nil {
		logrus.Panicf("task init InitConnectRpcClient fail,err:%s", err.Error())
	}
	//GoPush
	task.GoPush()
}
