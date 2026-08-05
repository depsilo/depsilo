package api

import (
	"testing"
	"time"

	"depsilo/internal/adapter"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestMetricsObserveAccessUsesSharedAdapterOutcome(t *testing.T) {
	metrics := &Metrics{
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "test_requests_total"},
			[]string{"adapter_type", "hit"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{Name: "test_request_duration_seconds"},
			[]string{"adapter_type"},
		),
		UpstreamRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "test_upstream_requests_total"},
			[]string{"upstream", "success"},
		),
	}

	metrics.ObserveAccess(adapter.AccessObservation{
		AdapterType: "npm",
		Method:      "GET",
		Hit:         false,
		Upstream:    "official",
		Latency:     250 * time.Millisecond,
		StatusCode:  200,
		BytesSent:   42,
	})
	metrics.ObserveAccess(adapter.AccessObservation{
		AdapterType: "npm",
		Method:      "GET",
		Hit:         false,
		Upstream:    "official",
		Latency:     50 * time.Millisecond,
		StatusCode:  502,
	})
	metrics.ObserveAccess(adapter.AccessObservation{
		AdapterType: "npm",
		Method:      "GET",
		Hit:         true,
		Upstream:    "official",
		Latency:     10 * time.Millisecond,
		StatusCode:  200,
		BytesSent:   42,
	})

	if got := counterValue(t, metrics.RequestsTotal.WithLabelValues("npm", "false")); got != 2 {
		t.Fatalf("npm miss requests = %v, want 2", got)
	}
	if got := counterValue(t, metrics.RequestsTotal.WithLabelValues("npm", "true")); got != 1 {
		t.Fatalf("npm hit requests = %v, want 1", got)
	}
	if got := counterValue(t, metrics.UpstreamRequestsTotal.WithLabelValues("official", "true")); got != 1 {
		t.Fatalf("successful upstream requests = %v, want only the miss", got)
	}
	if got := counterValue(t, metrics.UpstreamRequestsTotal.WithLabelValues("official", "false")); got != 1 {
		t.Fatalf("failed upstream requests = %v, want 1", got)
	}
}

func counterValue(t *testing.T, metric prometheus.Metric) float64 {
	t.Helper()
	var value dto.Metric
	if err := metric.Write(&value); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return value.GetCounter().GetValue()
}
