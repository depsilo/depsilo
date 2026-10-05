package admin

import (
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestWindowPayloadEstimatesTimeSavedFromPeriodAverages(t *testing.T) {
	// 8 hits averaging 20ms and 2 misses averaging 220ms: (220-20)*8 = 1600ms.
	payload := windowPayload(aggSnapshot{
		Total:          10,
		Hits:           8,
		SumHitLatency:  8 * 20,
		SumMissLatency: 2 * 220,
	})
	if got := payload["time_saved_ms"]; got != int64(1600) {
		t.Fatalf("time_saved_ms = %v, want 1600", got)
	}
}

func TestWindowPayloadWithholdsTimeSavedWithoutComparableSamples(t *testing.T) {
	cases := []struct {
		name string
		in   aggSnapshot
	}{
		{
			name: "hits only",
			in:   aggSnapshot{Total: 5, Hits: 5, SumHitLatency: 100},
		},
		{
			name: "misses only",
			in:   aggSnapshot{Total: 3, SumMissLatency: 900},
		},
		{
			name: "empty period",
			in:   aggSnapshot{},
		},
		{
			name: "cache slower than origin",
			in:   aggSnapshot{Total: 4, Hits: 2, SumHitLatency: 600, SumMissLatency: 2 * 50},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := windowPayload(tc.in)
			if got := payload["time_saved_ms"]; got != int64(0) {
				// Zero is the period's own "no comparable pair" value; the
				// client gates on hit_requests/miss_requests before showing it.
				t.Fatalf("time_saved_ms = %v, want 0", got)
			}
		})
	}
}

func TestDashboardWindowReportsTimeSavedForTheSelectedRange(t *testing.T) {
	handler, database := newOverviewTestHandler(t, false)
	now := time.Now().UTC()

	rows := make([]db.AccessLog, 0, 10)
	for i := 0; i < 8; i++ {
		rows = append(rows, db.AccessLog{
			AdapterType: "npm", Method: "GET", PackageName: "left-pad", Hit: true, CacheResult: "hit",
			BytesSent: 100, LatencyMs: 20, StatusCode: 200, CreatedAt: now.Add(-20 * time.Minute),
		})
	}
	for i := 0; i < 2; i++ {
		rows = append(rows, db.AccessLog{
			AdapterType: "npm", Method: "GET", PackageName: "left-pad", Hit: false, CacheResult: "miss",
			BytesSent: 1000, UpstreamRequests: 1, UpstreamBytes: 900, LatencyMs: 220, StatusCode: 200,
			CreatedAt: now.Add(-15 * time.Minute),
		})
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed access logs: %v", err)
	}

	body := getOverview(t, handler, "?range=1h")
	if body.Window.HitBytes != 800 {
		t.Fatalf("hit bytes = %d, want 800", body.Window.HitBytes)
	}
	if body.Window.TimeSavedMs != 1600 {
		t.Fatalf("time saved = %d ms, want 1600", body.Window.TimeSavedMs)
	}
}

func TestDashboardHourlyRollupReportsTimeSavedWithoutRescanningRawLogs(t *testing.T) {
	handler, database := newOverviewTestHandler(t, true)
	now := time.Now().UTC()
	date := now.Format("2006-01-02")

	rows := []db.AccessLogHourly{
		{Date: date, Hour: now.Hour(), AdapterType: "npm", Hit: true,
			RequestCount: 8, TotalBytes: 800, SumLatencyMs: 8 * 20},
		{Date: date, Hour: now.Hour(), AdapterType: "npm", Hit: false,
			RequestCount: 2, TotalBytes: 2000, UpstreamRequests: 2, UpstreamBytes: 1800, SumLatencyMs: 2 * 220},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("seed hourly rollup: %v", err)
	}

	body := getOverview(t, handler, "?range=7d")
	if body.Window.TimeSavedMs != 1600 {
		t.Fatalf("time saved = %d ms, want 1600 from the hourly rollup", body.Window.TimeSavedMs)
	}
}
