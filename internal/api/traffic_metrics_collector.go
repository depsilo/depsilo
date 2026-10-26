package api

import (
	"depsilo/internal/traffic"
	"github.com/prometheus/client_golang/prometheus"
)

// trafficMetricsCollector exposes bounded process counters from the same
// meter as Overview. Route labels describe the configured transport, not a
// public/private network classification; a direct upstream can be a LAN mirror.
type trafficMetricsCollector struct {
	metrics          *Metrics
	upstreamBytes    *prometheus.Desc
	upstreamRequests *prometheus.Desc
	redirectBytes    *prometheus.Desc
	redirectRequests *prometheus.Desc
	serviceBytes     *prometheus.Desc
}

func newTrafficMetricsCollector(metrics *Metrics) *trafficMetricsCollector {
	return &trafficMetricsCollector{
		metrics: metrics,
		upstreamBytes: prometheus.NewDesc(
			"depsilo_upstream_response_bytes_total",
			"Successful upstream GET response body bytes actually read, by direct or cascade peer transport; excludes HTTP errors, probes, and protocol overhead. Direct does not imply public WAN.",
			[]string{"route"}, nil,
		),
		upstreamRequests: prometheus.NewDesc(
			"depsilo_upstream_exchanges_total",
			"Upstream GET responses with status below 400, by direct or cascade peer transport; excludes probes and automatically followed intermediate redirects.",
			[]string{"route"}, nil,
		),
		redirectBytes: prometheus.NewDesc(
			"depsilo_upstream_redirect_response_bytes_total",
			"Subset of upstream response body bytes read from observed 3xx GET responses other than 304; included in upstream_response_bytes_total.",
			[]string{"route"}, nil,
		),
		redirectRequests: prometheus.NewDesc(
			"depsilo_upstream_redirect_exchanges_total",
			"Subset of observed upstream GET responses with 3xx status other than 304; included in upstream_exchanges_total. Automatically followed intermediate redirects are excluded.",
			[]string{"route"}, nil,
		),
		serviceBytes: prometheus.NewDesc(
			"depsilo_service_response_bytes_total",
			"Response body bytes delivered by native dependency-client handlers; excludes cascade relay responses and protocol overhead.",
			nil, nil,
		),
	}
}

func (c *trafficMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.upstreamBytes
	ch <- c.upstreamRequests
	ch <- c.redirectBytes
	ch <- c.redirectRequests
	ch <- c.serviceBytes
}

func (c *trafficMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	if c.metrics == nil {
		return
	}
	meter := c.metrics.trafficMeter.Load()
	if meter == nil {
		return
	}
	totals := meter.Totals()
	for _, item := range []struct {
		route  traffic.Route
		totals traffic.UpstreamTotals
	}{
		{traffic.RouteDirect, totals.Direct},
		{traffic.RoutePeer, totals.Peer},
	} {
		ch <- prometheus.MustNewConstMetric(c.upstreamBytes, prometheus.CounterValue, float64(item.totals.Bytes), string(item.route))
		ch <- prometheus.MustNewConstMetric(c.upstreamRequests, prometheus.CounterValue, float64(item.totals.Exchanges), string(item.route))
		ch <- prometheus.MustNewConstMetric(c.redirectBytes, prometheus.CounterValue, float64(item.totals.RedirectBytes), string(item.route))
		ch <- prometheus.MustNewConstMetric(c.redirectRequests, prometheus.CounterValue, float64(item.totals.RedirectExchanges), string(item.route))
	}
	ch <- prometheus.MustNewConstMetric(c.serviceBytes, prometheus.CounterValue, float64(totals.ServiceBytes))
}
