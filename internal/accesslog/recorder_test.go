package accesslog

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// newTestDB returns an in-memory sqlite DB with all the schemas the
// recorder touches. MaxOpenConns=1 pins the connection so the recorder
// goroutine and the test goroutine share the same :memory: instance —
// without it each pool connection sees a fresh empty DB.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := d.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := d.AutoMigrate(
		&db.AccessLog{}, &db.AccessLogFiveMinutely{}, &db.AccessLogHourly{},
		&db.AccessLogDaily{}, &db.AccessLogPackageDaily{}, &db.ControlPlaneState{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d
}

func mkEvent(adapter, pkg string, hit bool, at time.Time) Event {
	return Event{
		AdapterType: adapter,
		Method:      "GET",
		CacheKey:    adapter + "/" + pkg,
		PackageName: pkg,
		Upstream:    "tuna",
		Hit:         hit,
		LatencyMs:   42,
		BytesSent:   100,
		At:          at,
	}
}

func TestRecorder_BatchedFlush_WritesRawAndRollup(t *testing.T) {
	d := newTestDB(t)
	r := NewRecorder(d, Config{Enabled: true, BatchSize: 1000, BatchInterval: time.Hour})
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	at := mustParse(t, "2026-06-26T10:00:00Z")
	for i := 0; i < 5; i++ {
		r.Record(mkEvent("pypi", "numpy", true, at))
	}
	r.Record(mkEvent("pypi", "numpy", false, at)) // 1 miss
	r.Record(mkEvent("npm", "react", true, at))   // different adapter

	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}

	// 7 raw rows.
	var rawCount int64
	d.Model(&db.AccessLog{}).Count(&rawCount)
	if rawCount != 7 {
		t.Errorf("raw rows = %d, want 7", rawCount)
	}

	// Hourly: (pypi,hit=true) merged, (pypi,hit=false) separate, (npm,hit=true) separate → 3 rows.
	var hourly []db.AccessLogHourly
	d.Find(&hourly)
	if len(hourly) != 3 {
		t.Fatalf("hourly rows = %d, want 3 — got %+v", len(hourly), hourly)
	}
	for _, h := range hourly {
		switch {
		case h.AdapterType == "pypi" && h.Hit && h.RequestCount != 5:
			t.Errorf("pypi-hit RequestCount = %d, want 5", h.RequestCount)
		case h.AdapterType == "pypi" && !h.Hit && h.RequestCount != 1:
			t.Errorf("pypi-miss RequestCount = %d, want 1", h.RequestCount)
		case h.AdapterType == "npm" && h.RequestCount != 1:
			t.Errorf("npm RequestCount = %d, want 1", h.RequestCount)
		}
	}

	// FiveMinutely has the same adapter/hit/upstream dimensions as Hourly.
	var fiveMinutely []db.AccessLogFiveMinutely
	d.Find(&fiveMinutely)
	if len(fiveMinutely) != 3 {
		t.Fatalf("five-minute rows = %d, want 3 — got %+v", len(fiveMinutely), fiveMinutely)
	}

	// PackageDaily: numpy-hit (5), numpy-miss (1), react-hit (1) → 3 rows.
	var pkg []db.AccessLogPackageDaily
	d.Find(&pkg)
	if len(pkg) != 3 {
		t.Errorf("package daily rows = %d, want 3 — got %+v", len(pkg), pkg)
	}
}

func TestRecorder_FiveMinuteUpsertAccumulatesAcrossFlushes(t *testing.T) {
	d := newTestDB(t)
	r := NewRecorder(d, Config{Enabled: true, BatchSize: 1000, BatchInterval: time.Hour})
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	at := mustParse(t, "2026-07-12T10:02:00Z")
	for i := 0; i < 3; i++ {
		r.Record(mkEvent("pypi", "numpy", true, at))
	}
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("first flush: %v", err)
	}

	for i := 0; i < 4; i++ {
		r.Record(mkEvent("pypi", "numpy", true, at))
	}
	if err := r.Flush(context.Background()); err != nil {
		t.Fatalf("second flush: %v", err)
	}

	var rows []db.AccessLogFiveMinutely
	if err := d.Find(&rows).Error; err != nil {
		t.Fatalf("query five-minute rollup: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("five-minute rows = %d, want 1 — got %+v", len(rows), rows)
	}
	row := rows[0]
	wantBucket := mustParse(t, "2026-07-12T10:00:00Z").Unix()
	if row.RequestCount != 7 || row.TotalBytes != 700 || row.BucketStart != wantBucket {
		t.Fatalf("five-minute row = %+v, want request_count=7 total_bytes=700 bucket_start=%d", row, wantBucket)
	}
}

func TestRecorder_UpsertAccumulatesAcrossFlushes(t *testing.T) {
	// Two flushes that both target the same (date,hour,adapter,hit,upstream)
	// bucket must SUM into a single row, not replace each other. This is the
	// load-bearing property of the ON CONFLICT DO UPDATE clause.
	d := newTestDB(t)
	r := NewRecorder(d, Config{Enabled: true, BatchSize: 1000, BatchInterval: time.Hour})
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	at := mustParse(t, "2026-06-26T10:00:00Z")
	for i := 0; i < 3; i++ {
		r.Record(mkEvent("pypi", "numpy", true, at))
	}
	_ = r.Flush(context.Background())

	for i := 0; i < 4; i++ {
		r.Record(mkEvent("pypi", "numpy", true, at))
	}
	_ = r.Flush(context.Background())

	var hourly db.AccessLogHourly
	if err := d.Where("adapter_type = ? AND hit = ?", "pypi", true).First(&hourly).Error; err != nil {
		t.Fatalf("query hourly: %v", err)
	}
	if hourly.RequestCount != 7 {
		t.Errorf("RequestCount = %d, want 7 (3+4 across two flushes)", hourly.RequestCount)
	}
}

func TestRecorder_PackageNameEmpty_SkipsPackageDaily(t *testing.T) {
	// Events with no extractable package name (e.g. index requests) belong
	// in the hourly rollup but must NOT land in package_daily — the rollup
	// table's PackageName is part of the PK and an empty string row would
	// hog the top-package slot.
	d := newTestDB(t)
	r := NewRecorder(d, Config{Enabled: true, BatchSize: 1000, BatchInterval: time.Hour})
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	at := mustParse(t, "2026-06-26T10:00:00Z")
	r.Record(Event{AdapterType: "pypi", Hit: true, At: at, PackageName: ""})
	_ = r.Flush(context.Background())

	var h int64
	d.Model(&db.AccessLogHourly{}).Count(&h)
	if h != 1 {
		t.Errorf("hourly rows = %d, want 1", h)
	}
	var p int64
	d.Model(&db.AccessLogPackageDaily{}).Count(&p)
	if p != 0 {
		t.Errorf("package_daily rows = %d, want 0 (empty package name)", p)
	}
}

func TestRecorder_Close_FlushesQueuedEvents(t *testing.T) {
	// Close must drain events the channel still holds and flush them
	// before returning, otherwise shutdown loses the last batch.
	d := newTestDB(t)
	r := NewRecorder(d, Config{Enabled: true, BatchSize: 1000, BatchInterval: time.Hour})

	at := mustParse(t, "2026-06-26T10:00:00Z")
	for i := 0; i < 10; i++ {
		r.Record(mkEvent("pypi", "numpy", true, at))
	}
	// No explicit Flush — Close should do it.

	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var raw int64
	d.Model(&db.AccessLog{}).Count(&raw)
	if raw != 10 {
		t.Errorf("raw rows after Close = %d, want 10", raw)
	}
}

func TestRecorder_DropsOnFullChannel(t *testing.T) {
	// Spec §10 #3 explicitly accepts dropping under back-pressure rather
	// than blocking the request path. We can't easily fill the 4096-event
	// channel, so this test uses a recorder we construct directly with a
	// tiny channel.
	d := newTestDB(t)
	r := &batchedRecorder{
		db:            d,
		in:            make(chan Event, 1),
		batchSize:     1000,
		flushInterval: time.Hour,
		aggHourly:     make(map[hourlyKey]*counters),
		aggPkg:        make(map[pkgDailyKey]*counters),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		flushReq:      make(chan chan struct{}),
	}
	// Do NOT start r.loop() — we want events to pile up.

	at := time.Now().UTC()
	r.Record(mkEvent("pypi", "x", true, at)) // fills the buffer
	r.Record(mkEvent("pypi", "x", true, at)) // should drop
	r.Record(mkEvent("pypi", "x", true, at)) // should drop

	if got := r.Dropped(); got != 2 {
		t.Errorf("Dropped = %d, want 2", got)
	}
}

func TestRecorder_NullVariant_WritesRawOnly(t *testing.T) {
	// Enabled=false short-circuits to nullRecorder: legacy behavior of
	// "write raw row per request, no rollup". Used as the rollback path.
	d := newTestDB(t)
	r := NewRecorder(d, Config{Enabled: false})
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	at := mustParse(t, "2026-06-26T10:00:00Z")
	r.Record(mkEvent("pypi", "numpy", true, at))

	// nullRecorder writes asynchronously via goroutine — give it a moment.
	deadline := time.Now().Add(time.Second)
	var raw int64
	for time.Now().Before(deadline) {
		d.Model(&db.AccessLog{}).Count(&raw)
		if raw == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if raw != 1 {
		t.Errorf("raw rows = %d, want 1", raw)
	}
	var h int64
	d.Model(&db.AccessLogHourly{}).Count(&h)
	if h != 0 {
		t.Errorf("hourly rows = %d, want 0 (rollup disabled)", h)
	}
}
