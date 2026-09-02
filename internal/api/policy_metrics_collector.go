package api

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// policyMetricsCollector keeps the four policy freshness series coherent at
// scrape time. The mutable metric handles on Metrics are retained as a small
// testing/telemetry adapter, while this collector is the sole registered owner
// of the production metric names.
type policyMetricsCollector struct {
	metrics             *Metrics
	loadedTimestampDesc *prometheus.Desc
	ageSecondsDesc      *prometheus.Desc
	refreshFailuresDesc *prometheus.Desc
	degradedDesc        *prometheus.Desc
}

func newPolicyMetricsCollector(metrics *Metrics) *policyMetricsCollector {
	return &policyMetricsCollector{
		metrics:             metrics,
		loadedTimestampDesc: prometheus.NewDesc("depsilo_policy_snapshot_loaded_timestamp", "Unix timestamp of the last successfully loaded package-policy snapshot.", nil, nil),
		ageSecondsDesc:      prometheus.NewDesc("depsilo_policy_snapshot_age_seconds", "Age in seconds of the last successfully loaded package-policy snapshot.", nil, nil),
		refreshFailuresDesc: prometheus.NewDesc("depsilo_policy_refresh_failures_total", "Total package-policy snapshot refresh failures.", nil, nil),
		degradedDesc:        prometheus.NewDesc("depsilo_policy_degraded", "Whether package-policy evaluation is currently degraded (1) or healthy (0).", nil, nil),
	}
}

func (collector *policyMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	if collector == nil {
		return
	}
	ch <- collector.loadedTimestampDesc
	ch <- collector.ageSecondsDesc
	ch <- collector.refreshFailuresDesc
	ch <- collector.degradedDesc
}

func (collector *policyMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	if collector == nil || collector.metrics == nil {
		return
	}
	status := collector.metrics.policyStatusSnapshot()
	loaded := float64(0)
	if loadedAt := policySnapshotLoadedAt(status); !loadedAt.IsZero() {
		loaded = float64(loadedAt.UnixNano()) / float64(time.Second)
	}
	age := policySnapshotAge(status, time.Now())
	degraded := float64(0)
	if status.Degraded {
		degraded = 1
	}
	ch <- prometheus.MustNewConstMetric(collector.loadedTimestampDesc, prometheus.GaugeValue, loaded)
	ch <- prometheus.MustNewConstMetric(collector.ageSecondsDesc, prometheus.GaugeValue, age)
	ch <- prometheus.MustNewConstMetric(collector.refreshFailuresDesc, prometheus.CounterValue, float64(status.RefreshFailures))
	ch <- prometheus.MustNewConstMetric(collector.degradedDesc, prometheus.GaugeValue, degraded)
}
