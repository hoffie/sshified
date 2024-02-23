package main

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
)

var (
	metricPayloadBytes = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "sshified_response_payload_bytes",
			Help: "Total of all payload data transferred",
		},
	)
	metricErrorsByType = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sshified_connection_errors_total",
			Help: "Total of all error occurences by type",
		},
		[]string{"type"},
	)
	metricSshclientPool = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "sshified_sshclient_pool_total",
			Help: "Number of cached ssh connections",
		},
	)
	metricSshKeepaliveFailuresTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "sshified_ssh_keepalive_failures_total",
			Help: "Total of all SSH keepalive failures (aborts, reconnects)",
		},
	)
	metricRequestDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "sshified_request_duration_seconds",
			Help:    "Histogram for all proxy requests",
			Buckets: []float64{0.01, 0.1, 0.5, 1.0, 2.0, 5.0},
		},
	)
	metricRequestsTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "sshified_requests_total",
			Help: "Total of all requests",
		},
	)
	metricRequestsFailedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "sshified_requests_failed_total",
			Help: "Total of failed requests",
		},
	)
)

func init() {
	prometheus.MustRegister(metricPayloadBytes)
	prometheus.MustRegister(metricSshclientPool)
	prometheus.MustRegister(metricSshKeepaliveFailuresTotal)
	prometheus.MustRegister(metricRequestDuration)
	prometheus.MustRegister(metricRequestsTotal)
	prometheus.MustRegister(metricRequestsFailedTotal)
	prometheus.MustRegister(metricErrorsByType)
}

func setupMetrics(addr string) {
	if addr == "" {
		return
	}
	log.WithFields(log.Fields{"addr": addr}).Info("Serving metrics")
	s := &http.Server{
		Addr:           addr,
		Handler:        promhttp.Handler(),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	go func() {
		log.Fatal(s.ListenAndServe())
	}()
}
