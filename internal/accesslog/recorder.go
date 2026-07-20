package accesslog

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// Recorder is the seam the adapter layer drives. Production gets a
// batched implementation; disabling rollups gets a raw-only recorder
// with the same bounded, drainable lifecycle.
type Recorder interface {
	Record(e Event)
	Flush(ctx context.Context) error
	Close(ctx context.Context) error
}

// Config carries the recorder's tunables. RecorderConfig is mostly a
// projection of config.AccessLogConfig — the package doesn't import
// the config package so it can be tested without bringing viper along.
type Config struct {
	Enabled       bool
	BatchSize     int
	BatchInterval time.Duration
}

// NewRecorder returns a batched recorder when cfg.Enabled is true,
// and a nullRecorder otherwise. The null variant still writes raw
// rows to access_logs so the admin logs page keeps working; it just
// skips the rollup tables.
func NewRecorder(database *gorm.DB, cfg Config) Recorder {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.BatchInterval <= 0 {
		cfg.BatchInterval = 5 * time.Second
	}
	if !cfg.Enabled {
		r := &nullRecorder{
			db:            database,
			in:            make(chan Event, 4096),
			batchSize:     cfg.BatchSize,
			flushInterval: cfg.BatchInterval,
			stop:          make(chan struct{}),
			done:          make(chan struct{}),
			flushReq:      make(chan chan struct{}),
		}
		go r.loop()
		return r
	}
	r := &batchedRecorder{
		db:              database,
		in:              make(chan Event, 4096),
		batchSize:       cfg.BatchSize,
		flushInterval:   cfg.BatchInterval,
		aggFiveMinutely: make(map[fiveMinuteKey]*counters),
		aggHourly:       make(map[hourlyKey]*counters),
		aggPkg:          make(map[pkgDailyKey]*counters),
		stop:            make(chan struct{}),
		done:            make(chan struct{}),
		flushReq:        make(chan chan struct{}),
	}
	go r.loop()
	return r
}

// nullRecorder is the rollup-disabled fallback. It uses one worker and a
// bounded queue to write raw access_log rows without ever touching the rollup
// tables. The queue keeps Record non-blocking without creating an unbounded
// number of goroutines when SQLite is slow.
type nullRecorder struct {
	db            *gorm.DB
	in            chan Event
	batchSize     int
	flushInterval time.Duration

	dropped atomic.Uint64

	// stateMu establishes the Close/Record boundary. Close takes the write
	// lock before marking the recorder closed, so every Record holding the
	// read lock has either enqueued or rejected its event before draining
	// starts. The input channel itself stays open, avoiding send/close races.
	stateMu sync.RWMutex
	closed  bool

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	flushReq chan chan struct{}
}

func (n *nullRecorder) Record(e Event) {
	n.stateMu.RLock()
	defer n.stateMu.RUnlock()

	if n.closed {
		n.dropped.Add(1)
		return
	}
	select {
	case n.in <- e:
	default:
		n.dropped.Add(1)
		zap.L().Warn("raw access log recorder channel full, dropping event",
			zap.Uint64("total_dropped", n.dropped.Load()))
	}
}

// Dropped exposes events rejected because the queue was full or the recorder
// was already closing. It is intentionally not part of Recorder: callers use
// it only for diagnostics and tests.
func (n *nullRecorder) Dropped() uint64 { return n.dropped.Load() }

func (n *nullRecorder) loop() {
	defer close(n.done)
	ticker := time.NewTicker(n.flushInterval)
	defer ticker.Stop()

	buf := make([]db.AccessLog, 0, n.batchSize)
	flush := func(ctx context.Context) {
		if len(buf) == 0 {
			return
		}
		batch := buf
		buf = make([]db.AccessLog, 0, n.batchSize)
		if err := n.db.WithContext(ctx).CreateInBatches(batch, 200).Error; err != nil {
			zap.L().Warn("failed to flush raw access logs",
				zap.Error(err),
				zap.Int("count", len(batch)))
		}
	}
	drain := func(ctx context.Context) {
		for {
			select {
			case e := <-n.in:
				buf = append(buf, toAccessLog(e))
			default:
				flush(ctx)
				return
			}
		}
	}

	for {
		select {
		case <-n.stop:
			drain(context.Background())
			return
		case e := <-n.in:
			buf = append(buf, toAccessLog(e))
			if len(buf) >= n.batchSize {
				flush(context.Background())
			}
		case done := <-n.flushReq:
			drain(context.Background())
			close(done)
		case <-ticker.C:
			flush(context.Background())
		}
	}
}

// Flush is a synchronous barrier for events accepted before the worker handles
// the request. If Close wins the race, worker completion provides the same
// guarantee because Close drains the entire accepted queue.
func (n *nullRecorder) Flush(ctx context.Context) error {
	n.stateMu.RLock()
	if n.closed {
		n.stateMu.RUnlock()
		select {
		case <-n.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	done := make(chan struct{})
	select {
	case n.flushReq <- done:
		n.stateMu.RUnlock()
	case <-n.done:
		n.stateMu.RUnlock()
		return nil
	case <-ctx.Done():
		n.stateMu.RUnlock()
		return ctx.Err()
	}

	select {
	case <-done:
		return nil
	case <-n.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close atomically stops new Record calls, asks the worker to drain everything
// accepted before that boundary, and waits for completion. A canceled context
// only bounds the caller's wait; the worker keeps draining so a later Close can
// still observe successful completion.
func (n *nullRecorder) Close(ctx context.Context) error {
	n.stopOnce.Do(func() {
		n.stateMu.Lock()
		n.closed = true
		close(n.stop)
		n.stateMu.Unlock()
	})
	select {
	case <-n.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// batchedRecorder is the production implementation: events arrive over a
// bounded channel, get folded into in-memory counter maps, and flush to
// SQLite in batches when either batchSize or flushInterval trips. The
// channel is bounded so the request path NEVER blocks — back-pressure
// drops events with a warn log instead of slowing the proxy.
type batchedRecorder struct {
	db            *gorm.DB
	in            chan Event
	batchSize     int
	flushInterval time.Duration

	dropped atomic.Uint64

	// stateMu establishes the same strict Close/Record boundary as the
	// raw-only recorder. Events accepted while holding the read lock are
	// guaranteed to be visible to the worker before Close begins draining.
	stateMu sync.RWMutex
	closed  bool

	mu              sync.Mutex
	rawBuf          []db.AccessLog
	aggFiveMinutely map[fiveMinuteKey]*counters
	aggHourly       map[hourlyKey]*counters
	aggPkg          map[pkgDailyKey]*counters

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}

	// flushReq carries a "done" channel that the loop closes after it has
	// drained any pending events from r.in and persisted everything. This
	// is how Flush() and tests get a synchronous barrier without changing
	// the non-blocking Record() contract.
	flushReq chan chan struct{}
}

// Dropped exposes the count of events the channel rejected. Useful for
// metrics; not in the Recorder interface to avoid leaking the implementation.
func (r *batchedRecorder) Dropped() uint64 { return r.dropped.Load() }

// Record never blocks. If the channel is full we drop the event and bump
// the counter; the loss is observable but not fatal (access logs aren't
// billing data — see ADR-0002 §Consequences).
func (r *batchedRecorder) Record(e Event) {
	r.stateMu.RLock()
	defer r.stateMu.RUnlock()

	if r.closed {
		r.dropped.Add(1)
		return
	}
	select {
	case r.in <- e:
	default:
		r.dropped.Add(1)
		zap.L().Warn("access log recorder channel full, dropping event",
			zap.Uint64("total_dropped", r.dropped.Load()))
	}
}

func (r *batchedRecorder) loop() {
	defer close(r.done)
	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stop:
			// Drain anything queued just before Close, flush, exit.
			r.drainAndFlush(context.Background())
			return
		case e := <-r.in:
			r.ingest(e)
			if r.rawLen() >= r.batchSize {
				r.flushAll(context.Background())
			}
		case done := <-r.flushReq:
			r.drainAndFlush(context.Background())
			close(done)
		case <-ticker.C:
			r.flushAll(context.Background())
		}
	}
}

// drainAndFlush pulls every event currently in r.in into the in-memory
// buffers, then flushes. Used by Flush() and by Close() so callers get a
// strict barrier without racing the ingest path.
func (r *batchedRecorder) drainAndFlush(ctx context.Context) {
	for {
		select {
		case e := <-r.in:
			r.ingest(e)
		default:
			r.flushAll(ctx)
			return
		}
	}
}

func (r *batchedRecorder) ingest(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rawBuf = append(r.rawBuf, toAccessLog(e))
	key := e.FiveMinuteKey()
	if r.aggFiveMinutely[key] == nil {
		r.aggFiveMinutely[key] = &counters{}
	}
	r.aggFiveMinutely[key].add(e)
	h := e.HourlyKey()
	if r.aggHourly[h] == nil {
		r.aggHourly[h] = &counters{}
	}
	r.aggHourly[h].add(e)
	if k, ok := e.PackageDailyKey(); ok {
		if r.aggPkg[k] == nil {
			r.aggPkg[k] = &counters{}
		}
		r.aggPkg[k].add(e)
	}
}

func (r *batchedRecorder) rawLen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rawBuf)
}

// flushAll swaps the in-memory buffers out under the mutex (so ingest
// doesn't block on disk I/O), then writes the snapshot to SQLite. A
// failure in any write is warn-logged but doesn't abort
// the others — partial progress is preferable to "all or nothing".
func (r *batchedRecorder) flushAll(ctx context.Context) {
	r.mu.Lock()
	rawBuf := r.rawBuf
	aggFive := r.aggFiveMinutely
	aggH := r.aggHourly
	aggP := r.aggPkg
	r.rawBuf = nil
	r.aggFiveMinutely = make(map[fiveMinuteKey]*counters)
	r.aggHourly = make(map[hourlyKey]*counters)
	r.aggPkg = make(map[pkgDailyKey]*counters)
	r.mu.Unlock()

	if len(rawBuf) > 0 {
		if err := r.db.WithContext(ctx).CreateInBatches(rawBuf, 200).Error; err != nil {
			zap.L().Warn("failed to flush raw access logs",
				zap.Error(err),
				zap.Int("count", len(rawBuf)))
		}
	}
	if len(aggFive) > 0 {
		if err := upsertFiveMinutely(ctx, r.db, aggFive); err != nil {
			zap.L().Warn("failed to upsert five-minute rollup", zap.Error(err))
		}
	}
	if len(aggH) > 0 {
		if err := upsertHourly(ctx, r.db, aggH); err != nil {
			zap.L().Warn("failed to upsert hourly rollup", zap.Error(err))
		}
	}
	if len(aggP) > 0 {
		if err := upsertPackageDaily(ctx, r.db, aggP); err != nil {
			zap.L().Warn("failed to upsert package daily rollup", zap.Error(err))
		}
	}
}

// Flush blocks until any pending events queued by Record() have been
// ingested and persisted. The recorder loop owns ingestion (because the
// counter maps aren't safe for concurrent mutation under flush), so we
// can't just call flushAll directly — we'd race the ingest path.
//
// Used by graceful shutdown and by tests that want a strict happens-before
// barrier ("Record … Flush … assert DB row count").
func (r *batchedRecorder) Flush(ctx context.Context) error {
	r.stateMu.RLock()
	if r.closed {
		r.stateMu.RUnlock()
		select {
		case <-r.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	done := make(chan struct{})
	select {
	case r.flushReq <- done:
		r.stateMu.RUnlock()
	case <-r.done:
		r.stateMu.RUnlock()
		return nil
	case <-ctx.Done():
		r.stateMu.RUnlock()
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close signals the loop to drain + flush + exit, then waits up to
// ctx.Done() for it. Returns ctx.Err() on timeout so a stuck flush
// surfaces to the caller instead of hanging shutdown.
func (r *batchedRecorder) Close(ctx context.Context) error {
	r.stopOnce.Do(func() {
		r.stateMu.Lock()
		r.closed = true
		close(r.stop)
		r.stateMu.Unlock()
	})
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func toAccessLog(e Event) db.AccessLog {
	return db.AccessLog{
		AdapterType: e.AdapterType,
		Method:      e.Method,
		CacheKey:    e.CacheKey,
		PackageName: e.PackageName,
		Hit:         e.Hit,
		Upstream:    e.Upstream,
		LatencyMs:   e.LatencyMs,
		StatusCode:  e.StatusCode,
		ClientIP:    e.ClientIP,
		BytesSent:   e.BytesSent,
		CreatedAt:   e.At.UTC(),
	}
}
