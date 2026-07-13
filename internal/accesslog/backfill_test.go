package accesslog

import (
	"context"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestBackfill_PopulatesRollupsFromAccessLogs(t *testing.T) {
	d := newTestDB(t)

	at := mustParse(t, "2026-06-25T10:30:00Z")
	d.Create(&db.AccessLog{AdapterType: "pypi", PackageName: "numpy", Hit: true, Upstream: "tuna", BytesSent: 100, CreatedAt: at})
	d.Create(&db.AccessLog{AdapterType: "pypi", PackageName: "numpy", Hit: true, Upstream: "tuna", BytesSent: 200, CreatedAt: at})
	d.Create(&db.AccessLog{AdapterType: "npm", PackageName: "react", Hit: false, BytesSent: 50, StatusCode: 502, CreatedAt: at})

	if err := BackfillIfEmpty(context.Background(), d); err != nil {
		t.Fatalf("BackfillIfEmpty: %v", err)
	}

	var hourly []db.AccessLogHourly
	d.Find(&hourly)
	// (pypi, hit=true, tuna) and (npm, hit=false, '') → 2 hourly rows.
	if len(hourly) != 2 {
		t.Errorf("hourly rows = %d, want 2 — got %+v", len(hourly), hourly)
	}
	for _, h := range hourly {
		switch {
		case h.AdapterType == "pypi" && (h.RequestCount != 2 || h.TotalBytes != 300):
			t.Errorf("pypi bucket: %+v", h)
		case h.AdapterType == "npm" && (h.RequestCount != 1 || h.ErrorCount != 1):
			t.Errorf("npm bucket: %+v", h)
		}
	}

	var daily []db.AccessLogDaily
	d.Find(&daily)
	if len(daily) != 2 {
		t.Errorf("daily rows = %d, want 2", len(daily))
	}

	var pkg []db.AccessLogPackageDaily
	d.Find(&pkg)
	if len(pkg) != 2 {
		t.Errorf("package_daily rows = %d, want 2", len(pkg))
	}
}

func TestBackfill_SkipsWhenHourlyAlreadyHasData(t *testing.T) {
	// Once any rollup row exists we treat the table as initialized and
	// skip backfill — the recorder is authoritative from that point.
	d := newTestDB(t)
	d.Create(&db.AccessLogHourly{Date: "2026-06-25", Hour: 5, AdapterType: "pypi", RequestCount: 999})
	// Plus orphan raw row that would otherwise be backfilled.
	d.Create(&db.AccessLog{AdapterType: "pypi", CreatedAt: time.Now().UTC()})

	if err := BackfillIfEmpty(context.Background(), d); err != nil {
		t.Fatalf("BackfillIfEmpty: %v", err)
	}

	var n int64
	d.Model(&db.AccessLogHourly{}).Count(&n)
	if n != 1 {
		t.Errorf("hourly rows = %d, want 1 (backfill must be skipped)", n)
	}
	// The pre-existing row's RequestCount must not be touched.
	var h db.AccessLogHourly
	d.First(&h)
	if h.RequestCount != 999 {
		t.Errorf("pre-existing RequestCount overwritten: %d", h.RequestCount)
	}
}

func TestBackfill_SkipsAccessLogsWithEmptyPackageName(t *testing.T) {
	// Index requests (PackageName="") belong in hourly but NOT in
	// package_daily, otherwise they monopolize top_packages.
	d := newTestDB(t)
	at := mustParse(t, "2026-06-25T10:00:00Z")
	d.Create(&db.AccessLog{AdapterType: "pypi", PackageName: "", Hit: true, CreatedAt: at})

	if err := BackfillIfEmpty(context.Background(), d); err != nil {
		t.Fatalf("BackfillIfEmpty: %v", err)
	}

	var hourly int64
	d.Model(&db.AccessLogHourly{}).Count(&hourly)
	if hourly != 1 {
		t.Errorf("hourly rows = %d, want 1", hourly)
	}
	var pkg int64
	d.Model(&db.AccessLogPackageDaily{}).Count(&pkg)
	if pkg != 0 {
		t.Errorf("package_daily rows = %d, want 0 (empty package name skipped)", pkg)
	}
}

func TestBackfillFiveMinutely_BackfillsSevenDaysAndRunsOnce(t *testing.T) {
	d := newTestDB(t)
	now := mustParse(t, "2026-07-12T12:34:56Z")
	recentAt := now.Add(-time.Hour)

	if err := d.Create(&db.AccessLog{
		AdapterType: "pypi",
		Hit:         true,
		BytesSent:   123,
		LatencyMs:   45,
		StatusCode:  503,
		CreatedAt:   recentAt,
	}).Error; err != nil {
		t.Fatalf("create recent access log: %v", err)
	}
	if err := d.Create(&db.AccessLog{
		AdapterType: "npm",
		CreatedAt:   now.Add(-8 * 24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create old access log: %v", err)
	}

	if err := BackfillFiveMinutely(context.Background(), d, now); err != nil {
		t.Fatalf("BackfillFiveMinutely: %v", err)
	}

	var rows []db.AccessLogFiveMinutely
	if err := d.Find(&rows).Error; err != nil {
		t.Fatalf("query five-minute rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("five-minute rows = %d, want 1: %+v", len(rows), rows)
	}
	wantBucket := Event{At: recentAt}.FiveMinuteKey().BucketStart
	if rows[0].BucketStart != wantBucket || rows[0].AdapterType != "pypi" ||
		!rows[0].Hit || rows[0].Upstream != "" || rows[0].RequestCount != 1 ||
		rows[0].TotalBytes != 123 || rows[0].SumLatencyMs != 45 || rows[0].ErrorCount != 1 {
		t.Errorf("five-minute row = %+v", rows[0])
	}

	var marker db.ControlPlaneState
	if err := d.First(&marker, "key = ?", fiveMinuteBackfillMarker).Error; err != nil {
		t.Fatalf("query backfill marker: %v", err)
	}

	if err := d.Model(&db.AccessLogFiveMinutely{}).
		Where("bucket_start = ? AND adapter_type = ? AND hit = ? AND upstream = ?", wantBucket, "pypi", true, "").
		Update("request_count", 99).Error; err != nil {
		t.Fatalf("mutate five-minute row: %v", err)
	}
	if err := d.Create(&db.AccessLog{AdapterType: "cargo", CreatedAt: now.Add(-30 * time.Minute)}).Error; err != nil {
		t.Fatalf("create access log after marker: %v", err)
	}

	if err := BackfillFiveMinutely(context.Background(), d, now); err != nil {
		t.Fatalf("second BackfillFiveMinutely: %v", err)
	}
	rows = nil
	if err := d.Find(&rows).Error; err != nil {
		t.Fatalf("query five-minute rows after second call: %v", err)
	}
	if len(rows) != 1 || rows[0].RequestCount != 99 {
		t.Fatalf("second backfill changed rows: %+v", rows)
	}
}

func TestBackfillFiveMinutely_MarkerFailureRollsBackRows(t *testing.T) {
	now := mustParse(t, "2026-07-12T12:34:56Z")
	triggerSQL := `
CREATE TRIGGER reject_five_minutely_marker
BEFORE INSERT ON control_plane_states
WHEN NEW.key = '` + fiveMinuteBackfillMarker + `'
BEGIN
    SELECT RAISE(ABORT, 'reject five-minute marker');
END`

	for _, tt := range []struct {
		name     string
		existing *db.AccessLogFiveMinutely
	}{
		{name: "removes newly inserted rows"},
		{
			name: "restores rows deleted before rebuild",
			existing: &db.AccessLogFiveMinutely{
				BucketStart:  Event{At: now.Add(-30 * time.Minute)}.FiveMinuteKey().BucketStart,
				AdapterType:  "cargo",
				RequestCount: 77,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := newTestDB(t)
			if err := d.Create(&db.AccessLog{AdapterType: "pypi", CreatedAt: now.Add(-time.Hour)}).Error; err != nil {
				t.Fatalf("create access log: %v", err)
			}
			if tt.existing != nil {
				if err := d.Create(tt.existing).Error; err != nil {
					t.Fatalf("create existing five-minute row: %v", err)
				}
			}
			if err := d.Exec(triggerSQL).Error; err != nil {
				t.Fatalf("create marker rejection trigger: %v", err)
			}

			if err := BackfillFiveMinutely(context.Background(), d, now); err == nil {
				t.Fatal("BackfillFiveMinutely error = nil, want marker insertion failure")
			}

			var rows []db.AccessLogFiveMinutely
			if err := d.Find(&rows).Error; err != nil {
				t.Fatalf("query five-minute rows: %v", err)
			}
			if tt.existing == nil {
				if len(rows) != 0 {
					t.Fatalf("five-minute rows after rollback = %+v, want none", rows)
				}
			} else if len(rows) != 1 || rows[0].BucketStart != tt.existing.BucketStart ||
				rows[0].AdapterType != tt.existing.AdapterType || rows[0].RequestCount != tt.existing.RequestCount {
				t.Fatalf("five-minute rows after rollback = %+v, want existing row %+v", rows, *tt.existing)
			}

			var markerCount int64
			if err := d.Model(&db.ControlPlaneState{}).
				Where("key = ?", fiveMinuteBackfillMarker).
				Count(&markerCount).Error; err != nil {
				t.Fatalf("count markers: %v", err)
			}
			if markerCount != 0 {
				t.Fatalf("markers after rollback = %d, want 0", markerCount)
			}
		})
	}
}
