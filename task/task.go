/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 18:22
 */
package task

import (
	"github.com/sirupsen/logrus"
	"gochat/config"
	"gochat/pkg/metrics"
	"runtime"
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
