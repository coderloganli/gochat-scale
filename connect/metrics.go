package connect

import (
	"gochat/pkg/metrics"
)

// Connection accounting for the gauge the autoscaler reads.
//
// gochat_connections_active is the signal connect-ws scales on (docs/adr/0012),
// because a connection-holding service exhausts connection capacity long before
// it exhausts CPU. The gauge existed in pkg/metrics from the start but had no
// writer anywhere in the repository, so the series was never exported at all -
// these are the writers.
//
// The atomic in websocket.go is deliberately kept alongside this: it backs the
// maxConnections admission check, and an admission decision must not depend on
// whether a metrics registry happens to be working.

const (
	serviceWebsocket = "connect-ws"
	serviceTcp       = "connect-tcp"

	connTypeWebsocket = "websocket"
	connTypeTcp       = "tcp"
)

// initConnectionMetrics creates the series at zero so that it is exported before
// the first client arrives. A GaugeVec with no observed label values exports
// nothing at all, and "no series" and "zero connections" are indistinguishable
// to a scraper - which would leave the HPA reading <unknown> on an idle service.
func initConnectionMetrics(service, connType string) {
	metrics.ConnectionsActive.WithLabelValues(service, connType).Set(0)
}

func connectionOpened(service, connType string) {
	metrics.ConnectionsActive.WithLabelValues(service, connType).Inc()
	metrics.ConnectionsTotal.WithLabelValues(service, connType, "accepted").Inc()
}

func connectionClosed(service, connType string) {
	metrics.ConnectionsActive.WithLabelValues(service, connType).Dec()
}
