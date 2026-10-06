package cargo

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/packagepolicy"
)

// errCargoIndexUnavailable marks index metadata that cannot prove a version's
// publish time. The minimum-release-age gate treats it as missing provenance
// and fails closed for a bound ecosystem.
var errCargoIndexUnavailable = errors.New("cargo index metadata is unavailable")

const (
	// A crate version is immutable, so a resolved pubtime stays valid for a
	// long time; negative results retry quickly so a transient mirror problem
	// is not pinned for the process lifetime.
	cargoProvenancePositiveTTL = time.Hour
	cargoProvenanceNegativeTTL = 30 * time.Second
	cargoProvenanceCacheLimit  = 4096
)

type cargoProvenanceMemo struct {
	mu    sync.Mutex
	cache map[string]cargoProvenanceEntry
}

type cargoProvenanceEntry struct {
	published time.Time
	cksum     string
	ok        bool
	expiresAt time.Time
}

type cargoIndexEntry struct {
	Version string `json:"vers"`
	CKSum   string `json:"cksum"`
	PubTime string `json:"pubtime"`
}

// cargoIndexPath derives the crates.io sparse-index layout for a crate name.
// Names are stored lowercase in the index regardless of the spelling used by
// the client's download URL.
func cargoIndexPath(crateName string) (prefix, name string, ok bool) {
	name = strings.ToLower(strings.TrimSpace(crateName))
	if name == "" || strings.ContainsAny(name, "/\\\x00\r\n") {
		return "", "", false
	}
	switch len(name) {
	case 1:
		return "1", name, true
	case 2:
		return "2", name, true
	case 3:
		return "3/" + name[:1], name, true
	default:
		return name[:2] + "/" + name[2:4], name, true
	}
}

// publishedProvenance memoizes the crate index lookup for repeated downloads
// of the same immutable version.
func (h *Handler) publishedProvenance(ctx context.Context, crate, version string) (time.Time, string, bool) {
	memo := h.provenanceMemo
	key := strings.ToLower(crate) + "\x00" + strings.ToLower(version)
	now := time.Now()
	if memo != nil {
		memo.mu.Lock()
		if entry, ok := memo.cache[key]; ok && now.Before(entry.expiresAt) {
			memo.mu.Unlock()
			return entry.published, entry.cksum, entry.ok
		}
		memo.mu.Unlock()
	}

	published, cksum, ok, err := h.resolvePublished(ctx, crate, version)
	if err != nil {
		zap.L().Warn("cargo index provenance unavailable; the age gate will fail closed",
			zap.String("crate", crate),
			zap.String("version", version),
			zap.Error(err),
		)
	}
	if memo != nil {
		ttl := cargoProvenancePositiveTTL
		if !ok {
			ttl = cargoProvenanceNegativeTTL
		}
		memo.mu.Lock()
		if memo.cache == nil {
			memo.cache = make(map[string]cargoProvenanceEntry)
		}
		memo.cache[key] = cargoProvenanceEntry{published: published, cksum: cksum, ok: ok, expiresAt: now.Add(ttl)}
		for len(memo.cache) > cargoProvenanceCacheLimit {
			for cachedKey := range memo.cache {
				delete(memo.cache, cachedKey)
				break
			}
		}
		memo.mu.Unlock()
	}
	return published, cksum, ok
}

func (h *Handler) resolvePublished(ctx context.Context, crate, version string) (time.Time, string, bool, error) {
	prefix, name, ok := cargoIndexPath(crate)
	if !ok {
		return time.Time{}, "", false, fmt.Errorf("%w: invalid crate name", errCargoIndexUnavailable)
	}
	indexPath := prefix + "/" + name
	result, err := h.cacheMgr.Get(ctx, IndexCacheKey(prefix, name), "cargo", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		fetchResult, err := ups.Fetch(ctx, "/"+indexPath)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})
	if err != nil {
		return time.Time{}, "", false, fmt.Errorf("%w: index %s: %v", errCargoIndexUnavailable, indexPath, err)
	}
	defer result.Reader.Close()

	dialect, err := packagepolicy.DialectFor("cargo")
	if err != nil {
		return time.Time{}, "", false, err
	}
	decoder := json.NewDecoder(result.Reader)
	for {
		var entry cargoIndexEntry
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return time.Time{}, "", false, fmt.Errorf("%w: decode index %s: %v", errCargoIndexUnavailable, indexPath, err)
		}
		comparison, err := dialect.CompareVersions(entry.Version, version)
		if err != nil || comparison != 0 {
			continue
		}
		if entry.PubTime == "" || entry.CKSum == "" {
			return time.Time{}, "", false, fmt.Errorf("%w: version %s has no pubtime or checksum", errCargoIndexUnavailable, version)
		}
		published, err := time.Parse(time.RFC3339, entry.PubTime)
		if err != nil {
			return time.Time{}, "", false, fmt.Errorf("%w: version %s has an invalid pubtime", errCargoIndexUnavailable, version)
		}
		return published.UTC(), entry.CKSum, true, nil
	}
	return time.Time{}, "", false, fmt.Errorf("%w: version %s is not present in the index", errCargoIndexUnavailable, version)
}

// cargoArtifactSourceID names the exact index declaration (crate, version,
// checksum) that provided the publish time.
func cargoArtifactSourceID(crate, version, cksum string) string {
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/cargo-artifact-source/v1",
		strings.ToLower(crate),
		version,
		strings.ToLower(cksum),
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
