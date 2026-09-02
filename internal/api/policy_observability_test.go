package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/rules"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

type policyStatusFixture struct {
	status rules.PolicyStatus
}

func (fixture *policyStatusFixture) PolicyStatus() rules.PolicyStatus {
	return fixture.status
}

func TestReadinessIncludesDegradedPolicyWithoutChangingHTTPReadiness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	storage, err := cache.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}
	loadedAt := time.Now().Add(-12 * time.Minute).UTC()
	provider := &policyStatusFixture{status: rules.PolicyStatus{
		Status:                "degraded",
		Degraded:              true,
		UsingStaleSnapshot:    true,
		LastSuccessfulRefresh: loadedAt,
		SnapshotAgeSeconds:    12 * 60,
		RefreshFailures:       2,
		OnLoadError:           rules.OnLoadErrorUseStaleThenAllow,
	}}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/ready", nil)
	readinessHandler(database, storage, provider)(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Status string `json:"status"`
		Policy struct {
			Status             string `json:"status"`
			UsingStaleSnapshot bool   `json:"using_stale_snapshot"`
			SnapshotLoadedAt   string `json:"snapshot_loaded_at"`
			LastRefresh        string `json:"last_successful_refresh"`
		} `json:"policy"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readiness: %v; body=%s", err, recorder.Body.String())
	}
	if body.Status != "ready" {
		t.Fatalf("readiness status = %q, want ready", body.Status)
	}
	if body.Policy.Status != "degraded" || !body.Policy.UsingStaleSnapshot {
		t.Fatalf("policy status = %#v, want degraded/stale", body.Policy)
	}
	wantLoadedAt := loadedAt.Format(time.RFC3339Nano)
	if body.Policy.SnapshotLoadedAt != wantLoadedAt || body.Policy.LastRefresh != wantLoadedAt {
		t.Fatalf("policy loaded timestamps = snapshot:%q last:%q, want %q", body.Policy.SnapshotLoadedAt, body.Policy.LastRefresh, wantLoadedAt)
	}
}

func TestPolicyMetricsCollectorExportsFreshnessValuesAndDynamicAge(t *testing.T) {
	loadedAt := time.Now().Add(-90 * time.Second)
	provider := &policyStatusFixture{status: rules.PolicyStatus{
		Status:                "degraded",
		Degraded:              true,
		UsingStaleSnapshot:    true,
		LastSuccessfulRefresh: loadedAt,
		RefreshFailures:       7,
		OnLoadError:           rules.OnLoadErrorUseStaleThenDeny,
	}}
	metrics := &Metrics{
		PolicySnapshotLoadedTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_policy_loaded"}),
		PolicySnapshotAgeSeconds:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_policy_age"}),
		PolicyRefreshFailures:         prometheus.NewCounter(prometheus.CounterOpts{Name: "test_policy_failures"}),
		PolicyDegraded:                prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_policy_degraded"}),
	}
	metrics.policyCollector = newPolicyMetricsCollector(metrics)
	metrics.BindPolicyStatusProvider(provider)
	registry := prometheus.NewRegistry()
	if err := registry.Register(metrics.policyCollector); err != nil {
		t.Fatalf("register policy collector: %v", err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather policy metrics: %v", err)
	}
	values := make(map[string]float64, len(families))
	for _, family := range families {
		if len(family.Metric) != 1 {
			t.Fatalf("metric %s has %d samples, want 1", family.GetName(), len(family.Metric))
		}
		metric := family.Metric[0]
		switch family.GetType().String() {
		case "COUNTER":
			values[family.GetName()] = metric.GetCounter().GetValue()
		default:
			values[family.GetName()] = metric.GetGauge().GetValue()
		}
	}
	for _, name := range []string{
		"depsilo_policy_snapshot_loaded_timestamp",
		"depsilo_policy_snapshot_age_seconds",
		"depsilo_policy_refresh_failures_total",
		"depsilo_policy_degraded",
	} {
		if _, ok := values[name]; !ok {
			t.Fatalf("missing policy metric %q; got %v", name, values)
		}
	}
	if got := values["depsilo_policy_snapshot_loaded_timestamp"]; got <= 0 {
		t.Fatalf("loaded timestamp = %v, want positive", got)
	}
	if got := values["depsilo_policy_snapshot_age_seconds"]; got < 89 || got > 100 {
		t.Fatalf("snapshot age = %v, want approximately 90 seconds", got)
	}
	if got := values["depsilo_policy_refresh_failures_total"]; got != 7 {
		t.Fatalf("refresh failures = %v, want 7", got)
	}
	if got := values["depsilo_policy_degraded"]; got != 1 {
		t.Fatalf("degraded = %v, want 1", got)
	}
}

func TestMetricsImplementsPolicyTelemetry(t *testing.T) {
	var _ rules.PolicyTelemetry = (*Metrics)(nil)
	metrics := &Metrics{
		PolicySnapshotLoadedTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_policy_loaded_telemetry"}),
		PolicySnapshotAgeSeconds:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_policy_age_telemetry"}),
		PolicyRefreshFailures:         prometheus.NewCounter(prometheus.CounterOpts{Name: "test_policy_failures_telemetry"}),
		PolicyDegraded:                prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_policy_degraded_telemetry"}),
	}
	loadedAt := time.Now().Add(-time.Second)
	metrics.PolicySnapshotLoaded(loadedAt)
	metrics.PolicyRefreshFailed()
	metrics.PolicyState(true, true, 1)
	status := metrics.policyStatusSnapshot()
	if !status.Degraded || !status.UsingStaleSnapshot || status.RefreshFailures != 1 {
		t.Fatalf("telemetry status = %#v, want degraded/stale/one failure", status)
	}
	if !status.LastSuccessfulRefresh.Equal(loadedAt) {
		t.Fatalf("loaded timestamp = %s, want %s", status.LastSuccessfulRefresh, loadedAt)
	}
}
