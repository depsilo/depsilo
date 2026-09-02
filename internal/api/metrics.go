package api

import (
	"strconv"
	"sync"
	"time"

	"depsilo/internal/adapter"
	"depsilo/internal/rules"
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

	// Policy freshness metrics are exposed as ordinary metric handles for
	// callers that want to observe a snapshot explicitly. The registry uses a
	// small collector (below) so snapshot age is recomputed at scrape time
	// instead of freezing at the last refresh event.
	PolicySnapshotLoadedTimestamp prometheus.Gauge
	PolicySnapshotAgeSeconds      prometheus.Gauge
	PolicyRefreshFailures         prometheus.Counter
	PolicyDegraded                prometheus.Gauge

	policyMu           sync.RWMutex
	policyProvider     PolicyStatusProvider
	policyStatus       rules.PolicyStatus
	policyCounterValue uint64
	policyCollector    *policyMetricsCollector
}

var _ rules.PolicyTelemetry = (*Metrics)(nil)

// ObserveAccess implements adapter.RequestObserver at the telemetry seam.
// Every protocol adapter already reports its completed requests through
// adapter.LogAccess, so metrics stay consistent without protocol-specific
// Prometheus calls.
func (m *Metrics) ObserveAccess(observation adapter.AccessObservation) {
	if m == nil {
		return
	}
	m.RequestsTotal.WithLabelValues(observation.AdapterType, strconv.FormatBool(observation.Hit)).Inc()
	m.RequestDuration.WithLabelValues(observation.AdapterType).Observe(observation.Latency.Seconds())
	if !observation.Hit && observation.Upstream != "" {
		m.UpstreamRequestsTotal.WithLabelValues(
			observation.Upstream,
			strconv.FormatBool(observation.StatusCode < 500),
		).Inc()
	}
}

// BindPolicyStatusProvider attaches the process-owned policy status source to
// the telemetry collector. It is intentionally a read-only provider seam: a
// Prometheus scrape, readiness probe, or Admin status request must not trigger
// a database refresh or mutate policy state.
func (m *Metrics) BindPolicyStatusProvider(provider PolicyStatusProvider) {
	if m == nil {
		return
	}
	m.policyMu.Lock()
	m.policyProvider = provider
	m.policyMu.Unlock()
	if provider != nil {
		m.ObservePolicyStatus(provider.PolicyStatus())
	}
}

// ObservePolicyStatus records the latest policy snapshot for telemetry. The
// method is useful for tests and for alternate runtime adapters; production
// servers normally bind a provider with BindPolicyStatusProvider so gauges are
// refreshed automatically when metrics are scraped.
func (m *Metrics) ObservePolicyStatus(status rules.PolicyStatus) {
	if m == nil {
		return
	}
	m.policyMu.Lock()
	previousCounter := m.policyCounterValue
	if status.RefreshFailures < previousCounter {
		// A provider should be monotonic for the lifetime of a process. Keep
		// the local adapter monotonic even if a test or replacement provider is
		// reset underneath it.
		status.RefreshFailures = previousCounter
	}
	m.policyStatus = status
	m.policyCounterValue = status.RefreshFailures
	currentCounter := m.policyCounterValue
	m.policyMu.Unlock()
	m.setPolicyMetricHandles(status, previousCounter, currentCounter)
}

func (m *Metrics) setPolicyMetricHandles(status rules.PolicyStatus, previousCounter, currentCounter uint64) {
	if m.PolicySnapshotLoadedTimestamp != nil {
		loaded := float64(0)
		if loadedAt := policySnapshotLoadedAt(status); !loadedAt.IsZero() {
			loaded = float64(loadedAt.UnixNano()) / float64(time.Second)
		}
		m.PolicySnapshotLoadedTimestamp.Set(loaded)
	}
	if m.PolicySnapshotAgeSeconds != nil {
		m.PolicySnapshotAgeSeconds.Set(policySnapshotAge(status, time.Now()))
	}
	if m.PolicyRefreshFailures != nil {
		// Counters cannot be decremented. A status source is expected to be
		// monotonic for the lifetime of a process; only advance the handle.
		if currentCounter > previousCounter {
			m.PolicyRefreshFailures.Add(float64(currentCounter - previousCounter))
		}
	}
	if m.PolicyDegraded != nil {
		if status.Degraded {
			m.PolicyDegraded.Set(1)
		} else {
			m.PolicyDegraded.Set(0)
		}
	}
}

// mutatePolicyStatus applies one telemetry transition while holding the same
// lock used by status readers. This avoids losing a refresh-failure increment
// when a status probe races a refresh callback.
func (m *Metrics) mutatePolicyStatus(update func(*rules.PolicyStatus)) {
	if m == nil || update == nil {
		return
	}
	m.policyMu.Lock()
	status := m.policyStatus
	previousCounter := m.policyCounterValue
	update(&status)
	if status.RefreshFailures < previousCounter {
		status.RefreshFailures = previousCounter
	}
	m.policyStatus = status
	m.policyCounterValue = status.RefreshFailures
	currentCounter := m.policyCounterValue
	m.policyMu.Unlock()
	m.setPolicyMetricHandles(status, previousCounter, currentCounter)
}

// PolicySnapshotLoaded implements rules.PolicyTelemetry. Keeping these
// callbacks on the metrics adapter lets the rules module report transitions
// without importing the API package (and without making telemetry a request
// path dependency).
func (m *Metrics) PolicySnapshotLoaded(at time.Time) {
	if m == nil {
		return
	}
	m.mutatePolicyStatus(func(status *rules.PolicyStatus) {
		status.LastSuccessfulRefresh = at
		status.SnapshotLoadedAt = at
		status.Status = "healthy"
		status.Degraded = false
		status.UsingStaleSnapshot = false
		status.SnapshotAgeSeconds = 0
	})
}

// PolicyRefreshFailed implements rules.PolicyTelemetry. The cumulative
// counter is advanced by the engine's event rather than inferred from an HTTP
// request, so failures are counted once per refresh attempt.
func (m *Metrics) PolicyRefreshFailed() {
	if m == nil {
		return
	}
	m.mutatePolicyStatus(func(status *rules.PolicyStatus) {
		status.RefreshFailures++
		status.Degraded = true
		if policySnapshotLoadedAt(*status).IsZero() {
			status.Status = "unavailable"
		} else {
			status.Status = "degraded"
		}
	})
}

// PolicyState implements rules.PolicyTelemetry and records the state selected
// by the engine after a refresh attempt (fresh, stale, or no snapshot).
func (m *Metrics) PolicyState(degraded bool, usingStaleSnapshot bool, snapshotAgeSeconds float64) {
	if m == nil {
		return
	}
	m.mutatePolicyStatus(func(status *rules.PolicyStatus) {
		status.Degraded = degraded
		status.UsingStaleSnapshot = usingStaleSnapshot
		status.SnapshotAgeSeconds = snapshotAgeSeconds
		if policySnapshotLoadedAt(*status).IsZero() {
			status.Status = "unavailable"
		} else if degraded {
			status.Status = "degraded"
		} else {
			status.Status = "healthy"
		}
	})
}

// policyStatusSnapshot returns the provider's current value, falling back to
// the last explicitly observed value when no provider is bound.
func (m *Metrics) policyStatusSnapshot() rules.PolicyStatus {
	if m == nil {
		return rules.PolicyStatus{}
	}
	m.policyMu.RLock()
	provider := m.policyProvider
	status := m.policyStatus
	m.policyMu.RUnlock()
	if provider != nil {
		status = provider.PolicyStatus()
		// Prometheus counters are process-monotonic. A provider can be
		// replaced during an in-process server restart (or by a test), in
		// which case its engine-local failure count may start at zero. Preserve
		// the highest count observed by this Metrics instance rather than
		// exporting a misleading counter reset.
		m.policyMu.RLock()
		counter := m.policyCounterValue
		m.policyMu.RUnlock()
		if status.RefreshFailures < counter {
			status.RefreshFailures = counter
		}
	}
	return status
}

func policySnapshotAge(status rules.PolicyStatus, now time.Time) float64 {
	loadedAt := policySnapshotLoadedAt(status)
	if loadedAt.IsZero() {
		return 0
	}
	age := now.Sub(loadedAt).Seconds()
	if age < 0 {
		return 0
	}
	return age
}

// policySnapshotLoadedAt accepts both field names used by the status seam.
// LastSuccessfulRefresh is the engine's canonical field; SnapshotLoadedAt is
// retained as a wire/metric terminology alias for alternate providers.
func policySnapshotLoadedAt(status rules.PolicyStatus) time.Time {
	if !status.LastSuccessfulRefresh.IsZero() {
		return status.LastSuccessfulRefresh
	}
	return status.SnapshotLoadedAt
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
				Help: "Total bytes tracked by the durable package-cache inventory.",
			},
		),
		CacheFilesTotal: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_cache_files_total",
				Help: "Number of objects tracked by the durable package-cache inventory.",
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
		PolicySnapshotLoadedTimestamp: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_policy_snapshot_loaded_timestamp",
				Help: "Unix timestamp of the last successfully loaded package-policy snapshot.",
			},
		),
		PolicySnapshotAgeSeconds: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_policy_snapshot_age_seconds",
				Help: "Age in seconds of the last successfully loaded package-policy snapshot.",
			},
		),
		PolicyRefreshFailures: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "depsilo_policy_refresh_failures_total",
				Help: "Total package-policy snapshot refresh failures.",
			},
		),
		PolicyDegraded: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "depsilo_policy_degraded",
				Help: "Whether package-policy evaluation is currently degraded (1) or healthy (0).",
			},
		),
	}
	M.policyCollector = newPolicyMetricsCollector(M)

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
		M.policyCollector,
	)
}

// MetricsHandler returns a gin.HandlerFunc that serves Prometheus metrics.
func MetricsHandler() gin.HandlerFunc {
	h := promhttp.Handler()
	return func(c *gin.Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}
