package composer

import (
	"context"
	"time"
)

const (
	// Metadata can move for dev branches, so the memo is intentionally short.
	// It removes repeated p2 JSON parsing for installs that fetch the same
	// pinned dist several times in quick succession.
	distMemoPositiveTTL = time.Minute
	distMemoNegativeTTL = 30 * time.Second
	distMemoLimit       = 4096
)

type distMemoEntry struct {
	entry     *distEntry
	expiresAt time.Time
}

func (h *Handler) resolveDistEntryMemoized(
	ctx context.Context,
	vendor, pkg, versionNorm, reference string,
) (*distEntry, error) {
	key := vendor + "/" + pkg + "\x00" + versionNorm + "\x00" + reference
	now := time.Now()
	h.distMu.Lock()
	if cached, ok := h.distCache[key]; ok && now.Before(cached.expiresAt) {
		entry := cached.entry
		h.distMu.Unlock()
		return entry, nil
	}
	h.distMu.Unlock()

	entry, err := h.resolveDistEntry(ctx, vendor, pkg, versionNorm, reference)
	if err != nil {
		// Transient metadata failures are not pinned; the next request
		// re-resolves immediately.
		return nil, err
	}
	ttl := distMemoPositiveTTL
	if entry == nil {
		ttl = distMemoNegativeTTL
	}
	h.distMu.Lock()
	if h.distCache == nil {
		h.distCache = make(map[string]distMemoEntry)
	}
	h.distCache[key] = distMemoEntry{entry: entry, expiresAt: now.Add(ttl)}
	for len(h.distCache) > distMemoLimit {
		for cachedKey := range h.distCache {
			delete(h.distCache, cachedKey)
			break
		}
	}
	h.distMu.Unlock()
	return entry, nil
}
