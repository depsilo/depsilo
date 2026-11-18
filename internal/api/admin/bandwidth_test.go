package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

// The raw log and the daily rollups must report the same package and Upstream
// request counts for the same traffic.
func TestBandwidthReportCountsPackageAndUpstreamRequests(t *testing.T) {
	day := time.Now().UTC().AddDate(0, 0, -1)
	date := day.Format("2006-01-02")
	for _, useRollup := range []bool{false, true} {
		_, database := newOverviewTestHandler(t, useRollup)
		if useRollup {
			seed := []any{
				&[]db.AccessLogDaily{
					{Date: date, AdapterType: "npm", Hit: true, RequestCount: 2, TotalBytes: 200, SumLatencyMs: 20},
					{Date: date, AdapterType: "npm", Hit: false, Upstream: "npmjs", RequestCount: 1, TotalBytes: 500, SumLatencyMs: 300},
				},
				&[]db.AccessLogPackageDaily{
					{Date: date, AdapterType: "npm", PackageName: "left-pad", Hit: true, RequestCount: 2, TotalBytes: 200},
					{Date: date, AdapterType: "npm", PackageName: "left-pad", Hit: false, RequestCount: 1, TotalBytes: 500},
				},
			}
			for _, rows := range seed {
				if err := database.Create(rows).Error; err != nil {
					t.Fatal(err)
				}
			}
		} else {
			rows := []db.AccessLog{
				{AdapterType: "npm", PackageName: "left-pad", Hit: true, BytesSent: 100, LatencyMs: 10, CreatedAt: day},
				{AdapterType: "npm", PackageName: "left-pad", Hit: true, BytesSent: 100, LatencyMs: 10, CreatedAt: day},
				{AdapterType: "npm", PackageName: "left-pad", Hit: false, Upstream: "npmjs", BytesSent: 500, LatencyMs: 300, CreatedAt: day},
			}
			if err := database.Create(&rows).Error; err != nil {
				t.Fatal(err)
			}
		}

		router := gin.New()
		router.GET("/bandwidth", NewBandwidthHandler(database, useRollup).GetReport)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/bandwidth?range=7d", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("rollup=%t status=%d body=%s", useRollup, recorder.Code, recorder.Body.String())
		}
		var report contract.BandwidthReportResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &report); err != nil {
			t.Fatal(err)
		}
		wantPackage := contract.BandwidthPackage{PackageName: "left-pad", Ecosystem: "npm", TotalBytes: 700, HitBytes: 200, RequestCount: 3}
		if len(report.TopPackages) != 1 || report.TopPackages[0] != wantPackage {
			t.Errorf("rollup=%t top_packages = %+v, want [%+v]", useRollup, report.TopPackages, wantPackage)
		}
		wantUpstream := contract.BandwidthUpstream{Upstream: "npmjs", MissBytes: 500, RequestCount: 1, AvgLatencyMs: 300}
		if len(report.ByUpstream) != 1 || report.ByUpstream[0] != wantUpstream {
			t.Errorf("rollup=%t by_upstream = %+v, want [%+v]", useRollup, report.ByUpstream, wantUpstream)
		}
		if report.Summary.TotalRequests != 3 || report.Summary.HitBytes != 200 {
			t.Errorf("rollup=%t summary = %+v", useRollup, report.Summary)
		}
	}
}
