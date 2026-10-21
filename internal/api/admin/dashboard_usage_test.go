package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/accesslog"
	"depsilo/internal/db"
)

func TestDashboardUsageIsIndependentOfRetainedRequestWindows(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	recorder := accesslog.NewRecorder(database, accesslog.Config{BatchInterval: time.Hour})
	t.Cleanup(func() {
		if err := recorder.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	usageReader, ok := recorder.(accesslog.UsageReader)
	if !ok {
		t.Fatal("production recorder does not expose lifetime usage")
	}
	handler := NewDashboardHandler(database, nil, nil, false, 0, usageReader)
	read := func(handler *DashboardHandler) db.DependencyUsage {
		t.Helper()
		router := gin.New()
		router.GET("/dashboard", handler.GetDashboard)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/dashboard?range=1h", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		var body struct {
			Usage  db.DependencyUsage `json:"usage"`
			Window struct {
				TotalRequests int64 `json:"total_requests"`
			} `json:"window"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body.Window.TotalRequests != 0 {
			t.Fatalf("expected empty period, got %d requests", body.Window.TotalRequests)
		}
		return body.Usage
	}
	if usage := read(handler); usage.State != db.DependencyNeverUsed || usage.FirstSuccessfulRequestAt != nil {
		t.Fatalf("new instance usage=%+v", usage)
	}

	at := time.Now().UTC()
	recorder.Record(accesslog.Event{
		AdapterType: "pypi", Method: http.MethodGet, CacheKey: "pypi/packages/example.whl",
		DependencyKind: db.CacheKindArtifact, CacheResult: "miss", DeliveryResult: "completed",
		StatusCode: http.StatusOK, BytesSent: 512, At: at,
	})
	if err := recorder.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Simulate retention removing all raw evidence. A period with zero rows
	// must still report that this instance has completed a dependency download.
	if err := database.Where("1 = 1").Delete(&db.AccessLog{}).Error; err != nil {
		t.Fatal(err)
	}
	if usage := read(handler); usage.State != db.DependencyUsed || usage.FirstSuccessfulRequestAt == nil || !usage.FirstSuccessfulRequestAt.Equal(at) {
		t.Fatalf("used instance with empty period=%+v", usage)
	}
	if err := recorder.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A handler without the live recorder must recover the same durable fact.
	if usage := read(NewDashboardHandler(database, nil, nil, false, 0)); usage.State != db.DependencyUsed || usage.FirstSuccessfulRequestAt == nil {
		t.Fatalf("durable usage=%+v", usage)
	}
}
