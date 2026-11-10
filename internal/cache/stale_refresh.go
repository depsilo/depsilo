package cache

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
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
