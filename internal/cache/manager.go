package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/cascade"
	"depsilo/internal/db"
	"depsilo/internal/traffic"
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

// Manager handles cache lookup, singleflight dedup, and storage writes.
// Strategy: refresh-before-serve for mutable metadata,
// stale-while-revalidate for immutable artifacts, plus offline fallback.
//   - Cache entries are NEVER deleted by TTL expiration.
//   - If cache exists and is fresh (< TTL): serve immediately (HIT).
//   - If mutable metadata is stale: synchronously refresh so resolvers see
//     newly published versions; fall back to stale bytes on upstream failure,
//     or when the refresh outlasts staleRefreshBudget while it continues.
//   - If an immutable artifact is stale: serve immediately (HIT), then
//     trigger background verification/refresh.
//   - If cache miss: open upstream, stream bytes to the client AND to storage
//     in parallel (proxy-like first-byte latency). If the client disconnects
//     mid-stream the upstream→storage pump keeps running so the cache still
//     fills. DB entry is committed only after storage write succeeds.
//   - If upstream open fails on miss but stale cache exists: serve stale (HIT).
type Manager struct {
	transfers       *traffic.Transfers
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
	// staleRefreshBudget bounds how long an ordinary request waits for a stale
	// metadata entry's refresh before it receives the stale copy.
	staleRefreshBudget time.Duration

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

const (
	hitUpdateQueueSize         = 4096
	hitUpdatePendingLimit      = 1024
	hitUpdateFlushBatchSize    = 256
	hitUpdateFlushInterval     = 50 * time.Millisecond
	hitUpdateRetryInitial      = 100 * time.Millisecond
	hitUpdateRetryMax          = 5 * time.Second
	hitUpdateCloseFlushTimeout = 5 * time.Second
	scanQueueSize              = 256
	scanWorkerCount            = 2
	refreshWorkerLimit         = 16
)

// NewManager creates a new cache manager. immutableThreshold is the TTL at or
// above which an artifact is eligible for tamper detection. Metadata is
// excluded by cache-key classification regardless of its configured TTL.
func NewManager(storage Storage, database *gorm.DB, eventBus *EventBus, immutableThreshold time.Duration) *Manager {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	m := &Manager{
		transfers:           traffic.NewTransfers(),
		storage:             storage,
		db:                  database,
		eventBus:            eventBus,
		inflight:            make(map[string]*inflightFetch),
		mutations:           newKeyMutationGate(lifecycleCtx),
		invalidations:       newCacheInvalidationRegistry(),
		invalidationTimeout: 10 * time.Second,
		staleRefreshBudget:  defaultStaleRefreshBudget,
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

// SetTamperRecorder attaches the optional content-integrity recorder.
// Pass nil to disable tamper detection (the streaming SHA-256 is still
// computed by countingReader — it is cheap relative to the network I/O
// on the miss/refresh paths — but is never consulted).
func (m *Manager) SetTamperRecorder(r TamperRecorder) {
	m.componentMu.Lock()
	defer m.componentMu.Unlock()
	m.tamper = r
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

// relayFetchKind maps the cache manager's metadata/artifact classification
// onto the cascade wire kind so a parent cache node applies the same
// synchronous-refresh policy as the child that requested the entry.
func relayFetchKind(adapterType, key string) string {
	if db.ClassifyCacheKind(adapterType, key) == db.CacheKindMetadata {
		return cascade.KindIndex
	}
	return cascade.KindArtifact
}

func (m *Manager) cacheKind(adapterType, key string) string {
	return db.ClassifyCacheKind(adapterType, key)
}

// FetchFunc is called on cache miss to fetch data from upstream.
// Implementations should return the chosen upstream's display name as
// `upstreamName` so the access log records which upstream served the miss
// (used by the Admin Access Logs page UPSTREAM column).
type FetchFunc func(ctx context.Context) (body io.ReadCloser, contentType string, size int64, upstreamName string, err error)

var errCacheEntryRemovedDuringRefresh = errors.New("cache entry removed during refresh")
var errCacheWriteSuperseded = errors.New("cache write superseded by authoritative invalidation")

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

// TransferSnapshots reports bounded, process-local cache-fill activity.
func (m *Manager) TransferSnapshots() []traffic.Transfer { return m.transfers.Snapshot() }
