package api

import (
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for Depsilo.
type Metrics struct {
	RequestsTotal         *prometheus.CounterVec
	RequestDuration       *prometheus.HistogramVec
	UpstreamRequestsTotal *prometheus.CounterVec
	CacheSizeBytes        prometheus.Gauge
	CacheFilesTotal       prometheus.Gauge
	CompileCacheRequests  *prometheus.CounterVec
	CompileCacheBytes     *prometheus.CounterVec
	CompileCacheDuration  *prometheus.HistogramVec
	CompileCacheSizeBytes prometheus.Gauge
	CompileCacheEntries   prometheus.Gauge
	CompileCacheEvictions *prometheus.CounterVec
}

// M is the package-level Metrics instance, registered on init.
var M *Metrics

func init() {
	M = &Metrics{
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "depsilo_requests_total",
				Help: "Total number of proxy requests.",
			},
			[]string{"adapter_type", "hit"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "depsilo_request_duration_seconds",
				Help:    "Histogram of request latencies in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"adapter_type"},
		),
		UpstreamRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "depsilo_upstream_requests_total",
				Help: "Total number of upstream fetch requests.",
			},
			[]string{"upstream", "success"},
		),
		CacheSizeBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_cache_size_bytes",
				Help: "Current cache storage usage in bytes.",
			},
		),
		CacheFilesTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_cache_files_total",
				Help: "Number of cached files.",
			},
		),
		CompileCacheRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "depsilo_compile_cache_requests_total",
				Help: "Total number of compiler-cache protocol operations.",
			},
			[]string{"protocol", "operation", "result"},
		),
		CompileCacheBytes: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "depsilo_compile_cache_bytes_total",
				Help: "Total bytes stored or served by the compiler cache.",
			},
			[]string{"protocol", "direction"},
		),
		CompileCacheDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "depsilo_compile_cache_operation_duration_seconds",
				Help:    "Compiler-cache operation latency in seconds.",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"protocol", "operation"},
		),
		CompileCacheSizeBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_compile_cache_size_bytes",
				Help: "Current compiler-cache storage usage in bytes.",
			},
		),
		CompileCacheEntries: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_compile_cache_entries",
				Help: "Current number of compiler-cache entries.",
			},
		),
		CompileCacheEvictions: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "depsilo_compile_cache_evictions_total",
				Help: "Total compiler-cache entries evicted by reason.",
			},
			[]string{"reason"},
		),
	}

	prometheus.MustRegister(
		M.RequestsTotal,
		M.RequestDuration,
		M.UpstreamRequestsTotal,
		M.CacheSizeBytes,
		M.CacheFilesTotal,
		M.CompileCacheRequests,
		M.CompileCacheBytes,
		M.CompileCacheDuration,
		M.CompileCacheSizeBytes,
		M.CompileCacheEntries,
		M.CompileCacheEvictions,
	)
}

// MetricsHandler returns a gin.HandlerFunc that serves Prometheus metrics.
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
