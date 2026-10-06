package cran

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/upstream"
)

const (
	// CRAN artifacts are immutable; positives stay valid for a long time and
	// negatives retry quickly.
	cranProvenancePositiveTTL = time.Hour
	cranProvenanceNegativeTTL = 30 * time.Second
	cranProvenanceCacheLimit  = 4096
)

type cranProvenanceMemo struct {
	mu    sync.Mutex
	cache map[string]cranProvenanceEntry
}

type cranProvenanceEntry struct {
	published time.Time
	upstream  string
	exact     bool
	ok        bool
	expiresAt time.Time
}

// publishedProvenance resolves the publish time for a CRAN artifact through
// the configured upstream. Current source tarballs use the DESCRIPTION
// Date/Publication field (exact); archive and binary artifacts fall back to the
// artifact's Last-Modified header (approximate).
func (h *Handler) publishedProvenance(ctx context.Context, path, pkg, version string) (time.Time, string, bool, bool) {
	selected, err := h.selector.Select(ctx)
	if err != nil {
		zap.L().Warn("cran provenance upstream selection failed; the age gate will fail closed",
			zap.String("path", path),
			zap.Error(err),
		)
		return time.Time{}, "", false, false
	}
	key := selected.Name + "\x00" + path
	now := time.Now()
	if h.provenanceMemo != nil {
		h.provenanceMemo.mu.Lock()
		if entry, ok := h.provenanceMemo.cache[key]; ok && now.Before(entry.expiresAt) {
			published, upstreamSource, exact, exactOK := entry.published, entry.upstream, entry.exact, entry.ok
			h.provenanceMemo.mu.Unlock()
			return published, upstreamSource, exact, exactOK
		}
		h.provenanceMemo.mu.Unlock()
	}

	published, exact, ok, err := h.resolveProvenance(ctx, selected, path, pkg, version)
	if err != nil {
		zap.L().Warn("cran artifact provenance unavailable; the age gate will fail closed",
			zap.String("path", path),
			zap.Error(err),
		)
	}
	upstreamSource := selected.ProvenanceSourceID()
	if h.provenanceMemo != nil {
		ttl := cranProvenancePositiveTTL
		if !ok {
			ttl = cranProvenanceNegativeTTL
		}
		h.provenanceMemo.mu.Lock()
		if h.provenanceMemo.cache == nil {
			h.provenanceMemo.cache = make(map[string]cranProvenanceEntry)
		}
		h.provenanceMemo.cache[key] = cranProvenanceEntry{
			published: published,
			upstream:  upstreamSource,
			exact:     exact,
			ok:        ok,
			expiresAt: now.Add(ttl),
		}
		for len(h.provenanceMemo.cache) > cranProvenanceCacheLimit {
			for cachedKey := range h.provenanceMemo.cache {
				delete(h.provenanceMemo.cache, cachedKey)
				break
			}
		}
		h.provenanceMemo.mu.Unlock()
	}
	return published, upstreamSource, exact, ok
}

func (h *Handler) resolveProvenance(
	ctx context.Context,
	selected *upstream.Upstream,
	path, pkg, version string,
) (time.Time, bool, bool, error) {
	if isCurrentSourcePath(path, pkg, version) {
		if published, ok := h.descriptionPublished(ctx, selected, pkg, version); ok {
			return published, true, true, nil
		}
	}
	lastModified, err := selected.HeadLastModified(ctx, "/"+strings.TrimPrefix(path, "/"))
	if err != nil {
		return time.Time{}, false, false, err
	}
	return lastModified, false, true, nil
}

// isCurrentSourcePath reports whether the artifact is the current source
// tarball (not an archive and not a binary build).
func isCurrentSourcePath(path, pkg, version string) bool {
	if !strings.HasPrefix(path, "src/contrib/") || strings.HasPrefix(path, "src/contrib/Archive/") {
		return false
	}
	return path == "src/contrib/"+pkg+"_"+version+".tar.gz"
}

func (h *Handler) descriptionPublished(
	ctx context.Context,
	selected *upstream.Upstream,
	pkg, version string,
) (time.Time, bool) {
	descriptionPath := "web/packages/" + pkg + "/DESCRIPTION"
	result, err := h.cacheMgr.Get(ctx, CacheKey(descriptionPath), "cran", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		fetchResult, err := selected.Fetch(ctx, "/"+descriptionPath)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, selected.Name, nil
	})
	if err != nil {
		return time.Time{}, false
	}
	defer result.Reader.Close()
	body, err := io.ReadAll(io.LimitReader(result.Reader, 1<<20))
	if err != nil {
		return time.Time{}, false
	}
	text := string(body)
	if descriptionField(text, "Version") != version {
		return time.Time{}, false
	}
	raw := descriptionField(text, "Date/Publication")
	if raw == "" {
		return time.Time{}, false
	}
	published, err := parseCranPublished(raw)
	if err != nil {
		return time.Time{}, false
	}
	return published, true
}

// descriptionField extracts a DESCRIPTION-format field (RFC 822 shape).
func descriptionField(text, key string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	prefix := key + ":"
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

func parseCranPublished(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	for _, layout := range []string{
		"2006-01-02 15:04:05 UTC",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized CRAN date %q", raw)
}

// cranArtifactSourceID names the exact artifact and whether the timestamp came
// from DESCRIPTION or the approximate Last-Modified header.
func cranArtifactSourceID(upstreamSource, path string, exact bool) string {
	sourceKind := "last-modified"
	if exact {
		sourceKind = "description"
	}
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/cran-artifact-source/v1",
		upstreamSource,
		path,
		sourceKind,
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
