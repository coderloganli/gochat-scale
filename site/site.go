/**
 * Created by lock
 * Date: 2019-08-12
 * Time: 11:36
 */
package site

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
	"gochat/config"
	"net/http"
	"os"
	"path"
)

type Site struct {
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

func (s *Site) Run() {
	siteConfig := config.Conf.Site
	port := siteConfig.SiteBase.ListenPort
	addr := fmt.Sprintf(":%d", port)

	// Create a mux to handle both static files and metrics
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", server(http.Dir("./site")))

	logrus.Infof("Site server starting on %s", addr)
	logrus.Fatal(http.ListenAndServe(addr, mux))
}
