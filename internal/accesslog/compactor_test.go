package accesslog

import (
	"context"
	"testing"
	"time"

	"depsilo/internal/db"
)

func TestCompactor_SumsHourlyRowsIntoSingleDailyRow(t *testing.T) {
	d := newTestDB(t)

	// 24 hourly rows for one (date, adapter, hit, upstream) bucket.
	for hour := 0; hour < 24; hour++ {
		d.Create(&db.AccessLogHourly{
			Date: "2026-06-25", Hour: hour,
			AdapterType: "pypi", Hit: true, Upstream: "tuna",
			RequestCount: 10,
			TotalBytes:   1000,
			SumLatencyMs: 100,
			ErrorCount:   1,
			UpdatedAt:    time.Now().UTC(),
		})
	}

	if err := compactDate(context.Background(), d, "2026-06-25"); err != nil {
		t.Fatalf("compactDate: %v", err)
	}

	var rows []db.AccessLogDaily
	d.Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("daily rows = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.RequestCount != 240 || r.TotalBytes != 24000 || r.SumLatencyMs != 2400 || r.ErrorCount != 24 {
		t.Errorf("sums wrong: %+v", r)
	}
}

func TestCompactor_IdempotentOnRerun(t *testing.T) {
	// Running compactDate twice must NOT double-count. The DO UPDATE
	// clause replaces the daily row instead of adding to it.
	d := newTestDB(t)
	d.Create(&db.AccessLogHourly{
		Date: "2026-06-25", Hour: 10,
		AdapterType: "pypi", Hit: true, Upstream: "tuna",
		RequestCount: 100, TotalBytes: 5000, UpdatedAt: time.Now().UTC(),
	})

	_ = compactDate(context.Background(), d, "2026-06-25")
	_ = compactDate(context.Background(), d, "2026-06-25")

	var r db.AccessLogDaily
	d.First(&r)
	if r.RequestCount != 100 {
		t.Errorf("RequestCount = %d, want 100 (rerun should not double-count)", r.RequestCount)
	}
}

func TestNextCompactRunUTC_AlwaysAfterNow(t *testing.T) {
	cases := []string{
		"2026-06-26T00:04:59Z", // just before today's run
		"2026-06-26T00:05:00Z", // exactly at today's run → next is tomorrow
		"2026-06-26T12:00:00Z", // mid-day → next is tomorrow
		"2026-06-26T23:59:59Z", // just before midnight
	}
	for _, c := range cases {
		now := mustParse(t, c)
		next := nextCompactRunUTC(now)
		if !next.After(now) {
			t.Errorf("now=%s next=%s should be strictly after", c, next)
		}
		if next.UTC().Hour() != 0 || next.UTC().Minute() != 5 {
			t.Errorf("next=%s should be UTC 00:05", next)
		}
	}
}
