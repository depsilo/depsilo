package accesslog

import (
	"context"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestRetention_PrunesOldRawAccessLogs(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	old := db.AccessLog{AdapterType: "pypi", CreatedAt: now.AddDate(0, 0, -10)}
	recent := db.AccessLog{AdapterType: "pypi", CreatedAt: now.AddDate(0, 0, -3)}
	d.Create(&old)
	d.Create(&recent)

	RunRetention(context.Background(), d, RetentionConfig{RawDays: 7})

	var rows []db.AccessLog
	d.Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("access_logs after sweep = %d, want 1 (recent kept)", len(rows))
	}
	if rows[0].ID != recent.ID {
		t.Errorf("kept wrong row: %+v", rows[0])
	}
}

func TestRetention_ZeroDays_NoSweep(t *testing.T) {
	// RawDays=0 must mean "never sweep" — that's the safe default we ship.
	d := newTestDB(t)
	d.Create(&db.AccessLog{AdapterType: "pypi", CreatedAt: time.Now().UTC().AddDate(-1, 0, 0)})

	RunRetention(context.Background(), d, RetentionConfig{RawDays: 0})

	var rows int64
	d.Model(&db.AccessLog{}).Count(&rows)
	if rows != 1 {
		t.Errorf("access_logs = %d, want 1 (RawDays=0 must not sweep)", rows)
	}
}

func TestRetention_PrunesOldRollupRows(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	oldDate := now.AddDate(0, 0, -400).Format("2006-01-02")
	recentDate := now.AddDate(0, 0, -10).Format("2006-01-02")

	d.Create(&db.AccessLogHourly{Date: oldDate, Hour: 1, AdapterType: "pypi"})
	d.Create(&db.AccessLogHourly{Date: recentDate, Hour: 1, AdapterType: "pypi"})
	d.Create(&db.AccessLogDaily{Date: oldDate, AdapterType: "pypi"})
	d.Create(&db.AccessLogDaily{Date: recentDate, AdapterType: "pypi"})
	d.Create(&db.AccessLogPackageDaily{Date: oldDate, AdapterType: "pypi", PackageName: "numpy"})
	d.Create(&db.AccessLogPackageDaily{Date: recentDate, AdapterType: "pypi", PackageName: "numpy"})

	RunRetention(context.Background(), d, RetentionConfig{RollupDays: 365})

	for _, tab := range []string{"access_log_hourly", "access_log_daily", "access_log_package_daily"} {
		var n int64
		d.Raw("SELECT COUNT(*) FROM " + tab).Scan(&n)
		if n != 1 {
			t.Errorf("%s rows = %d, want 1 (old date should be swept)", tab, n)
		}
	}
}

func TestRetention_PrunesFiveMinuteRowsAfterEightDays(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	if err := d.Create(&db.AccessLogFiveMinutely{
		BucketStart: now.Add(-9 * 24 * time.Hour).Unix(),
		AdapterType: "pypi",
	}).Error; err != nil {
		t.Fatalf("create old five-minute row: %v", err)
	}
	if err := d.Create(&db.AccessLogFiveMinutely{
		BucketStart: now.Add(-7 * 24 * time.Hour).Unix(),
		AdapterType: "npm",
	}).Error; err != nil {
		t.Fatalf("create recent five-minute row: %v", err)
	}

	RunRetention(context.Background(), d, RetentionConfig{
		FiveMinuteDays: 8,
		RollupDays:     365,
	})

	var rows []db.AccessLogFiveMinutely
	if err := d.Find(&rows).Error; err != nil {
		t.Fatalf("query five-minute rows: %v", err)
	}
	if len(rows) != 1 || rows[0].AdapterType != "npm" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestRetention_ArchivesExpiringRollupsSoTotalsSurvive(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	older := now.AddDate(0, 0, -420).Format("2006-01-02")
	old := now.AddDate(0, 0, -400).Format("2006-01-02")
	recent := now.AddDate(0, 0, -10).Format("2006-01-02")

	rows := []db.AccessLogHourly{
		{Date: older, Hour: 3, AdapterType: "pypi", Hit: true, RequestCount: 4, TotalBytes: 400, SumLatencyMs: 8},
		{Date: old, Hour: 1, AdapterType: "pypi", Hit: true, RequestCount: 6, TotalBytes: 600, SumLatencyMs: 12},
		{Date: old, Hour: 2, AdapterType: "pypi", Hit: false, Upstream: "tuna", RequestCount: 2, TotalBytes: 900, UpstreamRequests: 2, UpstreamBytes: 950, SumLatencyMs: 300, ErrorCount: 1},
		{Date: recent, Hour: 1, AdapterType: "pypi", Hit: true, RequestCount: 5, TotalBytes: 500},
	}
	if err := d.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	pkgs := []db.AccessLogPackageDaily{
		{Date: old, AdapterType: "pypi", PackageName: "numpy", Hit: true, RequestCount: 6, TotalBytes: 600},
		{Date: older, AdapterType: "pypi", PackageName: "numpy", Hit: true, RequestCount: 4, TotalBytes: 400},
		{Date: recent, AdapterType: "pypi", PackageName: "numpy", Hit: true, RequestCount: 5, TotalBytes: 500},
	}
	if err := d.Create(&pkgs).Error; err != nil {
		t.Fatal(err)
	}

	RunRetention(context.Background(), d, RetentionConfig{RollupDays: 365})
	// A second sweep must not add the same days again.
	RunRetention(context.Background(), d, RetentionConfig{RollupDays: 365})

	var archive []db.AccessLogArchive
	d.Order("hit DESC").Find(&archive)
	if len(archive) != 2 {
		t.Fatalf("archive rows = %+v, want hit and tuna miss", archive)
	}
	if a := archive[0]; !a.Hit || a.RequestCount != 10 || a.TotalBytes != 1000 || a.SumLatencyMs != 20 {
		t.Errorf("hit archive = %+v, want 10 requests / 1000 bytes / 20 ms", a)
	}
	if a := archive[1]; a.Upstream != "tuna" || a.RequestCount != 2 || a.UpstreamBytes != 950 || a.UpstreamRequests != 2 || a.ErrorCount != 1 {
		t.Errorf("miss archive = %+v", a)
	}
	var pkg db.AccessLogPackageArchive
	d.First(&pkg)
	if pkg.PackageName != "numpy" || pkg.RequestCount != 10 || pkg.TotalBytes != 1000 {
		t.Errorf("package archive = %+v, want 10 requests / 1000 bytes", pkg)
	}
	var first db.ControlPlaneState
	d.Where("key = ?", db.AccessLogArchiveFirstDateKey).First(&first)
	if first.Value != older+"T03:00:00Z" {
		t.Errorf("archive first hour = %q, want %sT03:00:00Z", first.Value, older)
	}
	var hourly int64
	d.Model(&db.AccessLogHourly{}).Count(&hourly)
	if hourly != 1 {
		t.Errorf("hourly rows = %d, want only the retained day", hourly)
	}

	// Later sweeps add to the archive and never move its first date forward.
	later := now.AddDate(0, 0, -366).Format("2006-01-02")
	d.Create(&db.AccessLogHourly{Date: later, Hour: 0, AdapterType: "pypi", Hit: true, RequestCount: 1, TotalBytes: 10})
	RunRetention(context.Background(), d, RetentionConfig{RollupDays: 365})
	d.Where("hit = ?", true).First(&archive[0])
	if archive[0].RequestCount != 11 {
		t.Errorf("hit archive after a later sweep = %d requests, want 11", archive[0].RequestCount)
	}
	d.Where("key = ?", db.AccessLogArchiveFirstDateKey).First(&first)
	if first.Value != older+"T03:00:00Z" {
		t.Errorf("archive first hour moved to %q", first.Value)
	}
}

func TestArchiveRollupsBefore_FailureDeletesNothing(t *testing.T) {
	d := newTestDB(t)
	old := time.Now().UTC().AddDate(0, 0, -400).Format("2006-01-02")
	d.Create(&db.AccessLogHourly{Date: old, Hour: 1, AdapterType: "pypi", Hit: true, RequestCount: 3})
	if err := d.Exec("DROP TABLE access_log_package_archive").Error; err != nil {
		t.Fatal(err)
	}

	if err := ArchiveRollupsBefore(context.Background(), d, time.Now().UTC().Format("2006-01-02")); err == nil {
		t.Fatal("archive succeeded without its package table")
	}
	var hourly, archived int64
	d.Model(&db.AccessLogHourly{}).Count(&hourly)
	d.Model(&db.AccessLogArchive{}).Count(&archived)
	if hourly != 1 || archived != 0 {
		t.Fatalf("after a failed archive: hourly %d, archive %d; want the day kept and nothing archived", hourly, archived)
	}
}

func TestBackfillNeverRebuildsArchivedDays(t *testing.T) {
	d := newTestDB(t)
	now := time.Now().UTC()
	oldDay := now.AddDate(0, 0, -40)
	recentDay := now.AddDate(0, 0, -1)
	// Raw logs are kept longer than rollups (retention_days 0 or above the rollup horizon).
	raw := []db.AccessLog{
		{AdapterType: "pypi", PackageName: "numpy", Hit: true, CacheResult: "hit", BytesSent: 100, CreatedAt: oldDay},
		{AdapterType: "pypi", PackageName: "numpy", Hit: true, CacheResult: "hit", BytesSent: 50, CreatedAt: recentDay},
	}
	if err := d.Create(&raw).Error; err != nil {
		t.Fatal(err)
	}
	if err := BackfillIfEmpty(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	cutoff := now.AddDate(0, 0, -30).Format("2006-01-02")
	if err := ArchiveRollupsBefore(context.Background(), d, cutoff); err != nil {
		t.Fatal(err)
	}
	// The instance idles until every remaining hourly row is archived too.
	if err := ArchiveRollupsBefore(context.Background(), d, now.AddDate(0, 0, 1).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	var before db.AccessLogArchive
	d.First(&before)
	if before.RequestCount != 2 {
		t.Fatalf("archive = %d requests, want 2", before.RequestCount)
	}

	// A restart finds hourly empty and backfills; the next sweep must not re-add archived days.
	if err := BackfillIfEmpty(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveRollupsBefore(context.Background(), d, now.AddDate(0, 0, 1).Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	var after db.AccessLogArchive
	d.First(&after)
	var pkg db.AccessLogPackageArchive
	d.First(&pkg)
	if after.RequestCount != 2 || after.TotalBytes != 150 || pkg.RequestCount != 2 {
		t.Fatalf("after restart: archive %d requests / %d bytes, packages %d; want 2 / 150 / 2", after.RequestCount, after.TotalBytes, pkg.RequestCount)
	}
}
