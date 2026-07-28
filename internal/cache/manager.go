package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
)

// countingReader wraps an io.Reader, counts bytes read through it, and
// computes a streaming SHA-256 of everything it reads. Used to size
// bodies when upstream omits Content-Length AND to fingerprint content
// for tamper detection — both come free in the single pass the storage
// pump already makes.
type countingReader struct {
	r io.Reader
	n int64
	h hash.Hash
}

func NewCountingReader(r io.Reader) *countingReader {
	return &countingReader{r: r, h: sha256.New()}
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.n += int64(n)
		_, _ = cr.h.Write(p[:n]) // hash.Hash.Write never returns an error
	}
	return n, err
}

func (cr *countingReader) BytesRead() int64 {
	return cr.n
}

// SumHex returns the lowercase-hex SHA-256 of all bytes read so far.
// Call after the reader is fully drained.
func (cr *countingReader) SumHex() string {
	return hex.EncodeToString(cr.h.Sum(nil))
}

// SecurityScanner is the optional contract for scanning new packages on miss.
type SecurityScanner interface {
	ScanPackage(ctx context.Context, ecosystem, packageName string) error
}

// Manager handles cache lookup, singleflight dedup, and storage writes.
// Strategy: refresh-before-serve for mutable metadata,
// stale-while-revalidate for immutable artifacts, plus offline fallback.
//   - Cache entries are NEVER deleted by TTL expiration.
//   - If cache exists and is fresh (< TTL): serve immediately (HIT).
//   - If mutable metadata is stale: synchronously refresh so resolvers see
//     newly published versions; fall back to stale bytes on upstream failure.
//   - If an immutable artifact is stale: serve immediately (HIT), then
//     trigger background verification/refresh.
//   - If cache miss: open upstream, stream bytes to the client AND to storage
//     in parallel (proxy-like first-byte latency). If the client disconnects
//     mid-stream the upstream→storage pump keeps running so the cache still
//     fills. DB entry is committed only after storage write succeeds.
//   - If upstream open fails on miss but stale cache exists: serve stale (HIT).
type Manager struct {
	storage         Storage
	db              *gorm.DB
	eventBus        *EventBus
	securityScanner SecurityScanner

	tamper             TamperRecorder
	immutableThreshold time.Duration

	inflightMu          sync.Mutex
	inflight            map[string]*inflightFetch
	mutations           *keyMutationGate
	invalidations       *cacheInvalidationRegistry
	invalidationTimeout time.Duration

	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	lifecycleMu     sync.Mutex
	lifecycleClosed bool
	lifecycleWG     sync.WaitGroup
	lifecycleDone   chan struct{}

	componentMu  sync.RWMutex
	hitUpdates   chan hitCountUpdate
	scanQueue    chan packageScan
	scanMu       sync.Mutex
	scanPending  map[string]struct{}
	refreshMu    sync.Mutex
	refreshing   map[string]struct{}
	refreshSlots chan struct{}
}

type hitCountUpdate struct {
	entryID      uint
	lastAccessed time.Time
}

type packageScan struct {
	ecosystem   string
	packageName string
	key         string
}

const (
	hitUpdateQueueSize = 4096
	scanQueueSize      = 256
	scanWorkerCount    = 2
	refreshWorkerLimit = 16
)

// inflightFetch tracks a miss-path fetch that is currently streaming. Followers
// (concurrent requests for the same key) block on done until the storage write
// commits, then read from cache. Same semantics as a singleflight that releases
// when storage is durable — not when the leader's client finishes reading.
type inflightFetch struct {
	done           chan struct{}
	err            error
	committedEntry *db.CacheEntry
	refreshOutcome RefreshOutcome
	trackers       []*RefreshTracker // guarded by Manager.inflightMu
}

// SetSecurityScanner attaches an optional security scanner. Pass nil to detach.
func (m *Manager) SetSecurityScanner(s SecurityScanner) {
	m.componentMu.Lock()
	defer m.componentMu.Unlock()
	m.securityScanner = s
}

// NewManager creates a new cache manager. immutableThreshold is the TTL at or
// above which an artifact is eligible for tamper detection. Metadata is
// excluded by cache-key classification regardless of its configured TTL.
func NewManager(storage Storage, database *gorm.DB, eventBus *EventBus, immutableThreshold time.Duration) *Manager {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	m := &Manager{
		storage:             storage,
		db:                  database,
		eventBus:            eventBus,
		inflight:            make(map[string]*inflightFetch),
		mutations:           newKeyMutationGate(lifecycleCtx),
		invalidations:       newCacheInvalidationRegistry(),
		invalidationTimeout: 10 * time.Second,
		immutableThreshold:  immutableThreshold,
		lifecycleCtx:        lifecycleCtx,
		lifecycleCancel:     lifecycleCancel,
		lifecycleDone:       make(chan struct{}),
		hitUpdates:          make(chan hitCountUpdate, hitUpdateQueueSize),
		scanQueue:           make(chan packageScan, scanQueueSize),
		scanPending:         make(map[string]struct{}),
		refreshing:          make(map[string]struct{}),
		refreshSlots:        make(chan struct{}, refreshWorkerLimit),
	}
	m.goOwned(m.runHitCountWorker)
	for range scanWorkerCount {
		m.goOwned(m.runScanWorker)
	}
	return m
}

// ErrManagerClosed is returned when an operation would have to start new
// manager-owned work after shutdown has begun.
var ErrManagerClosed = errors.New("cache manager is closed")

// beginOwned reserves manager-owned work without starting a goroutine. It is
// used while a miss opens its upstream synchronously, so Close waits until the
// caller either hands the work off to the pump/store goroutines or releases the
// inflight reservation. Add and the closed check share lifecycleMu, preventing
// an Add/Wait race with Close.
func (m *Manager) beginOwned() (context.Context, func(), bool) {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycleClosed {
		return m.lifecycleCtx, func() {}, false
	}
	m.lifecycleWG.Add(1)
	return m.lifecycleCtx, m.lifecycleWG.Done, true
}

// goOwned starts one or more related manager-owned goroutines atomically. A
// related set (notably the upstream pump and storage writer) is either started
// in full or not started at all, so shutdown cannot strand one end of a pipe.
func (m *Manager) goOwned(tasks ...func(context.Context)) bool {
	if len(tasks) == 0 {
		return true
	}
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycleClosed {
		return false
	}
	m.lifecycleWG.Add(len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			defer m.lifecycleWG.Done()
			task(m.lifecycleCtx)
		}()
	}
	return true
}

// Close rejects new manager-owned work, cancels active work, and waits for all
// work that was admitted before the close boundary. It is safe to call Close
// concurrently or repeatedly. The caller's context bounds only the wait; a
// later Close can continue waiting for the same shutdown.
func (m *Manager) Close(ctx context.Context) error {
	m.lifecycleMu.Lock()
	firstClose := !m.lifecycleClosed
	if firstClose {
		m.lifecycleClosed = true
		// No future Add can pass the closed check after this point, so Wait is
		// safe to begin once lifecycleMu is released.
		go func() {
			m.lifecycleWG.Wait()
			close(m.lifecycleDone)
		}()
	}
	m.lifecycleMu.Unlock()

	// Cancel outside lifecycleMu: cancellation callbacks may promptly finish
	// tasks which in turn attempt a nested goOwned (for example a security
	// scan after a commit).
	m.lifecycleCancel()

	select {
	case <-m.lifecycleDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// bindLifecycle creates a context cancelled by either the caller or Manager
// shutdown. It is used by Prefetch, whose fill is explicitly operation-bound.
func (m *Manager) bindLifecycle(ctx context.Context) (context.Context, context.CancelFunc) {
	bound, cancel := context.WithCancel(m.lifecycleCtx)
	stopCaller := context.AfterFunc(ctx, cancel)
	return bound, func() {
		stopCaller()
		cancel()
	}
}

func (m *Manager) recordHit(entryID uint, lastAccessed time.Time) bool {
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycleClosed {
		return false
	}
	select {
	case m.hitUpdates <- hitCountUpdate{entryID: entryID, lastAccessed: lastAccessed}:
		return true
	default:
		// Hit statistics are advisory. Keep the request path non-blocking when
		// SQLite is slow; a later hit will still advance last_accessed.
		return false
	}
}

type mergedHitCount struct {
	count        int64
	lastAccessed time.Time
}

func mergeHitCount(pending map[uint]mergedHitCount, update hitCountUpdate) {
	merged := pending[update.entryID]
	merged.count++
	if update.lastAccessed.After(merged.lastAccessed) {
		merged.lastAccessed = update.lastAccessed
	}
	pending[update.entryID] = merged
}

func (m *Manager) flushHitCounts(ctx context.Context, pending map[uint]mergedHitCount) error {
	if len(pending) == 0 {
		return nil
	}
	err := m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for entryID, update := range pending {
			if err := tx.Model(&db.CacheEntry{}).
				Where("id = ?", entryID).
				Updates(map[string]interface{}{
					"hit_count":     gorm.Expr("hit_count + ?", update.count),
					"last_accessed": update.lastAccessed,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	clear(pending)
	return nil
}

func (m *Manager) runHitCountWorker(ctx context.Context) {
	const flushInterval = 50 * time.Millisecond
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	pending := make(map[uint]mergedHitCount)
	flush := func(flushCtx context.Context) {
		if err := m.flushHitCounts(flushCtx, pending); err != nil {
			zap.L().Debug("cache hit-count batch update failed", zap.Error(err))
		}
	}

	for {
		select {
		case update := <-m.hitUpdates:
			mergeHitCount(pending, update)
			if len(pending) >= 256 {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		case <-ctx.Done():
			// recordHit and Close serialize on lifecycleMu. Once cancellation is
			// observed, no producer can add another accepted event, so draining
			// until empty has a stable boundary.
			for {
				select {
				case update := <-m.hitUpdates:
					mergeHitCount(pending, update)
				default:
					flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					flush(flushCtx)
					cancel()
					return
				}
			}
		}
	}
}

func (m *Manager) enqueueScan(ecosystem, packageName string) bool {
	if packageName == "" || m.scanner() == nil {
		return false
	}
	key := ecosystem + "\x00" + packageName
	m.lifecycleMu.Lock()
	defer m.lifecycleMu.Unlock()
	if m.lifecycleClosed {
		return false
	}
	m.scanMu.Lock()
	defer m.scanMu.Unlock()
	if _, exists := m.scanPending[key]; exists {
		return false
	}
	job := packageScan{ecosystem: ecosystem, packageName: packageName, key: key}
	m.scanPending[key] = struct{}{}
	select {
	case m.scanQueue <- job:
		return true
	default:
		delete(m.scanPending, key)
		return false
	}
}

func (m *Manager) finishScan(job packageScan) {
	m.scanMu.Lock()
	delete(m.scanPending, job.key)
	m.scanMu.Unlock()
}

func (m *Manager) processScan(job packageScan) {
	defer m.finishScan(job)
	ctx, finishOwned, admitted := m.beginOwned()
	if !admitted {
		return
	}
	defer finishOwned()
	scanner := m.scanner()
	if scanner == nil {
		return
	}
	if err := scanner.ScanPackage(ctx, job.ecosystem, job.packageName); err != nil {
		zap.L().Debug("security scan for new package failed", zap.Error(err))
	}
}

func (m *Manager) runScanWorker(ctx context.Context) {
	for {
		select {
		case job := <-m.scanQueue:
			m.processScan(job)
		case <-ctx.Done():
			// Queued jobs have not started and are intentionally discarded at
			// shutdown. Active calls receive the cancelled lifecycle context and
			// remain covered by lifecycleWG.
			for {
				select {
				case job := <-m.scanQueue:
					m.finishScan(job)
				default:
					return
				}
			}
		}
	}
}

func (m *Manager) scheduleBackgroundRefresh(key, adapterType string, ttl time.Duration, fetchFn FetchFunc, timeoutHint ...time.Duration) bool {
	fetchTimeout := defaultFetchTimeout
	fetchIdleTimeout := time.Duration(0)
	if len(timeoutHint) > 0 {
		fetchTimeout = timeoutHint[0]
	}
	if len(timeoutHint) > 1 {
		fetchIdleTimeout = timeoutHint[1]
	}
	m.refreshMu.Lock()
	if _, exists := m.refreshing[key]; exists {
		m.refreshMu.Unlock()
		return false
	}
	select {
	case m.refreshSlots <- struct{}{}:
	default:
		m.refreshMu.Unlock()
		return false
	}
	m.refreshing[key] = struct{}{}
	m.refreshMu.Unlock()

	started := m.goOwned(func(ctx context.Context) {
		defer func() {
			m.refreshMu.Lock()
			delete(m.refreshing, key)
			<-m.refreshSlots
			m.refreshMu.Unlock()
		}()
		refreshCtx := WithFetchTimeout(ctx, fetchTimeout)
		refreshCtx = WithFetchIdleTimeout(refreshCtx, fetchIdleTimeout)
		m.backgroundRefresh(refreshCtx, key, adapterType, ttl, fetchFn)
	})
	if !started {
		m.refreshMu.Lock()
		delete(m.refreshing, key)
		<-m.refreshSlots
		m.refreshMu.Unlock()
	}
	return started
}

// SetTamperRecorder attaches the optional content-integrity recorder.
// Pass nil to disable tamper detection (the streaming SHA-256 is still
// computed by countingReader — it is cheap relative to the network I/O
// on the miss/refresh paths — but is never consulted).
func (m *Manager) SetTamperRecorder(r TamperRecorder) {
	m.componentMu.Lock()
	defer m.componentMu.Unlock()
	m.tamper = r
}

func (m *Manager) scanner() SecurityScanner {
	m.componentMu.RLock()
	defer m.componentMu.RUnlock()
	return m.securityScanner
}

func (m *Manager) tamperRecorder() TamperRecorder {
	m.componentMu.RLock()
	defer m.componentMu.RUnlock()
	return m.tamper
}

// isImmutable reports whether a TTL marks an artifact as immutable.
func (m *Manager) isImmutable(ttl time.Duration) bool {
	return m.immutableThreshold > 0 && ttl >= m.immutableThreshold
}

// isMutableMetadata identifies index/metadata responses from the adapter's
// generated cache-key shape. Classification is deliberately independent from
// TTL so unusual but valid operator settings cannot disable index refresh.
func (m *Manager) isMutableMetadata(adapterType, key string) bool {
	return db.ClassifyCacheKind(adapterType, key) == db.CacheKindMetadata
}

func (m *Manager) cacheKind(adapterType, key string) string {
	return db.ClassifyCacheKind(adapterType, key)
}

// FetchFunc is called on cache miss to fetch data from upstream.
// Implementations should return the chosen upstream's display name as
// `upstreamName` so the access log records which upstream served the miss
// (used by the Admin Access Logs page UPSTREAM column).
type FetchFunc func(ctx context.Context) (body io.ReadCloser, contentType string, size int64, upstreamName string, err error)

type forceRefreshContextKey struct{}

type forceRefreshRequest struct {
	forced       bool
	contextBound bool
	tracker      *RefreshTracker
}

// RefreshTracker reports when a refresh or prefetch has durably completed. It
// is attached to the inflight storage operation by Manager.Get, allowing admin
// callers to wait for slow local/S3 writes without polling the database.
type RefreshTracker struct {
	once    sync.Once
	done    chan struct{}
	mu      sync.Mutex
	err     error
	outcome RefreshOutcome
	used    bool
}

// RefreshOutcome identifies the Upstream used by a tracked refresh and
// whether its conditional request returned HTTP 304.
type RefreshOutcome struct {
	Upstream    string
	NotModified bool
}

func newRefreshTracker() *RefreshTracker {
	return &RefreshTracker{done: make(chan struct{})}
}

// WithForceRefresh marks an internal request as an operator-triggered cache
// refresh. Adapters keep their normal fetch/rewrite logic; Manager simply
// bypasses a fresh mutable-metadata hit for this request.
func WithForceRefresh(ctx context.Context) context.Context {
	return context.WithValue(ctx, forceRefreshContextKey{}, forceRefreshRequest{forced: true})
}

// WithTrackedForceRefresh additionally returns a completion handle for the
// durable cache write performed by the forced request.
func WithTrackedForceRefresh(ctx context.Context) (context.Context, *RefreshTracker) {
	tracker := newRefreshTracker()
	return context.WithValue(ctx, forceRefreshContextKey{}, forceRefreshRequest{forced: true, contextBound: true, tracker: tracker}), tracker
}

func forceRefreshRequested(ctx context.Context) bool {
	return refreshRequestFrom(ctx).forced
}

func refreshTrackerFrom(ctx context.Context) *RefreshTracker {
	return refreshRequestFrom(ctx).tracker
}

func refreshRequestFrom(ctx context.Context) forceRefreshRequest {
	request, _ := ctx.Value(forceRefreshContextKey{}).(forceRefreshRequest)
	return request
}

func persistenceRequired(ctx context.Context) bool {
	request := refreshRequestFrom(ctx)
	return request.contextBound || request.forced
}

func withTrackedPrefetch(ctx context.Context) (context.Context, *RefreshTracker) {
	tracker := newRefreshTracker()
	request := refreshRequestFrom(ctx)
	request.contextBound = true
	request.tracker = tracker
	return context.WithValue(ctx, forceRefreshContextKey{}, request), tracker
}

func (t *RefreshTracker) markUsed() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.used = true
	t.mu.Unlock()
}

func (t *RefreshTracker) complete(outcome RefreshOutcome, err error) {
	if t == nil {
		return
	}
	t.once.Do(func() {
		t.mu.Lock()
		t.err = err
		t.outcome = outcome
		t.mu.Unlock()
		close(t.done)
	})
}

// Used reports whether the request reached a cache refresh operation.
func (t *RefreshTracker) Used() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.used
}

// Wait blocks until the forced refresh's cache write and DB update are both
// complete. A conditional HTTP 304 is a successful refresh.
func (t *RefreshTracker) Wait(ctx context.Context) error {
	_, err := t.Outcome(ctx)
	return err
}

// Outcome waits for the durable refresh and returns its selected Upstream and
// conditional-request result. ErrNotModified is represented by NotModified
// and remains a successful outcome.
func (t *RefreshTracker) Outcome(ctx context.Context) (RefreshOutcome, error) {
	if t == nil {
		return RefreshOutcome{}, errors.New("nil refresh tracker")
	}
	select {
	case <-t.done:
	case <-ctx.Done():
		return RefreshOutcome{}, ctx.Err()
	}
	t.mu.Lock()
	err := t.err
	outcome := t.outcome
	t.mu.Unlock()
	if errors.Is(err, ErrNotModified) {
		return outcome, nil
	}
	return outcome, err
}

// ErrNotModified lets a conditional FetchFunc report HTTP 304 without turning
// it into an upstream failure. Manager extends the cached entry's TTL and
// serves its existing bytes.
var ErrNotModified = errors.New("upstream metadata not modified")

// ResponseValidators are persisted with a cache entry and reused by adapters
// for conditional metadata requests.
type ResponseValidators struct {
	ETag         string
	LastModified string
}

// WithResponseValidators annotates a fetched body without changing FetchFunc's
// public signature. Manager extracts and persists the validators while the
// wrapped body otherwise behaves exactly like the original reader.
func WithResponseValidators(body io.ReadCloser, etag, lastModified string) io.ReadCloser {
	return WithResponseMetadata(body, http.Header{
		"ETag":          {etag},
		"Last-Modified": {lastModified},
	})
}

func responseValidatorsFrom(body io.ReadCloser) ResponseValidators {
	return responseValidatorsFromMetadata(responseMetadataFrom(body))
}

func responseValidatorsFromMetadata(headers http.Header) ResponseValidators {
	return ResponseValidators{
		ETag:         headers.Get("ETag"),
		LastModified: headers.Get("Last-Modified"),
	}
}

// GetResult holds the result of a cache get operation.
// Upstream is populated on miss only — hits do not contact any upstream.
type GetResult struct {
	Reader      io.ReadCloser
	ContentType string
	Size        int64
	Hit         bool
	Upstream    string
	Headers     http.Header
}

// Get implements policy-aware caching:
//  1. Cache fresh → return immediately
//  2. Mutable metadata stale → refresh synchronously; stale-if-error
//  3. Immutable artifact stale → return immediately + background verify
//  4. Cache miss → fetch from upstream → store → return
//  5. Upstream fail + stale cache exists → return stale cache
func (m *Manager) Get(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	return m.get(ctx, key, adapterType, ttl, fetchFn)
}

// Prefetch resolves a cache entry without returning its body to a downstream
// client. On a miss it drains and closes Get's streaming reader, then waits
// until both storage and cache metadata are durable before returning. A fresh
// hit is consumed normally and does not force an upstream refresh.
func (m *Manager) Prefetch(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	operationCtx, operationCancel := m.bindLifecycle(ctx)
	defer operationCancel()
	if err := operationCtx.Err(); err != nil {
		return err
	}
	// bindLifecycle deliberately detaches values from the caller context. Carry
	// explicit transfer-timeout hints across that lifecycle seam.
	operationCtx = WithFetchTimeout(operationCtx, fetchTimeoutFrom(ctx))
	operationCtx = WithFetchIdleTimeout(operationCtx, fetchIdleTimeoutFrom(ctx))

	trackedCtx, tracker := withTrackedPrefetch(operationCtx)
	result, err := m.Get(trackedCtx, key, adapterType, ttl, fetchFn)
	if err != nil {
		return err
	}
	if result == nil || result.Reader == nil {
		return errors.New("cache prefetch returned no reader")
	}

	// Get deliberately detaches an ordinary client stream from its request so
	// a disconnect does not abandon a cache fill. Prefetch is itself the only
	// consumer, so close its reader when the operation context is cancelled.
	// The context-bound fetch/store path below then unwinds the inflight entry.
	closeDone := make(chan struct{})
	stopClose := context.AfterFunc(operationCtx, func() {
		_ = result.Reader.Close()
		close(closeDone)
	})
	_, drainErr := io.Copy(io.Discard, result.Reader)
	if !stopClose() {
		<-closeDone
	}
	closeErr := result.Reader.Close()

	var commitErr error
	if tracker.Used() {
		commitErr = tracker.Wait(operationCtx)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if lifecycleErr := m.lifecycleCtx.Err(); lifecycleErr != nil {
		return lifecycleErr
	}
	return errors.Join(
		wrapPrefetchError("drain cache response", drainErr),
		wrapPrefetchError("close cache response", closeErr),
		wrapPrefetchError("commit cache entry", commitErr),
	)
}

func wrapPrefetchError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("prefetch %s: %w", operation, err)
}

func (m *Manager) get(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	// Followers must join before waiting on the publication gate: the writer
	// holds that gate until object and metadata commit, while followers are
	// intentionally waiting for that same commit.
	var result *GetResult
	var err error
	joined := false
	exists := false
	forced := forceRefreshRequested(ctx)
	m.inflightMu.Lock()
	if flight, ok := m.inflight[key]; ok {
		if tracker := refreshTrackerFrom(ctx); tracker != nil {
			tracker.markUsed()
			flight.trackers = append(flight.trackers, tracker)
		}
		m.inflightMu.Unlock()
		result, err = m.followInflight(ctx, m.lifecycleCtx, key, flight)
		joined = true
		// A refresh flight may have started from an existing entry. Let the
		// common error path attempt a stale snapshot without requiring another
		// unlocked metadata preflight.
		exists = true
	} else {
		m.inflightMu.Unlock()
	}

	// Open the metadata row and storage reader under the same per-key gate used
	// by writers. Storage.Get returns a stable object snapshot (an open file or
	// S3 response body), so the gate can be released before downstream drains
	// it while still preventing new-body/old-metadata combinations.
	//
	// A forced refresh never serves this snapshot, so it must not wait behind
	// an older writer before contacting upstream. In particular, an
	// authoritative upstream response must be able to publish its tombstone
	// immediately and bound the subsequent cleanup wait.
	if !joined && !forced {
		unlockRead, lockErr := m.lockSnapshot(ctx, key)
		if lockErr != nil {
			return nil, lockErr
		}
		if m.invalidations.readable(key) {
			var entry db.CacheEntry
			dbErr := m.db.WithContext(ctx).Where("key = ?", key).First(&entry).Error
			if dbErr == nil {
				var existsErr error
				exists, existsErr = m.storage.Exists(ctx, entry.StoragePath)
				if existsErr != nil {
					zap.L().Warn("cache exists check failed", zap.String("key", key), zap.Error(existsErr))
					exists = false
				}
			}
			if dbErr == nil && exists {
				isFresh := time.Now().Before(entry.ExpiresAt)
				// Serving an expired package index before refreshing it makes a new
				// release invisible for exactly one install attempt: pip receives the
				// old version list, while the background refresh only helps the next
				// build. Fall through to fetchAndStore for mutable metadata so this
				// request receives the current index. The common error path below still
				// serves this stale entry when the upstream is unavailable.
				if !isFresh && m.isMutableMetadata(adapterType, key) {
					zap.L().Debug("metadata requires synchronous refresh",
						zap.String("key", key),
						zap.String("adapter_type", adapterType),
					)
				} else {
					// Serve fresh cache, or stale immutable content while it is
					// verified/refreshed in the background.
					if m.invalidations.readable(key) {
						reader, size, readErr := m.storage.Get(ctx, entry.StoragePath)
						if readErr == nil {
							unlockRead()
							// Keep the hit path free of SQLite writes and per-request
							// goroutines. The single bounded worker merges counts by entry ID.
							m.recordHit(entry.ID, time.Now())

							zap.L().Debug("cache hit",
								zap.String("key", key),
								zap.Bool("fresh", isFresh),
							)

							m.publishEvent(key, adapterType, true, 0)

							// Only immutable/long-TTL entries reach this stale branch.
							if !isFresh {
								m.scheduleBackgroundRefresh(
									key,
									adapterType,
									ttl,
									fetchFn,
									fetchTimeoutFrom(ctx),
									fetchIdleTimeoutFrom(ctx),
								)
							}

							return &GetResult{
								Reader:      reader,
								ContentType: entry.ContentType,
								Size:        size,
								Hit:         true,
								Headers:     decodeStoredResponseMetadata(entry.ResponseHeaders, entry.ETag, entry.LastModified),
							}, nil
						}
						zap.L().Warn("cache read failed despite exists", zap.String("key", key), zap.Error(readErr))
					}
				}
			}
		}
		unlockRead()
	}

	// Cache miss — fetch from upstream via singleflight
	if !joined {
		result, err = m.fetchAndStore(ctx, key, adapterType, ttl, fetchFn)
	}
	if err != nil {
		if errors.Is(err, ErrNotModified) && (exists || forced) {
			unlock, lockErr := m.lockMutation(ctx, key)
			if lockErr != nil {
				return nil, lockErr
			}
			entry, entryErr := m.refreshEntry(ctx, key)
			if entryErr != nil {
				unlock()
				return nil, entryErr
			}
			now := time.Now()
			update := m.db.WithContext(ctx).Model(&db.CacheEntry{}).Where("id = ? AND key = ?", entry.ID, key).Updates(map[string]interface{}{
				"expires_at": now.Add(ttl),
			})
			updateErr := refreshMetadataUpdateError(update, "cache validator update")
			unlock()
			if updateErr != nil {
				zap.L().Warn("cache validator touch failed", zap.String("key", key), zap.Error(updateErr))
				if forceRefreshRequested(ctx) || refreshTrackerFrom(ctx) != nil {
					return nil, updateErr
				}
			}
			return m.serveStale(ctx, key, adapterType)
		}
		if !allowStaleFallback(err) {
			return nil, err
		}
		// An operator-triggered refresh must report an upstream failure to the
		// admin UI. The old cache entry is deliberately left untouched so normal
		// traffic can continue to use it; automatic expiry refreshes retain the
		// stale-if-error behavior below.
		if forceRefreshRequested(ctx) {
			return nil, err
		}
		// A persistence-only failure must not turn a healthy upstream response
		// into a failed package install. A stale object is the cheapest fallback;
		// without one, fetch directly for this caller and skip the unavailable
		// cache path. Persistence-required Prefetch/refresh callers are handled by
		// the force-refresh branch above or retain the error below.
		if isCachePersistenceError(err) && !persistenceRequired(ctx) {
			if exists {
				zap.L().Warn("cache persistence failed while refreshing; serving stale cache",
					zap.String("key", key),
					zap.Error(err),
				)
				if stale, staleErr := m.serveStale(ctx, key, adapterType); staleErr == nil {
					return stale, nil
				} else {
					zap.L().Warn("stale cache unavailable after persistence failure; bypassing cache",
						zap.String("key", key),
						zap.Error(staleErr),
					)
				}
			}
			return m.fetchPassthrough(ctx, fetchFn)
		}
		// Transient upstream failures may use stale data as a last resort.
		// Authoritative errors (for example a package becoming private or being
		// removed) must reach the client instead of being rewritten to stale 200.
		if exists && allowStaleFallback(err) {
			zap.L().Warn("upstream failed, serving stale cache",
				zap.String("key", key),
				zap.Error(err),
			)
			if stale, staleErr := m.serveStale(ctx, key, adapterType); staleErr == nil {
				return stale, nil
			} else {
				// A joined miss has no unlocked metadata preflight, so
				// "exists" only means stale data might be available. Preserve
				// the real upstream failure when that optimistic lookup misses.
				zap.L().Debug("stale cache unavailable after upstream failure",
					zap.String("key", key),
					zap.Error(staleErr),
				)
			}
		}
		return nil, err
	}

	return result, nil
}

// fetchPassthrough gives an ordinary follower an independent upstream stream
// after the flight it joined failed only at the cache persistence layer. It
// deliberately skips another cache attempt: during a storage outage, repeatedly
// serialising followers behind doomed fills creates long tail latency.
func (m *Manager) fetchPassthrough(ctx context.Context, fetchFn FetchFunc) (*GetResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, finishOwned, admitted := m.beginOwned()
	if !admitted {
		return nil, ErrManagerClosed
	}
	operationCtx, operationCancel := m.bindLifecycle(ctx)
	fetchCtx, fetchCancel, cancelFetchWithCause := newFetchContext(operationCtx, fetchTimeoutFrom(ctx))
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			fetchCancel()
			operationCancel()
			finishOwned()
		})
	}
	body, contentType, size, upstreamName, err := fetchFn(fetchCtx)
	if err != nil {
		if body != nil {
			_ = body.Close()
		}
		cleanup()
		return nil, err
	}
	if body == nil {
		cleanup()
		return nil, errors.New("upstream returned a nil body")
	}
	headers := responseMetadataFrom(body)
	body = WithBodyIdleTimeout(body, fetchIdleTimeoutFrom(ctx), cancelFetchWithCause)
	stopContextClose := context.AfterFunc(fetchCtx, func() {
		_ = body.Close()
		cleanup()
	})
	return &GetResult{
		Reader: &cancelOnDoneReadCloser{
			ReadCloser: body,
			onDone: func() {
				// If the context callback has already started, it owns cleanup
				// and releases the lifecycle reservation only after body.Close
				// returns. Releasing here would let Manager.Close return while
				// that callback is still running.
				if stopContextClose() {
					cleanup()
				}
			},
		},
		ContentType: contentType,
		Size:        size,
		Hit:         false,
		Upstream:    upstreamName,
		Headers:     cloneResponseMetadata(headers),
	}, nil
}

// fetchAndStore opens the upstream and returns a reader that streams bytes to
// the client AS they arrive (proxy-like first-byte latency), while a background
// goroutine simultaneously drains the same stream into storage and commits the
// DB entry on success.
//
// Concurrency model — a custom inflight tracker (not singleflight.Group) is
// used because singleflight.Do blocks until its callback returns, which is
// incompatible with returning the live reader to the leader before the
// upstream→storage pump is finished. Semantics match the original spec:
//
//   - Leader (first request for the key) gets the live stream.
//   - Followers wait for a durable commit, then read from cache. If only
//     persistence fails, ordinary followers use stale bytes or an independent
//     pass-through; persistence-required operations retain the commit error.
//   - Client disconnect does not abort a healthy cache fill. If storage has
//     already failed too, the pump stops because no useful consumer remains.
//   - Upstream open failure: synchronous error to leader. Followers, if any,
//     wake up with the same error and propagate.
//   - Upstream mid-stream failure: leader's client read surfaces the error;
//     storage.Put returns error; no DB entry is committed (LocalStorage's
//     tmp+rename pattern also leaves no orphan file). On the next request the
//     key is treated as a miss again.
//   - Storage/DB failure: a normal leader continues streaming upstream bytes;
//     Prefetch and tracked/forced refresh remain fail-closed.
func (m *Manager) fetchAndStore(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	lifecycleCtx, finishOwned, admitted := m.beginOwned()
	if !admitted {
		return nil, ErrManagerClosed
	}
	defer finishOwned()

	request := refreshRequestFrom(ctx)
	tracker := request.tracker
	m.inflightMu.Lock()
	if flight, ok := m.inflight[key]; ok {
		if tracker != nil {
			tracker.markUsed()
			flight.trackers = append(flight.trackers, tracker)
		}
		m.inflightMu.Unlock()
		return m.followInflight(ctx, lifecycleCtx, key, flight)
	}
	flight := &inflightFetch{done: make(chan struct{})}
	if tracker != nil {
		tracker.markUsed()
		flight.trackers = append(flight.trackers, tracker)
	}
	m.inflight[key] = flight
	m.inflightMu.Unlock()
	writeGeneration := m.invalidations.beginWrite(key)

	// Open upstream synchronously so connect/auth errors surface to the caller
	// before we hand back a reader. A 10-minute ceiling matches the previous
	// behavior. Ordinary client requests are detached because the pump must
	// outlive a disconnect; a Prefetch has no downstream consumer and binds the
	// fill to its operation context so cancellation can unwind the inflight slot.
	fetchBase := lifecycleCtx
	storeCtx := lifecycleCtx
	if request.contextBound {
		fetchBase = ctx
		storeCtx = ctx
	}
	fetchCtx, fetchCancel, cancelFetchWithCause := newFetchContext(fetchBase, fetchTimeoutFrom(ctx))
	body, contentType, size, upstreamName, err := fetchFn(fetchCtx)
	flight.refreshOutcome = RefreshOutcome{
		Upstream:    upstreamName,
		NotModified: errors.Is(err, ErrNotModified),
	}
	if err != nil {
		// Publish an authoritative result only after its tombstone is visible and
		// cleanup has been attempted. Releasing the flight first would let a new
		// request fresh-hit revoked bytes in the gap before get() invalidated them.
		if !allowStaleFallback(err) {
			if invalidateErr := m.invalidateCachedEntry(ctx, key); invalidateErr != nil {
				zap.L().Error("failed to invalidate cache after authoritative upstream response",
					zap.String("key", key),
					zap.Error(invalidateErr),
				)
			}
		}
		if body != nil {
			_ = body.Close()
		}
		fetchCancel()
		m.invalidations.abortWrite(key)
		m.releaseInflight(key, flight, err)
		return nil, err
	}
	if body == nil {
		fetchCancel()
		err = errors.New("upstream returned a nil body")
		m.invalidations.abortWrite(key)
		m.releaseInflight(key, flight, err)
		return nil, err
	}
	headers := responseMetadataFrom(body)
	validators := responseValidatorsFromMetadata(headers)
	if err := storeCtx.Err(); err != nil {
		_ = body.Close()
		fetchCancel()
		m.invalidations.abortWrite(key)
		m.releaseInflight(key, flight, err)
		return nil, err
	}
	body = WithBodyIdleTimeout(body, fetchIdleTimeoutFrom(ctx), cancelFetchWithCause)

	// Two detachable pipes fan out from a single source. Either consumer may
	// disappear independently: a client disconnect still allows a healthy cache
	// fill, while a cache backend failure still allows a healthy download.
	clientPipeR, clientW := io.Pipe()
	storageR, storageW := io.Pipe()
	streamState := newMissStreamState(fetchCancel, persistenceRequired(ctx))
	storeOperationCtx, cancelStore := context.WithCancelCause(storeCtx)
	storageFanout := newMissStorageFanout(storageW, streamState, cancelStore)
	clientR := &observedPipeReader{
		PipeReader: clientPipeR,
		onDone:     streamState.downstreamClosed,
	}

	// The upstream/client pump, bounded storage fan-out, and persistence writer
	// are admitted as one lifecycle unit. Shutdown can therefore never start
	// only part of the pipeline and strand a pipe or queue.
	started := m.goOwned(
		func(context.Context) {
			m.pumpUpstream(fetchCtx, body, fetchCancel, streamState, storageFanout, clientW)
		},
		func(context.Context) {
			storageFanout.run(storeOperationCtx)
		},
		func(context.Context) {
			defer cancelStore(nil)
			m.storeAndCommit(storeOperationCtx, streamState, key, adapterType, ttl, contentType, size, validators, headers, storageR, flight, writeGeneration)
		},
	)
	if !started {
		startErr := storeCtx.Err()
		if startErr == nil {
			startErr = ErrManagerClosed
		}
		_ = body.Close()
		fetchCancel()
		cancelStore(startErr)
		storageFanout.closeInput(startErr)
		_ = storageW.CloseWithError(startErr)
		_ = clientW.CloseWithError(startErr)
		_ = storageR.CloseWithError(startErr)
		_ = clientPipeR.CloseWithError(startErr)
		m.invalidations.abortWrite(key)
		m.releaseInflight(key, flight, startErr)
		return nil, startErr
	}

	return &GetResult{
		Reader:      clientR,
		ContentType: contentType,
		Size:        size,
		Hit:         false,
		Upstream:    upstreamName,
		Headers:     cloneResponseMetadata(headers),
	}, nil
}

// followInflight waits for the leader to commit storage, then serves from cache.
func (m *Manager) followInflight(ctx, lifecycleCtx context.Context, key string, flight *inflightFetch) (*GetResult, error) {
	select {
	case <-flight.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lifecycleCtx.Done():
		return nil, lifecycleCtx.Err()
	}
	if flight.err != nil {
		return nil, flight.err
	}
	if flight.committedEntry == nil {
		return nil, errors.New("cache leader completed without committed metadata")
	}
	entry := *flight.committedEntry
	unlock, lockErr := m.lockSnapshot(ctx, key)
	if lockErr != nil {
		return nil, lockErr
	}
	if !m.invalidations.readable(key) {
		unlock()
		return nil, ErrCacheMiss
	}
	reader, size, gerr := m.storage.Get(ctx, entry.StoragePath)
	unlock()
	if gerr != nil {
		return nil, fmt.Errorf("read from cache after leader commit: %w", gerr)
	}
	m.recordHit(entry.ID, time.Now())
	m.publishEvent(key, entry.AdapterType, true, 0)
	return &GetResult{
		Reader:      reader,
		ContentType: entry.ContentType,
		Size:        size,
		Hit:         true,
		Headers:     decodeStoredResponseMetadata(entry.ResponseHeaders, entry.ETag, entry.LastModified),
	}, nil
}

// pumpUpstream writes the client synchronously (preserving bounded
// backpressure for a slow End User) and hands storage-owned chunks to a small
// independent queue. A stuck cache sink is detached after the queue's bounded
// grace period, while the client continues without full-body buffering.
func (m *Manager) pumpUpstream(
	ctx context.Context,
	body io.ReadCloser,
	fetchCancel context.CancelFunc,
	state *missStreamState,
	storage *missStorageFanout,
	clientW *io.PipeWriter,
) {
	cancelDone := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		err := fetchBodyContextError(ctx, body)
		if errors.Is(err, ErrFetchIdleTimeout) {
			state.recordUpstreamTransferFailure(err)
		}
		_ = body.Close()
		storage.detach(err)
		_ = clientW.CloseWithError(err)
		close(cancelDone)
	})
	defer func() {
		storage.closeInput(nil)
		if stopCancel() {
			_ = body.Close()
			fetchCancel()
			return
		}
		fetchCancel()
		<-cancelDone
	}()

	clientActive := true
	storageActive := true
	for {
		chunk := acquireMissStreamChunk()
		n, rerr := body.Read(chunk)
		storageOwnsChunk := false
		if n > 0 {
			// Downstream delivery is the primary operation. This write normally
			// has an active reader in the adapter's io.Copy and therefore gives
			// pip the first bytes before cache persistence can apply backpressure.
			if clientActive {
				if _, werr := clientW.Write(chunk[:n]); werr != nil {
					clientActive = false
					state.downstreamClosed()
				}
			}
			if storageActive {
				waitForStorage := state.storageBackpressureRequired()
				if storage.enqueue(ctx, chunk[:n], waitForStorage) {
					storageOwnsChunk = true
				} else {
					storageActive = false
				}
			}
			if !storageOwnsChunk {
				releaseMissStreamChunk(chunk)
			}
			if !clientActive && !storageActive {
				return
			}
		} else {
			releaseMissStreamChunk(chunk)
		}
		if rerr != nil {
			if rerr == io.EOF {
				storage.closeInput(nil)
				if clientActive {
					_ = clientW.Close()
				}
			} else {
				if ctxErr := ctx.Err(); ctxErr != nil {
					rerr = fetchBodyContextError(ctx, body)
				} else {
					state.recordUpstreamTransferFailure(rerr)
				}
				storage.closeInput(rerr)
				if clientActive {
					_ = clientW.CloseWithError(rerr)
				}
			}
			return
		}
	}
}

// storeAndCommit drains the storage pipe into storage.Put and, on success,
// upserts the CacheEntry row and publishes the cache event. Releases the
// inflight slot when done so followers can read from cache.
func (m *Manager) storeAndCommit(ctx context.Context, state *missStreamState, key, adapterType string, ttl time.Duration, contentType string, declaredSize int64, validators ResponseValidators, headers http.Header, storageR *io.PipeReader, flight *inflightFetch, writeGeneration uint64) {
	writeFinished := false
	defer func() {
		if !writeFinished {
			m.invalidations.abortWrite(key)
		}
	}()
	unlock, lockErr := m.lockMutation(ctx, key)
	if lockErr != nil {
		ctxErr := ctx.Err()
		cancelCause := context.Cause(ctx)
		_ = storageR.CloseWithError(lockErr)
		state.storageFailed()
		if cancelCause != nil &&
			!errors.Is(cancelCause, context.Canceled) &&
			!errors.Is(cancelCause, context.DeadlineExceeded) {
			m.logCacheStorageBackpressure(key, cancelCause)
			m.releaseInflight(key, flight, &cachePersistenceError{cause: cancelCause})
		} else if ctxErr != nil {
			m.releaseInflight(key, flight, ctxErr)
		} else {
			m.releaseInflight(key, flight, &cachePersistenceError{cause: lockErr})
		}
		return
	}
	defer unlock()
	if !m.invalidations.writeCurrent(key, writeGeneration) {
		_ = storageR.CloseWithError(errCacheWriteSuperseded)
		state.storageFailed()
		cleanupErr := m.discardSupersededWrite(ctx, key)
		m.releaseInflight(key, flight, &cachePersistenceError{cause: errors.Join(errCacheWriteSuperseded, cleanupErr)})
		return
	}

	cr := NewCountingReader(storageR)
	var storageReader io.Reader = cr
	var exactReader *exactSizeReader
	if declaredSize >= 0 {
		exactReader = newExactSizeReader(cr, declaredSize)
		storageReader = exactReader
	}
	putErr := m.storage.Put(ctx, key, storageReader, declaredSize, contentType)
	putSucceeded := putErr == nil
	if putErr == nil && exactReader != nil {
		putErr = exactReader.validateConsumed()
	}
	if putErr != nil {
		ctxErr := ctx.Err()
		cancelCause := context.Cause(ctx)
		if putSucceeded {
			// A backend that returned success without consuming the complete
			// representation may already have published unsafe bytes. Remove
			// both halves under the mutation gate instead of leaving an older
			// metadata row pointing at the newly written object.
			putErr = m.removeUncommittedObject(ctx, key, putErr)
		}
		// Detach the failed cache consumer. Normal downloads continue through the
		// client pipe; persistence-required Prefetch/refresh operations cancel via
		// missStreamState because they have no useful result without a commit.
		_ = storageR.CloseWithError(putErr)
		state.storageFailed()
		if upstreamErr := state.upstreamFailure(); upstreamErr != nil {
			m.releaseInflight(key, flight, upstreamErr)
			return
		}
		if cancelCause != nil &&
			!errors.Is(cancelCause, context.Canceled) &&
			!errors.Is(cancelCause, context.DeadlineExceeded) {
			m.logCacheStorageBackpressure(key, cancelCause)
			m.releaseInflight(key, flight, &cachePersistenceError{cause: cancelCause})
			return
		}
		if ctxErr != nil {
			m.releaseInflight(key, flight, ctxErr)
			return
		}
		persistenceErr := &cachePersistenceError{cause: putErr}
		zap.L().Warn("cache put failed; downstream remains in pass-through mode",
			zap.String("key", key),
			zap.Error(putErr),
		)
		m.releaseInflight(key, flight, persistenceErr)
		return
	}
	if !m.invalidations.writeCurrent(key, writeGeneration) {
		cleanupErr := m.discardSupersededWrite(ctx, key)
		m.releaseInflight(key, flight, &cachePersistenceError{cause: errors.Join(errCacheWriteSuperseded, cleanupErr)})
		return
	}

	size := declaredSize
	if size < 0 {
		size = cr.BytesRead()
	}
	now := time.Now()
	entry := db.CacheEntry{
		Key:             key,
		AdapterType:     adapterType,
		CacheKind:       m.cacheKind(adapterType, key),
		StoragePath:     key,
		Size:            size,
		HitCount:        0,
		ContentType:     contentType,
		ETag:            validators.ETag,
		LastModified:    validators.LastModified,
		ResponseHeaders: encodeResponseMetadata(headers),
		PackageName:     packagekey.ExtractName(adapterType, key),
		ExpiresAt:       now.Add(ttl),
		LastAccessed:    now,
	}
	persist := m.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "key"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"adapter_type":     adapterType,
				"storage_path":     key,
				"size":             size,
				"content_type":     contentType,
				"etag":             validators.ETag,
				"last_modified":    validators.LastModified,
				"response_headers": encodeResponseMetadata(headers),
				"cache_kind":       m.cacheKind(adapterType, key),
				"package_name":     packagekey.ExtractName(adapterType, key),
				"expires_at":       now.Add(ttl),
				"last_accessed":    now,
				"updated_at":       now,
			}),
		}).
		Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}}).
		Create(&entry)
	if persist.Error != nil {
		commitErr := m.removeUncommittedObject(
			ctx,
			key,
			fmt.Errorf("persist cache metadata: %w", persist.Error),
		)
		zap.L().Warn("cache DB commit failed; uncommitted object cleanup attempted",
			zap.String("key", key),
			zap.Error(commitErr),
		)
		m.releaseInflight(key, flight, &cachePersistenceError{cause: commitErr})
		return
	}
	if entry.ID == 0 {
		// RETURNING is supported by the production SQLite/PostgreSQL dialects.
		// Keep a defensive lookup for dialects or drivers that accept the clause
		// without populating the destination model.
		var identity struct {
			ID uint
		}
		if identityErr := m.db.WithContext(ctx).
			Model(&db.CacheEntry{}).
			Select("id").
			Where("key = ?", key).
			Scan(&identity).Error; identityErr != nil || identity.ID == 0 {
			if identityErr == nil {
				identityErr = errors.New("persisted cache metadata has no identity")
			}
			zap.L().Warn("cache metadata identity lookup failed",
				zap.String("key", key),
				zap.Error(identityErr),
			)
			m.releaseInflight(
				key,
				flight,
				&cachePersistenceError{cause: identityErr},
			)
			return
		}
		entry.ID = identity.ID
	}

	// Tamper baseline: first-seen SHA-256 of immutable artifacts.
	if tamper := m.tamperRecorder(); tamper != nil &&
		m.cacheKind(adapterType, key) == db.CacheKindArtifact &&
		m.tamperEligible(adapterType, key, ttl) {
		pkgName := packagekey.ExtractName(adapterType, key)
		// Verify (not Record): on a miss where a baseline already exists
		// — e.g. the artifact was LRU-evicted and re-fetched — a differing
		// hash is tampering and must alert. Alert-only here: the prior
		// bytes are already gone from storage, so we can't preserve them,
		// but the operator learns the cached copy changed. clientIP is
		// unknown at the store layer (the leader may have disconnected).
		_ = tamper.Verify(ctx, key, adapterType, pkgName,
			versionFromKey(adapterType, key), cr.SumHex(), size, "")
	}
	if !m.invalidations.commitWrite(key, writeGeneration) {
		cleanupErr := m.discardSupersededWrite(ctx, key)
		m.releaseInflight(key, flight, &cachePersistenceError{cause: errors.Join(errCacheWriteSuperseded, cleanupErr)})
		return
	}
	writeFinished = true

	m.publishEvent(key, adapterType, false, size)

	m.enqueueScan(adapterType, packagekey.ExtractName(adapterType, key))

	m.releaseInflight(key, flight, nil, entry)
}

func (m *Manager) logCacheStorageBackpressure(key string, cause error) {
	if !errors.Is(cause, errCacheStorageBackpressure) {
		return
	}
	zap.L().Warn("cache persistence detached after sustained storage backpressure",
		zap.String("key", key),
		zap.Duration("grace_period", missStorageBackpressureTTL),
		zap.Int("queue_bytes", missStorageQueueDepth*missStreamChunkSize),
		zap.Error(cause),
	)
}

func (m *Manager) lockMutation(ctx context.Context, key string) (func(), error) {
	if m.mutations == nil {
		return nil, errors.New("cache mutation gate is not initialized")
	}
	return m.mutations.lock(ctx, key)
}

func (m *Manager) lockSnapshot(ctx context.Context, key string) (func(), error) {
	if m.mutations == nil {
		return nil, errors.New("cache mutation gate is not initialized")
	}
	return m.mutations.lockSnapshot(ctx, key)
}

// Invalidate makes a cache key unreadable before removing its metadata and
// object. If neither backend can be cleaned, the in-memory tombstone remains
// fail-closed until a successful replacement write or process restart.
func (m *Manager) Invalidate(ctx context.Context, key string) error {
	if m == nil {
		return ErrManagerClosed
	}
	if key == "" {
		return errors.New("cache invalidation key is empty")
	}
	return m.invalidateCachedEntry(ctx, key)
}

// removeUncommittedObject compensates for the lack of a transaction spanning
// SQLite and Local/S3 storage. The caller holds the key mutation gate, so this
// delete cannot remove a newer fill. A detached, bounded context gives S3 a
// chance to remove bytes even when the failed DB operation exhausted or
// cancelled the original operation context.
func (m *Manager) removeUncommittedObject(ctx context.Context, key string, commitErr error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	generation := m.invalidations.beginInvalidation(key)
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	deleteResult := m.db.WithContext(cleanupCtx).
		Where("key = ?", key).
		Delete(&db.CacheEntry{})
	dbErr := deleteResult.Error
	storageErr := m.storage.Delete(cleanupCtx, key)
	cleanupSafe := dbErr == nil || storageErr == nil
	m.invalidations.finishInvalidation(key, generation, cleanupSafe)

	if dbErr != nil {
		dbErr = fmt.Errorf("remove uncommitted cache metadata: %w", dbErr)
	}
	if storageErr != nil {
		storageErr = fmt.Errorf("remove uncommitted cache object: %w", storageErr)
		zap.L().Error("failed to remove uncommitted cache object",
			zap.String("key", key),
			zap.Error(storageErr),
		)
	}
	return errors.Join(commitErr, dbErr, storageErr)
}

// invalidateCachedEntry makes an authoritative upstream result sticky across
// later transient failures. Metadata is removed even when object cleanup
// fails, and object cleanup is still attempted when the DB is unavailable; in
// either case the old representation cannot be served through a valid row.
func (m *Manager) invalidateCachedEntry(ctx context.Context, key string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	generation := m.invalidations.beginInvalidation(key)
	cleanupSafe := false
	defer func() {
		m.invalidations.finishInvalidation(key, generation, cleanupSafe)
	}()

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), m.invalidationTimeout)
	defer cancel()
	unlock, err := m.lockMutation(cleanupCtx, key)
	if err != nil {
		return err
	}
	defer unlock()
	if !m.invalidations.invalidationPending(key, generation) {
		return nil
	}

	deleteResult := m.db.WithContext(cleanupCtx).
		Where("key = ?", key).
		Delete(&db.CacheEntry{})
	dbErr := deleteResult.Error
	storageErr := m.storage.Delete(cleanupCtx, key)
	cleanupSafe = dbErr == nil || storageErr == nil
	if dbErr != nil {
		dbErr = fmt.Errorf("delete invalid cache metadata: %w", dbErr)
	}
	if storageErr != nil {
		storageErr = fmt.Errorf("delete invalid cache object: %w", storageErr)
	}
	return errors.Join(dbErr, storageErr)
}

// releaseInflight removes the in-flight entry and closes done so followers can
// proceed. err is recorded on the flight before done closes.
func (m *Manager) releaseInflight(key string, flight *inflightFetch, err error, committed ...db.CacheEntry) {
	m.inflightMu.Lock()
	flight.err = err
	if len(committed) > 0 {
		entry := committed[0]
		flight.committedEntry = &entry
	}
	if cur, ok := m.inflight[key]; ok && cur == flight {
		delete(m.inflight, key)
	}
	trackers := append([]*RefreshTracker(nil), flight.trackers...)
	outcome := flight.refreshOutcome
	m.inflightMu.Unlock()
	for _, tracker := range trackers {
		tracker.complete(outcome, err)
	}
	close(flight.done)
}

// backgroundRefresh refreshes a stale cache entry in the background. Admission
// is deduplicated by scheduleBackgroundRefresh before this task is started.
func (m *Manager) backgroundRefresh(parentCtx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) {
	writeGeneration := m.invalidations.beginWrite(key)
	writeFinished := false
	defer func() {
		if !writeFinished {
			m.invalidations.abortWrite(key)
		}
	}()
	ctx, cancel, cancelFetchWithCause := newFetchContext(parentCtx, fetchTimeoutFrom(parentCtx))
	defer cancel()

	err := func() error {
		body, contentType, size, _, err := fetchFn(ctx)
		if err != nil {
			if body != nil {
				_ = body.Close()
			}
			return err
		}
		if body == nil {
			return errors.New("background refresh returned a nil body")
		}
		headers := responseMetadataFrom(body)
		validators := responseValidatorsFromMetadata(headers)
		body = WithBodyIdleTimeout(body, fetchIdleTimeoutFrom(ctx), cancelFetchWithCause)
		defer body.Close()
		closeDone := make(chan struct{})
		stopClose := context.AfterFunc(ctx, func() {
			_ = body.Close()
			close(closeDone)
		})
		defer func() {
			if !stopClose() {
				<-closeDone
			}
		}()

		cr := NewCountingReader(body)
		var refreshReader io.Reader = cr
		var exactReader *exactSizeReader
		if size >= 0 {
			exactReader = newExactSizeReader(cr, size)
			refreshReader = exactReader
		}

		// Immutable + tamper on: verify-only. Drain to discard (do NOT
		// overwrite storage — immutable bytes are supposed to be
		// identical), then compare the hash. A mismatch keeps the
		// trusted first-seen copy and alerts; a match just extends TTL.
		if tamper := m.tamperRecorder(); tamper != nil &&
			m.cacheKind(adapterType, key) == db.CacheKindArtifact &&
			m.tamperEligible(adapterType, key, ttl) {
			if _, copyErr := io.Copy(io.Discard, refreshReader); copyErr != nil {
				return fmt.Errorf("bg verify read: %w", copyErr)
			}
			unlock, lockErr := m.lockMutation(ctx, key)
			if lockErr != nil {
				return lockErr
			}
			defer unlock()
			if !m.invalidations.writeCurrent(key, writeGeneration) {
				return errors.Join(errCacheWriteSuperseded, m.discardSupersededWrite(ctx, key))
			}
			entry, entryErr := m.refreshEntry(ctx, key)
			if entryErr != nil {
				return entryErr
			}
			pkgName := packagekey.ExtractName(adapterType, key)
			res := tamper.Verify(ctx, key, adapterType, pkgName,
				versionFromKey(adapterType, key), cr.SumHex(), cr.BytesRead(), "")
			if res.KnownMismatch {
				zap.L().Warn("tamper: refusing to overwrite tampered artifact", zap.String("key", key))
				// Keep first-seen bytes (no storage.Put). Advance the TTL
				// anyway so this stale entry stops re-triggering a refresh
				// on every request — otherwise a hot tampered package would
				// re-download the artifact and re-fire the critical webhook
				// per request. Re-verification (and the alert) now recurs at
				// most once per TTL.
				now := time.Now()
				update := m.db.WithContext(ctx).Model(&db.CacheEntry{}).Where("id = ? AND key = ?", entry.ID, key).Updates(map[string]interface{}{
					"expires_at":    now.Add(ttl),
					"last_accessed": now,
					"cache_kind":    m.cacheKind(adapterType, key),
				})
				if err := refreshMetadataUpdateError(update, "bg tamper TTL update"); err != nil {
					return err
				}
				if !m.invalidations.commitWrite(key, writeGeneration) {
					return errors.Join(errCacheWriteSuperseded, m.discardSupersededWrite(ctx, key))
				}
				writeFinished = true
				return nil
			}
			now := time.Now()
			update := m.db.WithContext(ctx).Model(&db.CacheEntry{}).Where("id = ? AND key = ?", entry.ID, key).Updates(map[string]interface{}{
				"expires_at":    now.Add(ttl),
				"last_accessed": now,
				"cache_kind":    m.cacheKind(adapterType, key),
			})
			if err := refreshMetadataUpdateError(update, "bg integrity TTL update"); err != nil {
				return err
			}
			if !m.invalidations.commitWrite(key, writeGeneration) {
				return errors.Join(errCacheWriteSuperseded, m.discardSupersededWrite(ctx, key))
			}
			writeFinished = true
			zap.L().Debug("tamper: integrity verified", zap.String("key", key))
			return nil
		}

		// Mutable (or tamper off): normal refresh — overwrite storage.
		unlock, lockErr := m.lockMutation(ctx, key)
		if lockErr != nil {
			return lockErr
		}
		defer unlock()
		if !m.invalidations.writeCurrent(key, writeGeneration) {
			return errors.Join(errCacheWriteSuperseded, m.discardSupersededWrite(ctx, key))
		}
		entry, entryErr := m.refreshEntry(ctx, key)
		if entryErr != nil {
			return entryErr
		}
		putErr := m.storage.Put(ctx, key, refreshReader, size, contentType)
		putSucceeded := putErr == nil
		if putErr == nil && exactReader != nil {
			putErr = exactReader.validateConsumed()
		}
		if putErr != nil {
			putErr = fmt.Errorf("bg refresh put: %w", putErr)
			if putSucceeded {
				return m.removeUncommittedObject(ctx, key, putErr)
			}
			return putErr
		}
		if size < 0 {
			size = cr.BytesRead()
		}
		now := time.Now()
		update := m.db.WithContext(ctx).Model(&db.CacheEntry{}).Where("id = ? AND key = ?", entry.ID, key).Updates(map[string]interface{}{
			"size":             size,
			"content_type":     contentType,
			"etag":             validators.ETag,
			"last_modified":    validators.LastModified,
			"response_headers": encodeResponseMetadata(headers),
			"cache_kind":       m.cacheKind(adapterType, key),
			"expires_at":       now.Add(ttl),
			"last_accessed":    now,
		})
		if err := refreshMetadataUpdateError(update, "bg refresh metadata update"); err != nil {
			return m.removeUncommittedObject(ctx, key, err)
		}
		if !m.invalidations.commitWrite(key, writeGeneration) {
			return errors.Join(errCacheWriteSuperseded, m.discardSupersededWrite(ctx, key))
		}
		writeFinished = true

		zap.L().Debug("background refresh done", zap.String("key", key))
		return nil
	}()

	if errors.Is(err, errCacheEntryRemovedDuringRefresh) {
		zap.L().Debug("background refresh skipped after cache entry removal", zap.String("key", key))
	} else if err != nil {
		if !allowStaleFallback(err) {
			if invalidateErr := m.invalidateCachedEntry(parentCtx, key); invalidateErr != nil {
				zap.L().Error("failed to invalidate background cache after authoritative upstream response",
					zap.String("key", key),
					zap.Error(invalidateErr),
				)
			}
		}
		zap.L().Warn("background refresh failed",
			zap.String("key", key),
			zap.Error(err),
		)
	}
}

var errCacheEntryRemovedDuringRefresh = errors.New("cache entry removed during refresh")
var errCacheWriteSuperseded = errors.New("cache write superseded by authoritative invalidation")

// discardSupersededWrite runs while the caller holds the key mutation gate.
// It removes both halves of a write that lost the generation race.
func (m *Manager) discardSupersededWrite(ctx context.Context, key string) error {
	deleteResult := m.db.WithContext(ctx).Where("key = ?", key).Delete(&db.CacheEntry{})
	var dbErr error
	if deleteResult.Error != nil {
		dbErr = fmt.Errorf("delete superseded cache metadata: %w", deleteResult.Error)
	}
	var storageErr error
	if err := m.storage.Delete(ctx, key); err != nil {
		storageErr = fmt.Errorf("delete superseded cache object: %w", err)
	}
	if dbErr == nil || storageErr == nil {
		m.invalidations.markCleanupSafe(key)
	}
	return errors.Join(dbErr, storageErr)
}

func (m *Manager) refreshEntry(ctx context.Context, key string) (db.CacheEntry, error) {
	var entry db.CacheEntry
	if err := m.db.WithContext(ctx).Where("key = ?", key).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return db.CacheEntry{}, errCacheEntryRemovedDuringRefresh
		}
		return db.CacheEntry{}, fmt.Errorf("reload cache metadata before refresh: %w", err)
	}
	return entry, nil
}

func refreshMetadataUpdateError(update *gorm.DB, operation string) error {
	if update.Error != nil {
		return fmt.Errorf("%s: %w", operation, update.Error)
	}
	if update.RowsAffected == 0 {
		return fmt.Errorf("%s: %w", operation, errCacheEntryRemovedDuringRefresh)
	}
	return nil
}

// serveStale returns a stale cached version when upstream is unavailable.
func (m *Manager) serveStale(ctx context.Context, key string, adapterType string) (*GetResult, error) {
	unlock, lockErr := m.lockSnapshot(ctx, key)
	if lockErr != nil {
		return nil, lockErr
	}
	defer unlock()

	if !m.invalidations.readable(key) {
		return nil, fmt.Errorf("stale cache for %s was authoritatively invalidated", key)
	}
	var entry db.CacheEntry
	if err := m.db.WithContext(ctx).Where("key = ?", key).First(&entry).Error; err != nil {
		return nil, fmt.Errorf("no stale cache for %s", key)
	}

	if !m.invalidations.readable(key) {
		return nil, fmt.Errorf("stale cache for %s was authoritatively invalidated", key)
	}
	reader, size, err := m.storage.Get(ctx, entry.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("stale cache read failed: %w", err)
	}

	m.recordHit(entry.ID, time.Now())

	m.publishEvent(key, adapterType, true, 0)

	return &GetResult{
		Reader:      reader,
		ContentType: entry.ContentType,
		Size:        size,
		Hit:         true,
		Headers:     decodeStoredResponseMetadata(entry.ResponseHeaders, entry.ETag, entry.LastModified),
	}, nil
}

// publishEvent sends a cache event to the SSE bus.
func (m *Manager) publishEvent(key, adapterType string, hit bool, size int64) {
	if m.eventBus == nil {
		return
	}
	parts := strings.Split(key, "/")
	m.eventBus.Publish(CacheEvent{
		Time:        time.Now(),
		PackageName: packagekey.ExtractName(adapterType, key),
		FileName:    parts[len(parts)-1],
		AdapterType: adapterType,
		Hit:         hit,
		Size:        size,
	})
}

// versionFromKey is a best-effort version string for tamper event
// readability. The cache key already encodes the artifact; we only need
// something human-facing, so an empty result is acceptable.
func versionFromKey(adapterType, key string) string {
	if version := packagekey.ExtractVersion(adapterType, key); version != "" {
		return version
	}
	// Not every ecosystem parser can recover a version. Keep the historical
	// filename fallback for those adapters; the cache key remains authoritative.
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return ""
}
