package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/clause"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/cascade"
	"depsilo/internal/db"
	"depsilo/internal/traffic"
)

// inflightFetch tracks a miss-path fetch that is currently streaming. Followers
// (concurrent requests for the same key) block on done until the storage write
// commits, then read from cache. Same semantics as a singleflight that releases
// when storage is durable — not when the leader's client finishes reading.
type inflightFetch struct {
	transfer       *traffic.TransferHandle
	done           chan struct{}
	err            error
	committedEntry *db.CacheEntry
	refreshOutcome RefreshOutcome
	trackers       []*RefreshTracker // guarded by Manager.inflightMu
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
	flight := &inflightFetch{done: make(chan struct{}), transfer: m.transfers.Start(adapterType, traffic.TransferName(ctx))}
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
	// An ordinary client fill runs on the manager lifecycle context, which
	// deliberately drops request values. Re-attach the caller's origin
	// accumulator across that seam so the bytes the pump reads from upstream
	// land on the access-log row that caused the fetch. The process-wide meter
	// counts every exchange regardless; this only restores per-request
	// attribution. A coalesced follower keeps its own (empty) accumulator —
	// the leader performed the exchange, so the bytes are charged once.
	fetchCtx = traffic.WithAttribution(fetchCtx, traffic.AttributionFromContext(ctx))
	fetchCtx = traffic.WithTransfer(fetchCtx, flight.transfer)
	// Cache fills run on a detached lifecycle context, so the cascade chain and
	// freshness budget must cross the same seam for peer-relayed egress.
	fetchCtx = cascade.WithChain(fetchCtx, cascade.ChainFrom(ctx))
	fetchCtx = cascade.WithFetchTTL(fetchCtx, ttl)
	fetchCtx = cascade.WithFetchKind(fetchCtx, relayFetchKind(adapterType, key))
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
	flight.transfer.Downloading(size, upstreamName)
	body = traffic.ObserveTransferBody(body, flight.transfer)

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
	return m.followInflightWithin(ctx, lifecycleCtx, key, flight, 0)
}

// followInflightWithin waits for flight to commit and then reads its entry. A
// positive budget bounds only the wait, never the read that follows, and
// reports errStaleRefreshPending when it expires first.
func (m *Manager) followInflightWithin(ctx, lifecycleCtx context.Context, key string, flight *inflightFetch, budget time.Duration) (*GetResult, error) {
	var expired <-chan time.Time
	if budget > 0 {
		timer := time.NewTimer(budget)
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-flight.done:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lifecycleCtx.Done():
		return nil, lifecycleCtx.Err()
	case <-expired:
		return nil, errStaleRefreshPending
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
	flight.transfer.Finish(err == nil && len(committed) > 0)
	for _, tracker := range trackers {
		tracker.complete(outcome, err)
	}
	close(flight.done)
}
