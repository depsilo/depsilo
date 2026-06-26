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
