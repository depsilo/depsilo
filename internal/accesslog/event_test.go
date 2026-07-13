package accesslog

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func TestEvent_HourlyKey_NormalizesToUTC(t *testing.T) {
	// 2026-06-26T23:30 +08:00 == 2026-06-26T15:30 UTC. The hourly bucket
	// must follow UTC so dashboards stay consistent across deployments.
	at := mustParse(t, "2026-06-26T23:30:00+08:00")
	e := Event{AdapterType: "pypi", Hit: true, Upstream: "tuna", At: at}

	k := e.HourlyKey()
	if k.Date != "2026-06-26" {
		t.Errorf("Date = %q, want 2026-06-26", k.Date)
	}
	if k.Hour != 15 {
		t.Errorf("Hour = %d, want 15 (UTC)", k.Hour)
	}
	if k.AdapterType != "pypi" || !k.Hit || k.Upstream != "tuna" {
		t.Errorf("dimensions wrong: %+v", k)
	}
}

func TestEvent_HourlyKey_HourTransitionAtMidnightUTC(t *testing.T) {
	// 2026-06-26T00:30 UTC and 2026-06-25T23:30 UTC must land in different
	// (Date, Hour) buckets even though they are only 60 minutes apart.
	a := Event{AdapterType: "npm", At: mustParse(t, "2026-06-25T23:30:00Z")}.HourlyKey()
	b := Event{AdapterType: "npm", At: mustParse(t, "2026-06-26T00:30:00Z")}.HourlyKey()

	if a.Date == b.Date {
		t.Errorf("midnight-spanning events landed on same Date %q", a.Date)
	}
	if a.Hour != 23 || b.Hour != 0 {
		t.Errorf("hours: a=%d b=%d, want 23 / 0", a.Hour, b.Hour)
	}
}

func TestEvent_FiveMinuteKey_AlignsInUTC(t *testing.T) {
	e := Event{AdapterType: "pypi", Hit: true, Upstream: "tuna", At: mustParse(t, "2026-07-12T18:27:49+08:00")}
	got := e.FiveMinuteKey()
	want := mustParse(t, "2026-07-12T10:25:00Z").Unix()
	if got.BucketStart != want || got.AdapterType != "pypi" || !got.Hit || got.Upstream != "tuna" {
		t.Fatalf("FiveMinuteKey() = %+v, want bucket=%d pypi hit tuna", got, want)
	}
}

func TestEvent_FiveMinuteKey_SeparatesBoundary(t *testing.T) {
	a := Event{At: mustParse(t, "2026-07-12T10:29:59Z")}.FiveMinuteKey()
	b := Event{At: mustParse(t, "2026-07-12T10:30:00Z")}.FiveMinuteKey()
	if b.BucketStart-a.BucketStart != 300 {
		t.Fatalf("bucket delta = %d", b.BucketStart-a.BucketStart)
	}
}

func TestEvent_PackageDailyKey_OmittedWhenPackageEmpty(t *testing.T) {
	// Adapter logs an "index" hit with no extractable package name (e.g.
	// /pypi/simple/ list). Package grain skips those events to avoid an
	// empty-string PK row dominating top_packages.
	e := Event{AdapterType: "pypi", PackageName: "", At: time.Now().UTC()}
	if _, ok := e.PackageDailyKey(); ok {
		t.Errorf("expected ok=false when PackageName is empty")
	}
}

func TestEvent_PackageDailyKey_PresentWhenPackageSet(t *testing.T) {
	at := mustParse(t, "2026-06-26T10:00:00Z")
	e := Event{
		AdapterType: "pypi",
		PackageName: "numpy",
		Hit:         true,
		At:          at,
	}
	k, ok := e.PackageDailyKey()
	if !ok {
		t.Fatal("expected ok=true")
	}
	want := pkgDailyKey{Date: "2026-06-26", AdapterType: "pypi", PackageName: "numpy", Hit: true}
	if k != want {
		t.Errorf("key = %+v, want %+v", k, want)
	}
}

func TestCounters_AddAccumulates(t *testing.T) {
	c := counters{}
	c.add(Event{BytesSent: 100, LatencyMs: 20, StatusCode: 200})
	c.add(Event{BytesSent: 50, LatencyMs: 30, StatusCode: 500}) // error
	c.add(Event{BytesSent: 25, LatencyMs: 10, StatusCode: 502}) // error

	if c.RequestCount != 3 {
		t.Errorf("RequestCount = %d, want 3", c.RequestCount)
	}
	if c.TotalBytes != 175 {
		t.Errorf("TotalBytes = %d, want 175", c.TotalBytes)
	}
	if c.SumLatencyMs != 60 {
		t.Errorf("SumLatencyMs = %d, want 60", c.SumLatencyMs)
	}
	if c.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d, want 2 (status >= 500)", c.ErrorCount)
	}
}

func TestCounters_ErrorCount_BoundaryAt500(t *testing.T) {
	// 499 is not an error; 500 is the inclusive boundary.
	c := counters{}
	c.add(Event{StatusCode: 499})
	c.add(Event{StatusCode: 500})
	if c.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1 (only 500 counts, not 499)", c.ErrorCount)
	}
}
