package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

func assertDashboardDatabaseError(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(body) != 2 || body["code"] != "DB_ERROR" || body["message"] != "failed to load dashboard statistics" {
		t.Fatalf("response = %#v, want safe error without partial statistics", body)
	}
}

func registerDashboardReadCallback(t *testing.T, database *gorm.DB, callback func(*gorm.DB)) {
	t.Helper()
	const name = "test:dashboard_read"
	if err := database.Callback().Row().Before("gorm:row").Register(name, callback); err != nil {
		t.Fatalf("register row callback: %v", err)
	}
	if err := database.Callback().Query().Before("gorm:query").Register(name, callback); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Callback().Row().Remove(name)
		_ = database.Callback().Query().Remove(name)
	})
}

func TestDashboardReturnsDatabaseErrorForEveryRequiredQuery(t *testing.T) {
	tests := []struct {
		name       string
		useRollup  bool
		table      string
		occurrence int
	}{
		{name: "last 24 hours", table: "access_logs", occurrence: 1},
		{name: "previous 24 hours", table: "access_logs", occurrence: 2},
		{name: "selected raw window", table: "access_logs", occurrence: 3},
		{name: "previous raw window", table: "access_logs", occurrence: 4},
		{name: "raw daily statistics", table: "access_logs", occurrence: 5},
		{name: "raw top packages", table: "access_logs", occurrence: 6},
		{name: "selected rollup window", useRollup: true, table: "access_log_hourly", occurrence: 1},
		{name: "previous rollup window", useRollup: true, table: "access_log_hourly", occurrence: 2},
		{name: "rollup daily statistics", useRollup: true, table: "access_log_hourly", occurrence: 3},
		{name: "rollup top packages", useRollup: true, table: "access_log_package_daily", occurrence: 1},
		{name: "origin coverage", table: "control_plane_states", occurrence: 1},
		{name: "lifetime usage", table: "control_plane_states", occurrence: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			database, router := newDashboardResponseTestServer(t, tt.useRollup, 0)
			seen := 0
			registerDashboardReadCallback(t, database, func(tx *gorm.DB) {
				if tx.Statement.Table != tt.table {
					return
				}
				seen++
				if seen == tt.occurrence {
					tx.AddError(errors.New("private database details must not reach the response"))
				}
			})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard?range=7d", nil))
			assertDashboardDatabaseError(t, recorder)
			if seen != tt.occurrence {
				t.Fatalf("matching queries = %d, want %d", seen, tt.occurrence)
			}
		})
	}
}

func TestDashboardEmptyStatisticsAndAbsentCoverageRemainSuccessful(t *testing.T) {
	for _, useRollup := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollup=%t", useRollup), func(t *testing.T) {
			_, router := newDashboardResponseTestServer(t, useRollup, 0)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard?range=7d", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var body dashboardOverviewBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode dashboard: %v", err)
			}
			if body.Window.TotalRequests != 0 || body.OriginCoverage.Measured || body.OriginCoverage.Since != nil {
				t.Fatalf("empty dashboard = %+v, want measured zero requests and no origin coverage", body)
			}
		})
	}
}

func TestDashboardCorruptCoverageDoesNotClaimUnmeasuredTraffic(t *testing.T) {
	database, router := newDashboardResponseTestServer(t, false, 0)
	if err := database.Create(&db.ControlPlaneState{Key: db.OriginTrafficCoverageKey, Value: "invalid timestamp"}).Error; err != nil {
		t.Fatalf("seed corrupt coverage: %v", err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	assertDashboardDatabaseError(t, recorder)
}

func TestDashboardPropagatesContextToEveryQuery(t *testing.T) {
	type contextKey struct{}
	key := contextKey{}
	for _, useRollup := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollup=%t", useRollup), func(t *testing.T) {
			database, router := newDashboardResponseTestServer(t, useRollup, 20)
			queries := 0
			registerDashboardReadCallback(t, database, func(tx *gorm.DB) {
				queries++
				if tx.Statement.Context.Value(key) != "dashboard request" {
					t.Errorf("query for %q lost request context", tx.Statement.Table)
				}
			})
			ctx := context.WithValue(context.Background(), key, "dashboard request")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard?range=7d", nil).WithContext(ctx))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if queries < 8 {
				t.Fatalf("observed %d queries, want every statistics and inventory read", queries)
			}
		})
	}
}

func TestDashboardCanceledRequestDoesNotReturnZeroStatistics(t *testing.T) {
	_, router := newDashboardResponseTestServer(t, true, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard?range=7d", nil).WithContext(ctx))
	assertDashboardDatabaseError(t, recorder)
}
