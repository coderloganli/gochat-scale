/**
 * Created by lock
 * Date: 2019-08-12
 * Time: 11:36
 */
package site

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"gochat/config"
	"gochat/pkg/lifecycle"
	"gochat/pkg/metrics"
)

type Site struct {
	srv *http.Server
}

func New() *Site {
	return &Site{}
}

func notFound(w http.ResponseWriter, r *http.Request) {
	// Here you can send your custom 404 back.
	data, _ := os.ReadFile("./site/index.html")
	_, _ = fmt.Fprintf(w, string(data))
	return
}

func server(fs http.FileSystem) http.Handler {
	fileServer := http.FileServer(fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filePath := path.Clean("./site" + r.URL.Path)
		_, err := os.Stat(filePath)
		if err != nil {
			notFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}

// Start serves the frontend and returns the way to stop it. It does not block.
func (s *Site) Start() (lifecycle.Stopper, error) {
	siteConfig := config.Conf.Site
	port := siteConfig.SiteBase.ListenPort
	addr := fmt.Sprintf(":%d", port)

	// Create a mux to handle both static files and metrics
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", server(http.Dir("./site")))

	s.srv = &http.Server{Addr: addr, Handler: mux}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("site listen: %w", err)
	}

	logrus.Infof("Site server starting on %s", addr)
	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("site server: %v", err)
		}
	}()

	return s.Stop, nil
}

// Stop refuses new requests and lets the ones in flight finish.
func (s *Site) Stop(ctx context.Context) error {
	metrics.SetDraining()
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}
