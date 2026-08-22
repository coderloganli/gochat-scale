/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 15:19
 */
package connect

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"
	"gochat/config"
)

const maxConnections = 10000

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

	// Bind before returning, so a port already in use is a startup error rather
	// than something the process discovers later. Serving happens in the
	// background: Start must return for the signal handler to be installed at
	// all, which is why the websocket role never saw SIGTERM before.
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return err
	}

	c.httpSrv = srv
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("websocket server: %v", err)
		}
	}()
	return nil
}

func (c *Connect) serveWs(server *Server, w http.ResponseWriter, r *http.Request) {
	// Checked before the capacity limit: a departing instance refuses new work
	// whether or not it is full, because a drain that keeps taking connections
	// never converges.
	if server.Draining() {
		http.Error(w, "draining", http.StatusServiceUnavailable)
		return
	}
	if server.Registry.Len() >= maxConnections {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}
	conn, err := sharedUpgrader.Upgrade(w, r, nil)
	if err != nil {
		logrus.Errorf("serverWs err:%s", err.Error())
		return
	}
	ch := NewChannel(server.Options.BroadcastSize)
	ch.conn = conn
	// Registered before the read loop starts, so that a connection which has not
	// authenticated yet — and therefore belongs to no bucket — is still
	// something shutdown can find and close.
	server.Registry.Add(ch)
	go server.writePump(ch, c)
	go server.readPump(ch, c)
}
