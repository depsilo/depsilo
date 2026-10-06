package conda

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// Conda package artifacts are immutable, so Last-Modified stays valid for a
	// long time; negative results retry quickly.
	condaProvenancePositiveTTL = time.Hour
	condaProvenanceNegativeTTL = 30 * time.Second
	condaProvenanceCacheLimit  = 4096
)

type condaProvenanceMemo struct {
	mu    sync.Mutex
	cache map[string]condaProvenanceEntry
}

type condaProvenanceEntry struct {
	lastModified time.Time
	upstream     string
	ok           bool
	expiresAt    time.Time
}

// publishedProvenance HEADs the exact artifact through the configured upstream
// and returns its Last-Modified time. This is the weaker, approximate
// provenance the operator explicitly acknowledged via approximate_sources.
func (h *Handler) publishedProvenance(ctx context.Context, path string) (time.Time, string, bool) {
	selected, err := h.selector.Select(ctx)
	if err != nil {
		zap.L().Warn("conda provenance upstream selection failed; the age gate will fail closed",
			zap.String("path", path),
			zap.Error(err),
		)
		return time.Time{}, "", false
	}
	key := selected.Name + "\x00" + path
	now := time.Now()
	if h.provenanceMemo != nil {
		h.provenanceMemo.mu.Lock()
		if entry, ok := h.provenanceMemo.cache[key]; ok && now.Before(entry.expiresAt) {
			lastModified, upstreamSource := entry.lastModified, entry.upstream
			h.provenanceMemo.mu.Unlock()
			return lastModified, upstreamSource, entry.ok
		}
		h.provenanceMemo.mu.Unlock()
	}

	lastModified, err := selected.HeadLastModified(ctx, "/"+strings.TrimPrefix(path, "/"))
	ok := err == nil
	if err != nil {
		zap.L().Warn("conda artifact Last-Modified unavailable; the age gate will fail closed",
			zap.String("path", path),
			zap.Error(err),
		)
	}
	upstreamSource := selected.ProvenanceSourceID()
	if h.provenanceMemo != nil {
		ttl := condaProvenancePositiveTTL
		if !ok {
			ttl = condaProvenanceNegativeTTL
		}
		h.provenanceMemo.mu.Lock()
		if h.provenanceMemo.cache == nil {
			h.provenanceMemo.cache = make(map[string]condaProvenanceEntry)
		}
		h.provenanceMemo.cache[key] = condaProvenanceEntry{
			lastModified: lastModified,
			upstream:     upstreamSource,
			ok:           ok,
			expiresAt:    now.Add(ttl),
		}
		for len(h.provenanceMemo.cache) > condaProvenanceCacheLimit {
			for cachedKey := range h.provenanceMemo.cache {
				delete(h.provenanceMemo.cache, cachedKey)
				break
			}
		}
		h.provenanceMemo.mu.Unlock()
	}
	return lastModified, upstreamSource, ok
}

// condaArtifactSourceID names the exact upstream artifact path whose
// Last-Modified provided the approximate timestamp.
func condaArtifactSourceID(upstreamSource, path string) string {
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/conda-artifact-source/v1",
		upstreamSource,
		path,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
