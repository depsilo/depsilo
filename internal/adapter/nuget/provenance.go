package nuget

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/packagepolicy"
)

// errRegistrationUnavailable marks registration metadata that cannot prove a
// version's publish time. The minimum-release-age gate treats it as missing
// provenance and fails closed for a bound ecosystem.
var errRegistrationUnavailable = errors.New("nuget registration metadata is unavailable")

const (
	// Package versions are immutable, so an exact version's publish time stays
	// valid for a long time; negative results retry quickly because a registry
	// hiccup or a missing page should not be pinned for the whole process.
	provenancePositiveTTL = time.Hour
	provenanceNegativeTTL = 30 * time.Second
	provenanceCacheLimit  = 4096
	registrationBaseTTL   = 10 * time.Minute
)

type nugetProvenanceEntry struct {
	published time.Time
	ok        bool
	expiresAt time.Time
}

type nugetServiceIndex struct {
	Resources []struct {
		ID   string          `json:"@id"`
		Type json.RawMessage `json:"@type"`
	} `json:"resources"`
}

type nugetRegistrationIndex struct {
	Items []nugetRegistrationPage `json:"items"`
}

type nugetRegistrationPage struct {
	ID    string                  `json:"@id"`
	Items []nugetRegistrationLeaf `json:"items"`
}

type nugetRegistrationPageDocument struct {
	Items []nugetRegistrationLeaf `json:"items"`
}

type nugetRegistrationLeaf struct {
	CatalogEntry struct {
		Version   string `json:"version"`
		Published string `json:"published"`
		Listed    *bool  `json:"listed"`
	} `json:"catalogEntry"`
}

// nugetPublishedForVersion resolves the exact package version's registration
// publish time through the configured upstream. The registration index and
// its pages are fetched with the same upstream selector as the flat-container
// download, so the timestamp governs bytes from the same source.
// publishedProvenance memoizes the registration lookup for repeated downloads
// of the same immutable package version. The metadata cache already bounds
// network traffic; this avoids re-parsing large registration documents on the
// hot path.
func (h *Handler) publishedProvenance(ctx context.Context, id, version string) (time.Time, bool) {
	key := strings.ToLower(id) + "\x00" + strings.ToLower(version)
	now := time.Now()
	h.provenanceMu.Lock()
	if entry, ok := h.provenanceCache[key]; ok && now.Before(entry.expiresAt) {
		h.provenanceMu.Unlock()
		return entry.published, entry.ok
	}
	h.provenanceMu.Unlock()

	published, ok, err := h.nugetPublishedForVersion(ctx, id, version)
	if err != nil {
		zap.L().Warn("nuget registration provenance unavailable; the age gate will fail closed",
			zap.String("package", id),
			zap.String("version", version),
			zap.Error(err),
		)
	}
	ttl := provenancePositiveTTL
	if !ok {
		ttl = provenanceNegativeTTL
	}
	h.provenanceMu.Lock()
	if h.provenanceCache == nil {
		h.provenanceCache = make(map[string]nugetProvenanceEntry)
	}
	h.provenanceCache[key] = nugetProvenanceEntry{published: published, ok: ok, expiresAt: now.Add(ttl)}
	for len(h.provenanceCache) > provenanceCacheLimit {
		for cachedKey := range h.provenanceCache {
			delete(h.provenanceCache, cachedKey)
			break
		}
	}
	h.provenanceMu.Unlock()
	return published, ok
}

func (h *Handler) nugetPublishedForVersion(ctx context.Context, id, version string) (time.Time, bool, error) {
	basePath, err := h.cachedRegistrationBase(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	idLower := strings.ToLower(id)
	indexPath := basePath + idLower + "/index.json"
	rawIndex, err := h.cachedUpstreamFetch(ctx, CacheKey("source/registration/"+idLower+"/index.json"), indexPath)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%w: registration index: %v", errRegistrationUnavailable, err)
	}
	var index nugetRegistrationIndex
	if err := json.Unmarshal(rawIndex, &index); err != nil {
		return time.Time{}, false, fmt.Errorf("%w: decode registration index: %v", errRegistrationUnavailable, err)
	}

	leaves := make([]nugetRegistrationLeaf, 0, 16)
	for _, page := range index.Items {
		if len(page.Items) > 0 {
			leaves = append(leaves, page.Items...)
			continue
		}
		if page.ID == "" {
			continue
		}
		pagePath, ok := registrationPagePath(page.ID, basePath)
		if !ok {
			continue
		}
		rawPage, err := h.cachedUpstreamFetch(
			ctx,
			CacheKey("source/registration/page/"+pathDigest(pagePath)),
			pagePath,
		)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("%w: registration page %s: %v", errRegistrationUnavailable, pagePath, err)
		}
		var pageDocument nugetRegistrationPageDocument
		if err := json.Unmarshal(rawPage, &pageDocument); err != nil {
			return time.Time{}, false, fmt.Errorf("%w: decode registration page %s: %v", errRegistrationUnavailable, pagePath, err)
		}
		leaves = append(leaves, pageDocument.Items...)
	}

	dialect, err := packagepolicy.DialectFor("nuget")
	if err != nil {
		return time.Time{}, false, err
	}
	for _, leaf := range leaves {
		comparison, err := dialect.CompareVersions(leaf.CatalogEntry.Version, version)
		if err != nil || comparison != 0 {
			continue
		}
		if leaf.CatalogEntry.Listed != nil && !*leaf.CatalogEntry.Listed {
			return time.Time{}, false, fmt.Errorf("%w: version %s is unlisted", errRegistrationUnavailable, version)
		}
		published, err := time.Parse(time.RFC3339, leaf.CatalogEntry.Published)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("%w: version %s has an invalid published time", errRegistrationUnavailable, version)
		}
		// NuGet resets `published` to 1900-01-01 for unlisted/deleted
		// versions; that sentinel is not the release time.
		if published.Year() <= 1900 {
			return time.Time{}, false, fmt.Errorf("%w: version %s has no release timestamp", errRegistrationUnavailable, version)
		}
		return published.UTC(), true, nil
	}
	return time.Time{}, false, fmt.Errorf("%w: version %s is not present in the registration index", errRegistrationUnavailable, version)
}

// cachedRegistrationBase resolves and memoizes the RegistrationsBaseUrl path.
// A transient service-index failure keeps the last good path so the
// registration fetch can surface its own error.
func (h *Handler) cachedRegistrationBase(ctx context.Context) (string, error) {
	now := time.Now()
	h.provenanceMu.Lock()
	if h.registrationBase != "" && now.Before(h.registrationBaseExpires) {
		base := h.registrationBase
		h.provenanceMu.Unlock()
		return base, nil
	}
	h.provenanceMu.Unlock()

	rawServiceIndex, err := h.cachedUpstreamFetch(ctx, CacheKey("source/v3/index.json"), "/v3/index.json")
	if err != nil {
		if stale := h.staleRegistrationBase(); stale != "" {
			return stale, nil
		}
		return "", fmt.Errorf("%w: service index: %v", errRegistrationUnavailable, err)
	}
	base, ok := registrationBasePath(rawServiceIndex)
	if !ok {
		if stale := h.staleRegistrationBase(); stale != "" {
			return stale, nil
		}
		return "", fmt.Errorf("%w: no RegistrationsBaseUrl resource", errRegistrationUnavailable)
	}
	h.provenanceMu.Lock()
	h.registrationBase = base
	h.registrationBaseExpires = now.Add(registrationBaseTTL)
	h.provenanceMu.Unlock()
	return base, nil
}

func (h *Handler) staleRegistrationBase() string {
	h.provenanceMu.Lock()
	defer h.provenanceMu.Unlock()
	return h.registrationBase
}

// cachedUpstreamFetch reads an upstream path through the shared metadata cache.
func (h *Handler) cachedUpstreamFetch(ctx context.Context, cacheKey, upstreamPath string) ([]byte, error) {
	result, err := h.cacheMgr.Get(ctx, cacheKey, "nuget", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		fetchResult, err := ups.Fetch(ctx, upstreamPath)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})
	if err != nil {
		return nil, err
	}
	defer result.Reader.Close()
	return io.ReadAll(result.Reader)
}

// registrationBasePath selects the best RegistrationsBaseUrl resource and
// returns its path relative to the upstream origin.
func registrationBasePath(data []byte) (string, bool) {
	var index nugetServiceIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return "", false
	}
	bestRank := 0
	bestPath := ""
	for _, resource := range index.Resources {
		for _, resourceType := range serviceIndexTypes(resource.Type) {
			rank := 0
			switch {
			case strings.HasPrefix(resourceType, "RegistrationsBaseUrl/3.6.0"):
				rank = 3
			case strings.HasPrefix(resourceType, "RegistrationsBaseUrl/3.4.0"):
				rank = 2
			case strings.HasPrefix(resourceType, "RegistrationsBaseUrl"):
				rank = 1
			}
			if rank <= bestRank {
				continue
			}
			parsed, err := url.Parse(resource.ID)
			if err != nil || parsed.Path == "" {
				continue
			}
			path := parsed.Path
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
			if !strings.HasSuffix(path, "/") {
				path += "/"
			}
			bestRank, bestPath = rank, path
		}
	}
	return bestPath, bestPath != ""
}

func serviceIndexTypes(raw json.RawMessage) []string {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil && single != "" {
		return []string{single}
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return list
	}
	return nil
}

// registrationPagePath converts a page @id into a path fetched through the
// configured upstream. Absolute page URLs keep their path; relative URLs
// resolve against the registration base.
func registrationPagePath(rawURL, basePath string) (string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Path == "" {
		return "", false
	}
	if parsed.IsAbs() {
		return parsed.Path, true
	}
	base, err := url.Parse(basePath)
	if err != nil {
		return "", false
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Path == "" {
		return "", false
	}
	return resolved.Path, true
}

func pathDigest(path string) string {
	digest := sha256.Sum256([]byte(path))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// nugetArtifactSourceID names the version identity whose registration entry
// declared the publish time. NuGet package versions are immutable, so the
// version identity is a stable artifact source even across mirror failover.
func nugetArtifactSourceID(id, version string) string {
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/nuget-artifact-source/v1",
		strings.ToLower(id),
		strings.ToLower(version),
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
