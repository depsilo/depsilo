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
