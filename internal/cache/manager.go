package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

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
// Strategy: stale-while-revalidate + offline fallback.
//   - Cache entries are NEVER deleted by TTL expiration.
//   - If cache exists and is fresh (< TTL): serve immediately (HIT).
//   - If cache exists but is stale (>= TTL): serve immediately (HIT),
//     then trigger a background refresh from upstream.
//   - If cache miss: open upstream, stream bytes to the client AND to storage
//     in parallel (proxy-like first-byte latency). If the client disconnects
//     mid-stream the upstream→storage pump keeps running so the cache still
//     fills. DB entry is committed only after storage write succeeds.
//   - If upstream open fails on miss but stale cache exists: serve stale (HIT).
type Manager struct {
	storage         Storage
	db              *gorm.DB
	group           singleflight.Group // used by backgroundRefresh only
	eventBus        *EventBus
	securityScanner SecurityScanner

	tamper             TamperRecorder
	immutableThreshold time.Duration

	inflightMu sync.Mutex
	inflight   map[string]*inflightFetch
}

// inflightFetch tracks a miss-path fetch that is currently streaming. Followers
// (concurrent requests for the same key) block on done until the storage write
// commits, then read from cache. Same semantics as a singleflight that releases
// when storage is durable — not when the leader's client finishes reading.
type inflightFetch struct {
	done chan struct{}
	err  error
}

// SetSecurityScanner attaches an optional security scanner. Pass nil to detach.
func (m *Manager) SetSecurityScanner(s SecurityScanner) {
	m.securityScanner = s
}

// NewManager creates a new cache manager. immutableThreshold is the TTL
// at or above which an artifact is treated as immutable for tamper
// detection (metadata uses short TTLs, blobs long ones). Pass e.g. 1h.
func NewManager(storage Storage, database *gorm.DB, eventBus *EventBus, immutableThreshold time.Duration) *Manager {
	return &Manager{
		storage:            storage,
		db:                 database,
		eventBus:           eventBus,
		inflight:           make(map[string]*inflightFetch),
		immutableThreshold: immutableThreshold,
	}
}

// SetTamperRecorder attaches the optional content-integrity recorder.
// Pass nil to disable tamper detection (the streaming SHA-256 is still
// computed by countingReader — it is cheap relative to the network I/O
// on the miss/refresh paths — but is never consulted).
func (m *Manager) SetTamperRecorder(r TamperRecorder) { m.tamper = r }

// isImmutable reports whether a TTL marks an artifact as immutable.
func (m *Manager) isImmutable(ttl time.Duration) bool {
	return m.immutableThreshold > 0 && ttl >= m.immutableThreshold
}

// FetchFunc is called on cache miss to fetch data from upstream.
// Implementations should return the chosen upstream's display name as
// `upstreamName` so the access log records which upstream served the miss
// (used by the Admin Access Logs page UPSTREAM column).
type FetchFunc func(ctx context.Context) (body io.ReadCloser, contentType string, size int64, upstreamName string, err error)

// GetResult holds the result of a cache get operation.
// Upstream is populated on miss only — hits do not contact any upstream.
type GetResult struct {
	Reader      io.ReadCloser
	ContentType string
	Size        int64
	Hit         bool
	Upstream    string
}

// Get implements stale-while-revalidate caching:
//  1. Cache fresh → return immediately
//  2. Cache stale → return immediately + background refresh
//  3. Cache miss  → fetch from upstream → store → return
//  4. Upstream fail + stale cache exists → return stale cache
func (m *Manager) Get(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	// Check if we have a cached version (fresh or stale)
	exists, err := m.storage.Exists(ctx, key)
	if err != nil {
		zap.L().Warn("cache exists check failed", zap.String("key", key), zap.Error(err))
	}

	if exists {
		var entry db.CacheEntry
		dbErr := m.db.Where("key = ?", key).First(&entry).Error

		if dbErr == nil {
			isFresh := time.Now().Before(entry.ExpiresAt)

			// Serve from cache (both fresh and stale)
			reader, size, readErr := m.storage.Get(ctx, key)
			if readErr == nil {
				// Fire-and-forget hit-count update. The synchronous version put a
				// SQLite UPDATE on every cache-hit critical path; under fanout
				// (e.g. `pip install` resolving 10+ deps in parallel) the writes
				// serialized on the WAL and added 100-800ms tail latency to a
				// path that should be < 10ms. A handful of dropped counter ticks
				// on shutdown is acceptable — the cache content itself is safe
				// in storage and re-derived on next access.
				entryID := entry.ID
				now := time.Now()
				go func() {
					if err := m.db.Model(&db.CacheEntry{}).
						Where("id = ?", entryID).
						Updates(map[string]interface{}{
							"hit_count":     gorm.Expr("hit_count + 1"),
							"last_accessed": now,
						}).Error; err != nil {
						zap.L().Debug("cache hit-count update failed", zap.Error(err))
					}
				}()

				zap.L().Debug("cache hit",
					zap.String("key", key),
					zap.Bool("fresh", isFresh),
				)

				m.publishEvent(key, adapterType, true, 0)

				// If stale, trigger background refresh (non-blocking)
				if !isFresh {
					go m.backgroundRefresh(key, adapterType, ttl, fetchFn)
				}

				return &GetResult{
					Reader:      reader,
					ContentType: entry.ContentType,
					Size:        size,
					Hit:         true,
				}, nil
			}
			zap.L().Warn("cache read failed despite exists", zap.String("key", key), zap.Error(readErr))
		}
	}

	// Cache miss — fetch from upstream via singleflight
	result, err := m.fetchAndStore(ctx, key, adapterType, ttl, fetchFn)
	if err != nil {
		// Upstream failed — try serving stale cache as last resort
		if exists {
			zap.L().Warn("upstream failed, serving stale cache",
				zap.String("key", key),
				zap.Error(err),
			)
			return m.serveStale(ctx, key, adapterType)
		}
		return nil, err
	}

	return result, nil
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
//   - Followers (concurrent requests for the same key) wait on done, then read
//     from cache. The done channel is closed only after storage.Put commits and
//     the DB entry is written — not when the leader's client finishes reading.
//   - Client disconnect on the leader does NOT abort the pump. The cache still
//     fills and the DB entry is committed.
//   - Upstream open failure: synchronous error to leader. Followers, if any,
//     wake up with the same error and propagate.
//   - Upstream mid-stream failure: leader's client read surfaces the error;
//     storage.Put returns error; no DB entry is committed (LocalStorage's
//     tmp+rename pattern also leaves no orphan file). On the next request the
//     key is treated as a miss again.
func (m *Manager) fetchAndStore(ctx context.Context, key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	m.inflightMu.Lock()
	if flight, ok := m.inflight[key]; ok {
		m.inflightMu.Unlock()
		return m.followInflight(ctx, key, flight)
	}
	flight := &inflightFetch{done: make(chan struct{})}
	m.inflight[key] = flight
	m.inflightMu.Unlock()

	// Open upstream synchronously so connect/auth errors surface to the caller
	// before we hand back a reader. A 10-minute ceiling matches the previous
	// behavior; the context is detached from ctx because the pump must outlive
	// the leader's request when the client disconnects early.
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	body, contentType, size, upstreamName, err := fetchFn(fetchCtx)
	if err != nil {
		fetchCancel()
		m.releaseInflight(key, flight, err)
		return nil, err
	}

	// Two pipes fan out from a single source: the client gets one (best-effort
	// write — disconnect is silently absorbed) and storage gets the other
	// (must-succeed write — Put consumes it). io.Pipe is unbuffered, so writes
	// block until the reader consumes; that gives natural back-pressure
	// upstream when either consumer is slow.
	clientR, clientW := io.Pipe()
	storageR, storageW := io.Pipe()

	// Pump: upstream → (storage, client). Drives the whole stream. Lives until
	// upstream EOF or read error. Client disconnect does NOT terminate it.
	go m.pumpUpstream(body, fetchCancel, storageW, clientW)

	// Storage writer: drains storageR into storage.Put and commits the DB
	// entry on success. Owns inflight release.
	go m.storeAndCommit(key, adapterType, ttl, contentType, size, storageR, flight)

	return &GetResult{
		Reader:      clientR,
		ContentType: contentType,
		Size:        size,
		Hit:         false,
		Upstream:    upstreamName,
	}, nil
}

// followInflight waits for the leader to commit storage, then serves from cache.
func (m *Manager) followInflight(ctx context.Context, key string, flight *inflightFetch) (*GetResult, error) {
	select {
	case <-flight.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if flight.err != nil {
		return nil, flight.err
	}
	reader, size, gerr := m.storage.Get(ctx, key)
	if gerr != nil {
		return nil, fmt.Errorf("read from cache after leader commit: %w", gerr)
	}
	var entry db.CacheEntry
	_ = m.db.Where("key = ?", key).First(&entry).Error
	return &GetResult{
		Reader:      reader,
		ContentType: entry.ContentType,
		Size:        size,
		Hit:         false,
	}, nil
}

// pumpUpstream copies upstream → storagePipe and (best-effort) upstream →
// clientPipe. Storage write errors abort the pump (cache is no longer valid);
// client write errors are ignored (client disconnect must not break the cache).
func (m *Manager) pumpUpstream(body io.ReadCloser, fetchCancel context.CancelFunc, storageW, clientW *io.PipeWriter) {
	defer body.Close()
	defer fetchCancel()

	buf := make([]byte, 32*1024)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := storageW.Write(buf[:n]); werr != nil {
				// Storage consumer gave up — surface that to the client and stop.
				_ = clientW.CloseWithError(werr)
				return
			}
			// Best-effort: ignore client disconnect (ErrClosedPipe).
			_, _ = clientW.Write(buf[:n])
		}
		if rerr != nil {
			if rerr == io.EOF {
				_ = storageW.Close()
				_ = clientW.Close()
			} else {
				_ = storageW.CloseWithError(rerr)
				_ = clientW.CloseWithError(rerr)
			}
			return
		}
	}
}

// storeAndCommit drains the storage pipe into storage.Put and, on success,
// upserts the CacheEntry row and publishes the cache event. Releases the
// inflight slot when done so followers can read from cache.
func (m *Manager) storeAndCommit(key, adapterType string, ttl time.Duration, contentType string, declaredSize int64, storageR *io.PipeReader, flight *inflightFetch) {
	cr := NewCountingReader(storageR)
	putErr := m.storage.Put(context.Background(), key, cr, declaredSize, contentType)
	if putErr != nil {
		// Drain anything the pump still tries to write so it doesn't deadlock.
		_, _ = io.Copy(io.Discard, storageR)
		zap.L().Warn("cache put failed", zap.String("key", key), zap.Error(putErr))
		m.releaseInflight(key, flight, putErr)
		return
	}

	size := declaredSize
	if size <= 0 {
		size = cr.BytesRead()
	}
	now := time.Now()
	entry := db.CacheEntry{
		Key:          key,
		AdapterType:  adapterType,
		StoragePath:  key,
		Size:         size,
		HitCount:     0,
		ContentType:  contentType,
		PackageName:  packagekey.ExtractName(adapterType, key),
		ExpiresAt:    now.Add(ttl),
		LastAccessed: now,
	}
	if createErr := m.db.Create(&entry).Error; createErr != nil {
		// Already exists — update instead.
		m.db.Where("key = ?", key).Updates(map[string]interface{}{
			"size":          size,
			"content_type":  contentType,
			"package_name":  packagekey.ExtractName(adapterType, key),
			"expires_at":    now.Add(ttl),
			"last_accessed": now,
		})
	}

	// Tamper baseline: first-seen SHA-256 of immutable artifacts.
	if m.tamper != nil && m.isImmutable(ttl) {
		pkgName := packagekey.ExtractName(adapterType, key)
		// Verify (not Record): on a miss where a baseline already exists
		// — e.g. the artifact was LRU-evicted and re-fetched — a differing
		// hash is tampering and must alert. Alert-only here: the prior
		// bytes are already gone from storage, so we can't preserve them,
		// but the operator learns the cached copy changed. clientIP is
		// unknown at the store layer (the leader may have disconnected).
		_ = m.tamper.Verify(context.Background(), key, adapterType, pkgName,
			versionFromKey(adapterType, key), cr.SumHex(), size, "")
	}

	m.publishEvent(key, adapterType, false, size)

	if m.securityScanner != nil {
		pkgName := packagekey.ExtractName(adapterType, key)
		if pkgName != "" {
			go func() {
				if err := m.securityScanner.ScanPackage(context.Background(), adapterType, pkgName); err != nil {
					zap.L().Debug("security scan for new package failed", zap.Error(err))
				}
			}()
		}
	}

	m.releaseInflight(key, flight, nil)
}

// releaseInflight removes the in-flight entry and closes done so followers can
// proceed. err is recorded on the flight before done closes.
func (m *Manager) releaseInflight(key string, flight *inflightFetch, err error) {
	flight.err = err
	m.inflightMu.Lock()
	if cur, ok := m.inflight[key]; ok && cur == flight {
		delete(m.inflight, key)
	}
	m.inflightMu.Unlock()
	close(flight.done)
}

// backgroundRefresh refreshes a stale cache entry in the background.
// It uses singleflight so concurrent requests for the same key share one fetch.
func (m *Manager) backgroundRefresh(key string, adapterType string, ttl time.Duration, fetchFn FetchFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	_, err, _ := m.group.Do("bg:"+key, func() (interface{}, error) {
		body, contentType, size, _, err := fetchFn(ctx)
		if err != nil {
			return nil, err
		}
		defer body.Close()

		cr := NewCountingReader(body)

		// Immutable + tamper on: verify-only. Drain to discard (do NOT
		// overwrite storage — immutable bytes are supposed to be
		// identical), then compare the hash. A mismatch keeps the
		// trusted first-seen copy and alerts; a match just extends TTL.
		if m.tamper != nil && m.isImmutable(ttl) {
			if _, copyErr := io.Copy(io.Discard, cr); copyErr != nil {
				return nil, fmt.Errorf("bg verify read: %w", copyErr)
			}
			pkgName := packagekey.ExtractName(adapterType, key)
			res := m.tamper.Verify(context.Background(), key, adapterType, pkgName,
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
				m.db.Where("key = ?", key).Updates(map[string]interface{}{
					"expires_at":    now.Add(ttl),
					"last_accessed": now,
				})
				return nil, nil
			}
			now := time.Now()
			m.db.Where("key = ?", key).Updates(map[string]interface{}{
				"expires_at":    now.Add(ttl),
				"last_accessed": now,
			})
			zap.L().Debug("tamper: integrity verified", zap.String("key", key))
			return nil, nil
		}

		// Mutable (or tamper off): normal refresh — overwrite storage.
		if putErr := m.storage.Put(ctx, key, cr, size, contentType); putErr != nil {
			return nil, fmt.Errorf("bg refresh put: %w", putErr)
		}
		if size <= 0 {
			size = cr.BytesRead()
		}
		now := time.Now()
		m.db.Where("key = ?", key).Updates(map[string]interface{}{
			"size":          size,
			"content_type":  contentType,
			"expires_at":    now.Add(ttl),
			"last_accessed": now,
		})

		zap.L().Debug("background refresh done", zap.String("key", key))
		return nil, nil
	})

	if err != nil {
		zap.L().Warn("background refresh failed",
			zap.String("key", key),
			zap.Error(err),
		)
	}
}

// serveStale returns a stale cached version when upstream is unavailable.
func (m *Manager) serveStale(ctx context.Context, key string, adapterType string) (*GetResult, error) {
	var entry db.CacheEntry
	if err := m.db.Where("key = ?", key).First(&entry).Error; err != nil {
		return nil, fmt.Errorf("no stale cache for %s", key)
	}

	reader, size, err := m.storage.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("stale cache read failed: %w", err)
	}

	m.db.Model(&entry).Updates(map[string]interface{}{
		"hit_count":     gorm.Expr("hit_count + 1"),
		"last_accessed": time.Now(),
	})

	m.publishEvent(key, adapterType, true, 0)

	return &GetResult{
		Reader:      reader,
		ContentType: entry.ContentType,
		Size:        size,
		Hit:         true,
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

// Storage returns the underlying storage for direct access.
func (m *Manager) Storage() Storage {
	return m.storage
}

// DB returns the underlying database for direct access.
func (m *Manager) DB() *gorm.DB {
	return m.db
}

// versionFromKey is a best-effort version string for tamper event
// readability. The cache key already encodes the artifact; we only need
// something human-facing, so an empty result is acceptable.
func versionFromKey(adapterType, key string) string {
	// The filename tail usually carries the version; keep it simple —
	// the authoritative identity is the cache key itself.
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return ""
}
