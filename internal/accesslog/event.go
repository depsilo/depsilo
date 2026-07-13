// Package accesslog implements the async batched recorder that rolls each
// proxy request up into the access_log_hourly + access_log_package_daily
// tables, plus the daily compactor (hourly → daily) and the retention
// sweeper. The recorder is intentionally non-blocking — its channel will
// drop events under back-pressure rather than slow the request path.
//
// See docs/specs/2026-06-26-access-log-rollup.md for the full design.
package accesslog

import "time"

// Event is the load-bearing record handed to Recorder.Record from the
// adapter access-log hook. At carries the UTC timestamp the request was
// observed; all rollup key derivation goes through e.At.UTC() so callers
// don't have to remember to normalize.
type Event struct {
	AdapterType string
	Method      string
	CacheKey    string
	PackageName string
	Upstream    string
	ClientIP    string
	Hit         bool
	LatencyMs   int64
	StatusCode  int
	BytesSent   int64
	At          time.Time
}

type fiveMinuteKey struct {
	BucketStart int64
	AdapterType string
	Hit         bool
	Upstream    string
}

func (e Event) FiveMinuteKey() fiveMinuteKey {
	unix := e.At.UTC().Unix()
	return fiveMinuteKey{
		BucketStart: (unix / 300) * 300,
		AdapterType: e.AdapterType,
		Hit:         e.Hit,
		Upstream:    e.Upstream,
	}
}

// hourlyKey mirrors AccessLogHourly's composite primary key.
type hourlyKey struct {
	Date        string // "YYYY-MM-DD" in UTC
	Hour        int    // 0-23 in UTC
	AdapterType string
	Hit         bool
	Upstream    string
}

// HourlyKey returns the rollup bucket coordinates for e. The ok value
// is always true today; the signature shape matches PackageDailyKey for
// symmetry should we later add an "ignore-this-event" path.
func (e Event) HourlyKey() hourlyKey {
	t := e.At.UTC()
	return hourlyKey{
		Date:        t.Format("2006-01-02"),
		Hour:        t.Hour(),
		AdapterType: e.AdapterType,
		Hit:         e.Hit,
		Upstream:    e.Upstream,
	}
}

// pkgDailyKey mirrors AccessLogPackageDaily's composite primary key.
type pkgDailyKey struct {
	Date        string
	AdapterType string
	PackageName string
	Hit         bool
}

// PackageDailyKey returns the package-grain bucket for e. ok is false
// when PackageName is empty — those events are aggregated into the
// hourly table but skipped at the package grain (no useful PK value).
func (e Event) PackageDailyKey() (pkgDailyKey, bool) {
	if e.PackageName == "" {
		return pkgDailyKey{}, false
	}
	return pkgDailyKey{
		Date:        e.At.UTC().Format("2006-01-02"),
		AdapterType: e.AdapterType,
		PackageName: e.PackageName,
		Hit:         e.Hit,
	}, true
}

// counters is the per-bucket accumulator the recorder maintains in memory
// between flushes. Cheaper than touching the DB on every event.
type counters struct {
	RequestCount int64
	TotalBytes   int64
	SumLatencyMs int64
	ErrorCount   int64
}

func (c *counters) add(e Event) {
	c.RequestCount++
	c.TotalBytes += e.BytesSent
	c.SumLatencyMs += e.LatencyMs
	if e.StatusCode >= 500 {
		c.ErrorCount++
	}
}
