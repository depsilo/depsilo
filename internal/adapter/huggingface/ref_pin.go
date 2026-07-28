package huggingface

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

const stalePinRetryBackoff = 30 * time.Second

type refPinResult struct {
	pinned       bool
	ephemeral    bool
	parsed       Parsed
	target       string
	selected     *upstream.Upstream
	headResponse *resolvedResponse
	cachedHead   *cache.HeadResult
}

func (h *Handler) resolveMutableRef(
	ctx context.Context,
	inbound *http.Request,
	parsed Parsed,
	target string,
) (refPinResult, error) {
	pinKey, ok := mutableRefPinKey(parsed)
	if !ok || h.db == nil {
		return refPinResult{parsed: parsed, target: target}, nil
	}

	now := time.Now().UTC()
	oldPin, found, loadErr := h.loadRefPin(ctx, pinKey)
	if loadErr != nil {
		zap.L().Warn("Hugging Face ref pin read failed; continuing without persisted pin",
			zap.String("key", pinKey),
			zap.Error(loadErr),
		)
		oldPin = db.HuggingFaceRefPin{}
		found = false
	}
	if found && now.Before(oldPin.ExpiresAt) {
		result, ok := pinnedRefResult(parsed, inbound.URL.RawQuery, oldPin.Commit)
		if ok {
			return result, nil
		}
		_ = h.deleteRefPin(ctx, pinKey)
		found = false
	}

	selected, err := h.selector.Select(ctx)
	if err != nil || selected == nil {
		if err == nil {
			err = errors.New("selector returned nil upstream")
		}
		failure := &proxyFailure{status: http.StatusServiceUnavailable, cause: err}
		if fallback, ok := h.cachedPinFallback(ctx, parsed, inbound.URL.RawQuery, oldPin, found, failure); ok {
			return fallback, nil
		}
		return refPinResult{}, failure
	}

	headRequest := inbound.Clone(ctx)
	headRequest.Method = http.MethodHead
	headRequest.Body = http.NoBody
	headRequest.GetBody = nil
	headRequest.ContentLength = 0
	for _, name := range []string{
		"Range",
		"If-Range",
		"If-Match",
		"If-None-Match",
		"If-Modified-Since",
		"If-Unmodified-Since",
	} {
		headRequest.Header.Del(name)
	}
	resolved, resolveErr := h.resolver.resolve(ctx, selected, headRequest, target, false)
	if resolveErr != nil {
		failure := &proxyFailure{
			status:   http.StatusBadGateway,
			upstream: selected.Name,
			cause:    resolveErr,
		}
		if fallback, ok := h.cachedPinFallback(ctx, parsed, inbound.URL.RawQuery, oldPin, found, failure); ok {
			fallback.selected = h.selectAfterFailure(ctx, selected)
			return fallback, nil
		}
		if inbound.Method != http.MethodHead {
			// Resolver errors may have latched selected out of service after an
			// integrity violation. Even ordinary transport failures may have
			// changed its passive health. Any direct fallback must therefore
			// choose a different healthy source when one is available.
			return refPinResult{
				parsed:   parsed,
				target:   target,
				selected: h.selectAfterFailure(ctx, selected),
			}, nil
		}
		return refPinResult{}, failure
	}
	if resolved.StatusCode != http.StatusOK {
		repositoryRevoked := repositoryRevocationStatus(
			resolved.StatusCode,
			resolved.Header,
			resolved.provenance,
		)
		if repositoryRevoked {
			// Persist the fail-closed boundary before a potentially slow error
			// body is buffered for the downstream response.
			h.revokeRepositoryAsync(ctx, parsed)
		}
		statusErr := newUpstreamStatusError(resolved, selected.Name, h.artifactIdleTimeout)
		if !repositoryRevoked {
			if fallback, ok := h.cachedPinFallback(
				ctx,
				parsed,
				inbound.URL.RawQuery,
				oldPin,
				found,
				statusErr,
			); ok {
				// A transient status has already affected this upstream's
				// passive health. Bind the eventual stale-pin GET to a healthy
				// alternative when one is available.
				fallback.selected = h.selectAfterFailure(ctx, selected)
				return fallback, nil
			}
		}
		switch {
		case !repositoryRevoked && !statusErr.AllowStaleFallback():
			h.invalidateAuthoritativePin(
				ctx,
				pinKey,
				parsed,
				inbound.URL.RawQuery,
				oldPin,
				found,
			)
		}
		if inbound.Method != http.MethodHead {
			if !repositoryRevoked && statusErr.AllowStaleFallback() {
				return refPinResult{
					parsed:   parsed,
					target:   target,
					selected: h.selectAfterFailure(ctx, selected),
				}, nil
			}
			return refPinResult{parsed: parsed, target: target, selected: selected}, nil
		}
		return refPinResult{}, statusErr
	}

	commit := resolved.Header.Get("X-Repo-Commit")
	if !IsCommitSHA(commit) {
		// A third-party mirror may omit the canonical commit contract. Preserve
		// compatibility by serving it directly, but never cache a moving ref
		// whose HEAD and GET cannot be tied to one snapshot.
		return refPinResult{
			parsed:       parsed,
			target:       target,
			selected:     selected,
			headResponse: resolved,
		}, nil
	}

	candidate, ok := pinnedRefResult(parsed, inbound.URL.RawQuery, commit)
	if !ok {
		_ = resolved.Body.Close()
		return refPinResult{}, &proxyFailure{
			status:   http.StatusBadGateway,
			upstream: selected.Name,
			cause:    errors.New("Hugging Face returned an invalid canonical commit"),
		}
	}
	candidate.selected = selected
	candidate.headResponse = resolved
	if loadErr != nil {
		candidate.ephemeral = true
		return candidate, nil
	}

	pinTTL := h.cfg.TTLIndex
	if pinTTL <= 0 {
		pinTTL = 5 * time.Minute
	}
	winner, err := h.claimRefPin(ctx, db.HuggingFaceRefPin{
		Key:       pinKey,
		Commit:    commit,
		ExpiresAt: now.Add(pinTTL),
	}, now)
	if err != nil {
		zap.L().Warn("Hugging Face ref pin write failed; using ephemeral canonical ref",
			zap.String("key", pinKey),
			zap.String("commit", commit),
			zap.Error(err),
		)
		candidate.ephemeral = true
		return candidate, nil
	}
	if winner.Commit == commit {
		return candidate, nil
	}
	_ = resolved.Body.Close()

	result, ok := pinnedRefResult(parsed, inbound.URL.RawQuery, winner.Commit)
	if !ok {
		return refPinResult{}, &proxyFailure{
			status:   http.StatusBadGateway,
			upstream: selected.Name,
			cause:    errors.New("Hugging Face ref pin contained an invalid commit"),
		}
	}
	result.selected = selected
	return result, nil
}

func pinnedRefResult(parsed Parsed, rawQuery, commit string) (refPinResult, bool) {
	canonical, ok := withCommit(parsed, commit)
	if !ok {
		return refPinResult{}, false
	}
	target, ok := requestTarget(canonical, rawQuery)
	if !ok {
		return refPinResult{}, false
	}
	return refPinResult{
		pinned: true,
		parsed: canonical,
		target: target,
	}, true
}

func (h *Handler) cachedPinFallback(
	ctx context.Context,
	parsed Parsed,
	rawQuery string,
	oldPin db.HuggingFaceRefPin,
	found bool,
	failure error,
) (refPinResult, bool) {
	if !found || h.cacheMgr == nil || !stalePinAllowed(failure) {
		return refPinResult{}, false
	}
	result, ok := pinnedRefResult(parsed, rawQuery, oldPin.Commit)
	if !ok {
		return refPinResult{}, false
	}
	cacheKey, err := CacheKeyForRawQuery(result.parsed, rawQuery)
	if err != nil || cacheKey == "" {
		return refPinResult{}, false
	}
	repository, hasRepository := repositoryForParsed(result.parsed)
	leaveCache := func() {}
	if hasRepository && h.revocations != nil {
		var admitted bool
		leaveCache, admitted = h.revocations.enterCache(repository.packageName)
		if !admitted {
			return refPinResult{}, false
		}
	}
	head, err := h.cacheMgr.Head(ctx, cacheKey, "huggingface")
	leaveCache()
	if err != nil {
		return refPinResult{}, false
	}
	h.backoffStalePinRetry(ctx, oldPin)
	result.cachedHead = head
	return result, true
}

func (h *Handler) backoffStalePinRetry(ctx context.Context, oldPin db.HuggingFaceRefPin) {
	if h.db == nil || oldPin.Key == "" {
		return
	}
	now := time.Now().UTC()
	if oldPin.ExpiresAt.After(now) {
		return
	}
	update := h.db.WithContext(ctx).
		Model(&db.HuggingFaceRefPin{}).
		Where(map[string]interface{}{
			"key":    oldPin.Key,
			"commit": oldPin.Commit,
		}).
		Where("expires_at <= ?", now).
		Update("expires_at", now.Add(stalePinRetryBackoff))
	if update.Error != nil {
		zap.L().Warn("Hugging Face stale ref pin retry backoff failed",
			zap.String("key", oldPin.Key),
			zap.Error(update.Error),
		)
	}
}

func stalePinAllowed(err error) bool {
	var policy cache.StaleFallbackPolicy
	if errors.As(err, &policy) {
		return policy.AllowStaleFallback()
	}
	return true
}

func (h *Handler) loadRefPin(ctx context.Context, key string) (db.HuggingFaceRefPin, bool, error) {
	var pin db.HuggingFaceRefPin
	err := h.db.WithContext(ctx).Where("key = ?", key).First(&pin).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.HuggingFaceRefPin{}, false, nil
	}
	return pin, err == nil, err
}

func (h *Handler) claimRefPin(
	ctx context.Context,
	candidate db.HuggingFaceRefPin,
	now time.Time,
) (db.HuggingFaceRefPin, error) {
	insert := h.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&candidate)
	if insert.Error != nil {
		return db.HuggingFaceRefPin{}, fmt.Errorf("create Hugging Face ref pin: %w", insert.Error)
	}
	if insert.RowsAffected == 1 {
		return candidate, nil
	}

	update := h.db.WithContext(ctx).
		Model(&db.HuggingFaceRefPin{}).
		Where("key = ? AND expires_at <= ?", candidate.Key, now).
		Updates(map[string]interface{}{
			"commit":     candidate.Commit,
			"expires_at": candidate.ExpiresAt,
			"updated_at": now,
		})
	if update.Error != nil {
		return db.HuggingFaceRefPin{}, fmt.Errorf("renew Hugging Face ref pin: %w", update.Error)
	}
	if update.RowsAffected == 1 {
		return candidate, nil
	}

	var winner db.HuggingFaceRefPin
	if err := h.db.WithContext(ctx).Where("key = ?", candidate.Key).First(&winner).Error; err != nil {
		return db.HuggingFaceRefPin{}, fmt.Errorf("read winning Hugging Face ref pin: %w", err)
	}
	return winner, nil
}

func (h *Handler) deleteRefPin(ctx context.Context, key string) error {
	return h.db.WithContext(ctx).Where("key = ?", key).Delete(&db.HuggingFaceRefPin{}).Error
}

type excludingUpstreamSelector interface {
	SelectExcluding(context.Context, *upstream.Upstream) (*upstream.Upstream, error)
}

func (h *Handler) selectAfterFailure(
	ctx context.Context,
	failed *upstream.Upstream,
) *upstream.Upstream {
	if h == nil || h.selector == nil {
		return nil
	}
	if selector, ok := h.selector.(excludingUpstreamSelector); ok {
		if selected, err := selector.SelectExcluding(ctx, failed); err == nil && selected != nil {
			return selected
		}
	} else if selected, err := h.selector.Select(ctx); err == nil &&
		selected != nil && selected != failed {
		return selected
	}
	// With only one source, retrying an ordinary transient failure preserves
	// availability. A critical failure latches the source unhealthy, so it must
	// never be re-admitted through this fallback.
	if failed != nil && failed.IsHealthy() {
		return failed
	}
	return nil
}

func (h *Handler) invalidateAuthoritativePin(
	ctx context.Context,
	pinKey string,
	parsed Parsed,
	rawQuery string,
	oldPin db.HuggingFaceRefPin,
	found bool,
) {
	var cleanupErr error
	// Invalidate the exact canonical representation before deleting the lease.
	// If pin deletion later fails, cachedPinFallback still cannot revive bytes
	// that the Hub has authoritatively marked private or missing.
	if found && h.cacheMgr != nil {
		pinned, ok := pinnedRefResult(parsed, rawQuery, oldPin.Commit)
		if !ok {
			cleanupErr = errors.Join(cleanupErr, errors.New("derive revoked Hugging Face canonical ref"))
		} else {
			cacheKey, err := CacheKeyForRawQuery(pinned.parsed, rawQuery)
			if err != nil || cacheKey == "" {
				if err == nil {
					err = errors.New("empty revoked Hugging Face cache key")
				}
				cleanupErr = errors.Join(cleanupErr, err)
			} else if err := h.cacheMgr.Invalidate(ctx, cacheKey); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
	}
	if err := h.deleteRefPin(ctx, pinKey); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if cleanupErr != nil {
		zap.L().Error("Hugging Face authoritative ref cleanup failed closed",
			zap.String("key", pinKey),
			zap.Error(cleanupErr),
		)
	}
}
