package api

import (
	"testing"
	"time"

	"depsilo/internal/traffic"
	"github.com/prometheus/client_golang/prometheus"
)

func TestTrafficMetricsExposeProcessTotalsAndZeroRoutes(t *testing.T) {
	metrics := &Metrics{}
	meter := traffic.NewMeter()
	metrics.BindTrafficMeter(meter)
	registry := prometheus.NewRegistry()
	registry.MustRegister(newTrafficMetricsCollector(metrics))
	meter.ObserveUpstreamExchange(traffic.RouteDirect, false)
	meter.ObserveUpstreamBytes(traffic.RouteDirect, 42, false)
	meter.ObserveUpstreamExchange(traffic.RouteDirect, true)
	meter.ObserveUpstreamBytes(traffic.RouteDirect, 6, true)
	meter.ObserveServiceRequest(84)
	// Scraping and expiry of the rate window must not reset counters.
	_ = meter.Snapshot(60, time.Now().Add(time.Hour))
	expected := map[string]float64{
		"depsilo_upstream_response_bytes_total/direct":          48,
		"depsilo_upstream_response_bytes_total/peer":            0,
		"depsilo_upstream_exchanges_total/direct":               2,
		"depsilo_upstream_exchanges_total/peer":                 0,
		"depsilo_service_response_bytes_total/":                 84,
		"depsilo_upstream_redirect_response_bytes_total/direct": 6,
		"depsilo_upstream_redirect_response_bytes_total/peer":   0,
		"depsilo_upstream_redirect_exchanges_total/direct":      1,
		"depsilo_upstream_redirect_exchanges_total/peer":        0,
	}
	for range 2 {
		families, err := registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, family := range families {
			for _, metric := range family.Metric {
				if metric.Counter == nil {
					t.Fatalf("%s is not a counter", family.GetName())
				}
				route := ""
				for _, label := range metric.Label {
					if label.GetName() != "route" {
						t.Fatalf("unexpected label: %v", label)
					}
					route = label.GetValue()
				}
				key := family.GetName() + "/" + route
				want, ok := expected[key]
				if !ok || metric.GetCounter().GetValue() != want {
					t.Fatalf("%s = %v, want %v (known=%v)", key, metric.GetCounter().GetValue(), want, ok)
				}
				count++
			}
		}
		if count != len(expected) {
			t.Fatalf("series count = %d, want %d", count, len(expected))
		}
	}
	metrics.BindTrafficMeter(nil)
	if got, err := registry.Gather(); err != nil || len(got) != 0 {
		t.Fatalf("unbound meter published observations: %v, %v", got, err)
	}
}
