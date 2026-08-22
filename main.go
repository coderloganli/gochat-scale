/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 10:56
 */
package main

import (
	"flag"
	"fmt"
	"os"

	"gochat/api"
	"gochat/connect"
	"gochat/logic"
	"gochat/pkg/lifecycle"
	"gochat/pkg/logging"
	"gochat/site"
	"gochat/task"

	"github.com/sirupsen/logrus"
)

// start dispatches to the module's entry point. Every one of them starts its
// work and hands back a way to stop it; none of them block, and none of them
// handle signals. That uniformity is the point: whether a module reacts to
// SIGTERM used to depend on whether its Run happened to have returned yet.
func start(module string) (lifecycle.Stopper, error) {
	switch module {
	case "logic":
		return logic.New().Start()
	case "connect_websocket":
		return connect.New().Start()
	case "connect_tcp":
		return connect.New().StartTcp()
	case "task":
		return task.New().Start()
	case "api":
		return api.New().Start()
	case "site":
		return site.New().Start()
	default:
		return nil, fmt.Errorf("unknown module %q", module)
	}
}

func main() {
	var module string
	flag.StringVar(&module, "module", "", "assign run module")
	flag.Parse()
	logging.InitFromEnv()

	fmt.Println(fmt.Sprintf("start run %s module", module))

	stop, err := start(module)
	if err != nil {
		logrus.Errorf("cannot start %s: %v", module, err)
		os.Exit(1)
	}
	fmt.Println(fmt.Sprintf("run %s module done!", module))

	lifecycle.WaitAndStop(module, stop)
	fmt.Println("Server exiting")
}
