package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/upstream"
)

const (
	approximateProvenancePositiveTTL = time.Hour
	approximateProvenanceNegativeTTL = 30 * time.Second
	approximateProvenanceCacheLimit  = 4096
)

// ApproximateProvenance resolves and memoizes Last-Modified provenance for
// adapters whose registry does not expose an exact publish time. The operator
// must acknowledge the ecosystem through supply_chain.approximate_sources
// before the policy accepts a positive threshold.
type ApproximateProvenance struct {
	mu    sync.Mutex
	cache map[string]approximateProvenanceEntry
}

type approximateProvenanceEntry struct {
	provenance QuarantineProvenance
	ok         bool
	expiresAt  time.Time
}

func NewApproximateProvenance() *ApproximateProvenance {
	return &ApproximateProvenance{}
}

// Resolve HEADs the exact artifact path through the selector and returns the
// parsed Last-Modified as provenance. Resolved timestamps are cached for an
// hour; failures retry quickly so a transient mirror problem is not pinned.
func (p *ApproximateProvenance) Resolve(
	ctx context.Context,
	selector upstream.Selector,
	ecosystem, path string,
) (QuarantineProvenance, bool) {
	if p == nil || selector == nil {
		return QuarantineProvenance{}, false
	}
	selected, err := selector.Select(ctx)
	if err != nil {
		zap.L().Warn("approximate provenance upstream selection failed; the age gate will fail closed",
			zap.String("ecosystem", ecosystem),
			zap.String("path", path),
			zap.Error(err),
		)
		return QuarantineProvenance{}, false
	}
	key := selected.Name + "\x00" + path
	now := time.Now()
	p.mu.Lock()
	if entry, found := p.cache[key]; found && now.Before(entry.expiresAt) {
		provenance, ok := entry.provenance, entry.ok
		p.mu.Unlock()
		return provenance, ok
	}
	p.mu.Unlock()

	lastModified, err := selected.HeadLastModified(ctx, "/"+strings.TrimPrefix(path, "/"))
	ok := err == nil
	if err != nil {
		zap.L().Warn("approximate provenance Last-Modified unavailable; the age gate will fail closed",
			zap.String("ecosystem", ecosystem),
			zap.String("path", path),
			zap.Error(err),
		)
	}
	provenance := QuarantineProvenance{}
	if ok {
		provenance = QuarantineProvenance{
			SourceID:  ApproximateArtifactSourceID(ecosystem, selected.ProvenanceSourceID(), path),
			PublishAt: lastModified,
		}
	}
	ttl := approximateProvenancePositiveTTL
	if !ok {
		ttl = approximateProvenanceNegativeTTL
	}
	p.mu.Lock()
	if p.cache == nil {
		p.cache = make(map[string]approximateProvenanceEntry)
	}
	p.cache[key] = approximateProvenanceEntry{provenance: provenance, ok: ok, expiresAt: now.Add(ttl)}
	for len(p.cache) > approximateProvenanceCacheLimit {
		for cachedKey := range p.cache {
			delete(p.cache, cachedKey)
			break
		}
	}
	p.mu.Unlock()
	return provenance, ok
}

// ApproximateArtifactSourceID names the exact upstream artifact whose
// Last-Modified provided the approximate timestamp.
func ApproximateArtifactSourceID(ecosystem, upstreamSource, path string) string {
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/approximate-artifact-source/v1",
		ecosystem,
		upstreamSource,
		path,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
