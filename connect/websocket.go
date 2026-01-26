/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 15:19
 */
package connect

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"gochat/config"
)

const maxConnections = 10000

var activeConnections int64

// bufferPool implements websocket.BufferPool using sync.Pool
// to reuse buffers across connections.
type bufferPool struct {
	pool sync.Pool
}

func (p *bufferPool) Get() interface{} {
	return p.pool.Get()
}

func (p *bufferPool) Put(x interface{}) {
	p.pool.Put(x)
}

var writeBufferPool = &bufferPool{}

var sharedUpgrader websocket.Upgrader

func initUpgrader(server *Server) {
	sharedUpgrader = websocket.Upgrader{
		ReadBufferSize:  server.Options.ReadBufferSize,
		WriteBufferSize: server.Options.WriteBufferSize,
		WriteBufferPool: writeBufferPool,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}
}

func (c *Connect) InitWebsocket() error {
	initUpgrader(DefaultServer)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c.serveWs(DefaultServer, w, r)
	})

	srv := &http.Server{
		Addr:              config.Conf.Connect.ConnectWebsocket.Bind,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    4096,
	}

	err := srv.ListenAndServe()
	return err
}

func (c *Connect) serveWs(server *Server, w http.ResponseWriter, r *http.Request) {
	if atomic.LoadInt64(&activeConnections) >= maxConnections {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	conn, err := sharedUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logrus.Errorf("serverWs err:%s", err.Error())
		return
	}
	atomic.AddInt64(&activeConnections, 1)
	ch := NewChannel(server.Options.BroadcastSize)
	ch.conn = conn
	go server.writePump(ch, c)
	go server.readPump(ch, c)
}
