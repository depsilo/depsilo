package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/cascade"
	"depsilo/internal/db"
)

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
	operationCtx = cascade.WithChain(operationCtx, cascade.ChainFrom(ctx))

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
		result, err = m.followInflightWithin(ctx, m.lifecycleCtx, key, flight, m.staleWaitBudget(ctx, adapterType, key))
		if errors.Is(err, errStaleRefreshPending) {
			if stale, staleErr := m.serveStale(ctx, key, adapterType); staleErr == nil {
				logSlowRefresh(key)
				return stale, nil
			}
			// Without a stale copy this flight is a first fill: keep waiting.
			result, err = m.followInflight(ctx, m.lifecycleCtx, key, flight)
		}
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
		if budget := m.staleWaitBudget(ctx, adapterType, key); exists && budget > 0 {
			result, err = m.refreshWithin(ctx, budget, key, adapterType, ttl, fetchFn)
		} else {
			result, err = m.fetchAndStore(ctx, key, adapterType, ttl, fetchFn)
		}
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
			return m.fetchPassthrough(ctx, key, adapterType, ttl, fetchFn)
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
func (m *Manager) fetchPassthrough(
	ctx context.Context,
	key string,
	adapterType string,
	ttl time.Duration,
	fetchFn FetchFunc,
) (*GetResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, finishOwned, admitted := m.beginOwned()
	if !admitted {
		return nil, ErrManagerClosed
	}
	operationCtx, operationCancel := m.bindLifecycle(ctx)
	operationCtx = cascade.WithChain(operationCtx, cascade.ChainFrom(ctx))
	operationCtx = cascade.WithFetchTTL(operationCtx, ttl)
	operationCtx = cascade.WithFetchKind(operationCtx, relayFetchKind(adapterType, key))
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
