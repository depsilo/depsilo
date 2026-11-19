package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/cascade"
	"depsilo/internal/db"
)

// defaultStaleRefreshBudget keeps a stale metadata refresh inside common
// package-manager timeouts (pip waits 15s by default) when an Upstream stops
// answering, for example on a link that drops packets.
const defaultStaleRefreshBudget = 10 * time.Second

// errStaleRefreshPending reports that a metadata refresh outlasted the
// request's wait budget. The refresh continues; it never leaves Get.
var errStaleRefreshPending = errors.New("metadata refresh outlasted the stale-serving budget")

// staleWaitBudget bounds how long an ordinary request waits for the refresh of
// stale metadata before it is served the stale copy. Prefetch, tracked and
// forced refreshes exist to obtain the refresh itself, so they wait for it.
func (m *Manager) staleWaitBudget(ctx context.Context, adapterType, key string) time.Duration {
	request := refreshRequestFrom(ctx)
	if request.contextBound || request.forced || request.tracker != nil || !m.isMutableMetadata(adapterType, key) {
		return 0
	}
	return m.staleRefreshBudget
}

// refreshWithin refreshes stale metadata while this request waits at most
// budget. A slower refresh keeps running on the manager lifecycle, and its
// response is released unread once it arrives; like a client disconnect, that
// lets a healthy fill complete. Without a servable stale copy, the request
// waits for the refresh after all.
func (m *Manager) refreshWithin(ctx context.Context, budget time.Duration, key, adapterType string, ttl time.Duration, fetchFn FetchFunc) (*GetResult, error) {
	type outcome struct {
		result *GetResult
		err    error
	}
	done := make(chan outcome, 1)
	refreshCtx := context.WithoutCancel(ctx)
	if !m.goOwned(func(context.Context) {
		result, err := m.fetchAndStore(refreshCtx, key, adapterType, ttl, fetchFn)
		done <- outcome{result, err}
	}) {
		return nil, ErrManagerClosed
	}
	release := func() {
		go func() {
			if late := <-done; late.result != nil && late.result.Reader != nil {
				_ = late.result.Reader.Close()
			}
		}()
	}

	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case refreshed := <-done:
		return refreshed.result, refreshed.err
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	case <-timer.C:
	}
	if stale, err := m.serveStale(ctx, key, adapterType); err == nil {
		release()
		logSlowRefresh(key)
		return stale, nil
	}
	select {
	case refreshed := <-done:
		return refreshed.result, refreshed.err
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
}

func logSlowRefresh(key string) {
	zap.L().Warn("metadata refresh is slow; serving stale cache while it continues", zap.String("key", key))
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
		refreshCtx = cascade.WithFetchTTL(refreshCtx, ttl)
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
	ctx = cascade.WithFetchTTL(ctx, ttl)
	ctx = cascade.WithFetchKind(ctx, relayFetchKind(adapterType, key))

	err := m.refreshFromUpstream(ctx, key, adapterType, ttl, fetchFn, cancelFetchWithCause, writeGeneration)
	// Every successful refresh path ends with a committed write generation.
	writeFinished = err == nil

	m.handleBackgroundRefreshError(parentCtx, key, err)
}

// refreshFromUpstream fetches key and applies the response to the cache while
// the body stays bound to ctx: an immutable artifact under tamper detection is
// verified in place, anything else overwrites the stored object.
func (m *Manager) refreshFromUpstream(ctx context.Context, key, adapterType string, ttl time.Duration, fetchFn FetchFunc, cancelFetchWithCause context.CancelCauseFunc, writeGeneration uint64) error {
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

	// Immutable + tamper on: verify-only. Drain to discard (do NOT
	// overwrite storage — immutable bytes are supposed to be
	// identical), then compare the hash. A mismatch keeps the
	// trusted first-seen copy and alerts; a match just extends TTL.
	if tamper := m.tamperRecorder(); tamper != nil &&
		m.cacheKind(adapterType, key) == db.CacheKindArtifact &&
		m.tamperEligible(adapterType, key, ttl) {
		return m.verifyRefreshedArtifact(ctx, tamper, key, adapterType, ttl, writeGeneration, body, size)
	}

	// Mutable (or tamper off): normal refresh — overwrite storage.
	return m.overwriteRefreshedEntry(ctx, key, adapterType, ttl, writeGeneration, body, size, contentType, validators, headers)
}

// verifyRefreshedArtifact drains a refreshed immutable artifact and checks it
// against the tamper baseline without overwriting storage, then extends the
// entry's TTL.
func (m *Manager) verifyRefreshedArtifact(ctx context.Context, tamper TamperRecorder, key, adapterType string, ttl time.Duration, writeGeneration uint64, body io.Reader, size int64) error {
	cr, refreshReader, _ := newSizedReader(body, size)
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
		if err := m.extendRefreshedTTL(ctx, entry.ID, key, adapterType, ttl, "bg tamper TTL update"); err != nil {
			return err
		}
		return m.commitRefreshWrite(ctx, key, writeGeneration)
	}
	if err := m.extendRefreshedTTL(ctx, entry.ID, key, adapterType, ttl, "bg integrity TTL update"); err != nil {
		return err
	}
	if err := m.commitRefreshWrite(ctx, key, writeGeneration); err != nil {
		return err
	}
	zap.L().Debug("tamper: integrity verified", zap.String("key", key))
	return nil
}

// extendRefreshedTTL advances a verified entry's expiry without touching its
// stored representation. The caller holds the key mutation gate.
func (m *Manager) extendRefreshedTTL(ctx context.Context, entryID uint, key, adapterType string, ttl time.Duration, operation string) error {
	now := time.Now()
	update := m.db.WithContext(ctx).Model(&db.CacheEntry{}).Where("id = ? AND key = ?", entryID, key).Updates(map[string]interface{}{
		"expires_at":    now.Add(ttl),
		"last_accessed": now,
		"cache_kind":    m.cacheKind(adapterType, key),
	})
	return refreshMetadataUpdateError(update, operation)
}

// overwriteRefreshedEntry replaces the stored object with the refreshed body
// and updates its metadata row. The body streams without the key mutation
// gate; the gate covers the object switch and the row update.
func (m *Manager) overwriteRefreshedEntry(ctx context.Context, key, adapterType string, ttl time.Duration, writeGeneration uint64, body io.Reader, size int64, contentType string, validators ResponseValidators, headers http.Header) error {
	cr, refreshReader, exactReader := newSizedReader(body, size)
	var entry db.CacheEntry
	gate := m.newPublishGate(ctx, key, writeGeneration, func() error {
		current, err := m.refreshEntry(ctx, key)
		entry = current
		return err
	})
	defer gate.release()
	putErr := m.putPublished(ctx, key, refreshReader, size, contentType, gate.acquire)
	switch {
	case gate.lockErr != nil:
		return gate.lockErr
	case gate.superseded:
		return errors.Join(errCacheWriteSuperseded, m.discardSupersededWrite(ctx, key))
	case gate.recheckErr != nil:
		return gate.recheckErr
	}
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
	if err := m.commitRefreshWrite(ctx, key, writeGeneration); err != nil {
		return err
	}

	zap.L().Debug("background refresh done", zap.String("key", key))
	return nil
}

// commitRefreshWrite publishes the refresh's write generation, discarding the
// write when a newer generation superseded it. The caller holds the key
// mutation gate.
func (m *Manager) commitRefreshWrite(ctx context.Context, key string, writeGeneration uint64) error {
	if !m.invalidations.commitWrite(key, writeGeneration) {
		return errors.Join(errCacheWriteSuperseded, m.discardSupersededWrite(ctx, key))
	}
	return nil
}

// handleBackgroundRefreshError logs a failed refresh and invalidates the
// cached entry when the Upstream answered authoritatively.
func (m *Manager) handleBackgroundRefreshError(parentCtx context.Context, key string, err error) {
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

// discardSupersededWrite runs while the caller holds the key mutation gate,
// for a write that lost the generation race. If the publish hook refused it,
// it made nothing visible and the row and object under key belong to someone
// else; if it was superseded after its own publish, the gate it has held
// since then means they are its own. Either way they are deleted only to
// retry an invalidation that has not yet removed what it revoked (for a write
// published under the gate, that is the write itself); once a newer write
// has committed, or either half of the invalidation's delete succeeded, they
// are left alone. That matches how invalidation defines cleanupSafe: after a
// successful row delete, an object whose delete failed stays in storage until
// a newer fill replaces it, but reads go through the row, so it is leaked
// space, never served.
func (m *Manager) discardSupersededWrite(ctx context.Context, key string) error {
	if !m.invalidations.cleanupPending(key) {
		return nil
	}
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
