/**
 * Created by lock
 * Date: 2019-08-12
 * Time: 11:17
 */
package api

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gochat/api/router"
	"gochat/api/rpc"
	"gochat/config"
	"gochat/pkg/tracing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type Chat struct {
}

func New() *Chat {
	return &Chat{}
}

// api server,Also, you can use gin,echo ... framework wrap
func (c *Chat) Run() {
	// Initialize tracer
	tracingCfg := tracing.Config{
		Enabled:      config.Conf.Common.CommonTracing.Enabled,
		Endpoint:     config.Conf.Common.CommonTracing.Endpoint,
		SamplingRate: config.Conf.Common.CommonTracing.SamplingRate,
	}
	shutdown, err := tracing.InitTracer("api", tracingCfg)
	if err != nil {
		logrus.Errorf("Failed to initialize tracer: %v", err)
	} else {
		defer func() {
			if err := shutdown(context.Background()); err != nil {
				logrus.Errorf("Failed to shutdown tracer: %v", err)
			}
		}()
	}

	//init rpc client
	rpc.InitLogicRpcClient()

	r := router.Register()
	runMode := config.GetGinRunMode()
	logrus.Info("server start , now run mode is ", runMode)
	gin.SetMode(runMode)
	apiConfig := config.Conf.Api
	port := apiConfig.ApiBase.ListenPort
	flag.Parse()

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("start listen : %s\n", err)
		}
	}()
	// if have two quit signal , this signal will priority capture ,also can graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	<-quit
	logrus.Infof("Shutdown Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logrus.Errorf("Server Shutdown: %v", err)
	}
	logrus.Infof("Server exiting")
	os.Exit(0)
}
