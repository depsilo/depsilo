package api

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for Depslio.
type Metrics struct {
	RequestsTotal         *prometheus.CounterVec
	RequestDuration       *prometheus.HistogramVec
	UpstreamRequestsTotal *prometheus.CounterVec
	CacheSizeBytes        prometheus.Gauge
	CacheFilesTotal       prometheus.Gauge
}

// M is the package-level Metrics instance, registered on init.
var M *Metrics

func init() {
	M = &Metrics{
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "depslio_requests_total",
				Help: "Total number of proxy requests.",
			},
			[]string{"adapter_type", "hit"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "depslio_request_duration_seconds",
				Help:    "Histogram of request latencies in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"adapter_type"},
		),
		UpstreamRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "depslio_upstream_requests_total",
				Help: "Total number of upstream fetch requests.",
			},
			[]string{"upstream", "success"},
		),
		CacheSizeBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depslio_cache_size_bytes",
				Help: "Current cache storage usage in bytes.",
			},
		),
		CacheFilesTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depslio_cache_files_total",
				Help: "Number of cached files.",
			},
		),
	}

	prometheus.MustRegister(
		M.RequestsTotal,
		M.RequestDuration,
		M.UpstreamRequestsTotal,
		M.CacheSizeBytes,
		M.CacheFilesTotal,
	)
}

// MetricsHandler returns a gin.HandlerFunc that serves Prometheus metrics.
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
