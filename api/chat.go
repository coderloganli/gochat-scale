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
	"net"
	"net/http"
	"time"

	"gochat/api/router"
	"gochat/api/rpc"
	"gochat/config"
	"gochat/pkg/lifecycle"
	"gochat/pkg/metrics"
	"gochat/pkg/tracing"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

type Chat struct {
	srv            *http.Server
	tracerShutdown func(context.Context) error
}

func New() *Chat {
	return &Chat{}
}

// Start brings the api up and returns the way to stop it. It does not block,
// and it does not install a signal handler of its own: main.go owns the signal
// and pkg/lifecycle owns the budget.
func (c *Chat) Start() (lifecycle.Stopper, error) {
	// Initialize tracer; its shutdown goes into the stopper.
	tracingCfg := tracing.Config{
		Enabled:      config.Conf.Common.CommonTracing.Enabled,
		Endpoint:     config.Conf.Common.CommonTracing.Endpoint,
		SamplingRate: config.Conf.Common.CommonTracing.SamplingRate,
	}
	shutdown, err := tracing.InitTracer("api", tracingCfg)
	if err != nil {
		logrus.Errorf("Failed to initialize tracer: %v", err)
	} else {
		c.tracerShutdown = shutdown
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

	c.srv = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: r,
	}

	// Bind before returning, so a port already in use is a startup error rather
	// than something the process discovers later.
	ln, err := net.Listen("tcp", c.srv.Addr)
	if err != nil {
		return nil, fmt.Errorf("api listen: %w", err)
	}
	go func() {
		if err := c.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("start listen : %s\n", err)
		}
	}()

	return c.Stop, nil
}

// Stop refuses new requests and lets the ones in flight finish, within the
// caller's budget.
func (c *Chat) Stop(ctx context.Context) error {
	metrics.SetDraining()

	var firstErr error
	if c.srv != nil {
		if err := c.srv.Shutdown(ctx); err != nil {
			logrus.Errorf("Server Shutdown: %v", err)
			firstErr = err
		}
	}
	if c.tracerShutdown != nil {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		if err := c.tracerShutdown(stopCtx); err != nil {
			logrus.Warnf("api tracer shutdown: %v", err)
			if firstErr == nil {
				firstErr = err
			}
		}
		cancel()
	}
	return firstErr
}
