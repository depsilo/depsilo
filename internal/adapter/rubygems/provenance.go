package rubygems

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// errRubyGemsIndexUnavailable marks compact-index metadata that cannot prove
// an artifact's release provenance. The minimum-release-age gate treats it as
// missing provenance and fails closed for a bound ecosystem.
var errRubyGemsIndexUnavailable = errors.New("rubygems compact index metadata is unavailable")

const (
	// Gem versions are immutable, so a resolved created_at stays valid for a
	// long time; negative results retry quickly so a transient mirror problem
	// is not pinned for the process lifetime.
	rubygemsProvenancePositiveTTL = time.Hour
	rubygemsProvenanceNegativeTTL = 30 * time.Second
	rubygemsProvenanceCacheLimit  = 4096
)

type rubygemsProvenanceMemo struct {
	mu    sync.Mutex
	cache map[string]rubygemsProvenance
}

type rubygemsProvenance struct {
	resolution rubygemsResolution
	expiresAt  time.Time
}

type rubygemsResolution struct {
	gem       string
	version   string
	cksum     string
	published time.Time
	ok        bool
}

// publishedProvenance resolves the compact-index entry for a downloaded gem
// filename and memoizes the result. The download path only carries the
// combined `name-version[-platform]` filename, so the resolver tries every
// hyphen split and requires exactly one authoritative compact-index match.
func (h *Handler) publishedProvenance(ctx context.Context, fullName string) (rubygemsResolution, error) {
	key := strings.ToLower(fullName)
	now := time.Now()
	if h.provenanceMemo != nil {
		h.provenanceMemo.mu.Lock()
		if entry, found := h.provenanceMemo.cache[key]; found && now.Before(entry.expiresAt) {
			resolution := entry.resolution
			h.provenanceMemo.mu.Unlock()
			return resolution, nil
		}
		h.provenanceMemo.mu.Unlock()
	}

	resolution, err := h.resolveProvenance(ctx, fullName)
	if err != nil {
		zap.L().Warn("rubygems compact index provenance unavailable; the age gate will fail closed",
			zap.String("artifact", fullName),
			zap.Error(err),
		)
	}
	if h.provenanceMemo != nil {
		ttl := rubygemsProvenancePositiveTTL
		if !resolution.ok {
			ttl = rubygemsProvenanceNegativeTTL
		}
		h.provenanceMemo.mu.Lock()
		if h.provenanceMemo.cache == nil {
			h.provenanceMemo.cache = make(map[string]rubygemsProvenance)
		}
		h.provenanceMemo.cache[key] = rubygemsProvenance{resolution: resolution, expiresAt: now.Add(ttl)}
		for len(h.provenanceMemo.cache) > rubygemsProvenanceCacheLimit {
			for cachedKey := range h.provenanceMemo.cache {
				delete(h.provenanceMemo.cache, cachedKey)
				break
			}
		}
		h.provenanceMemo.mu.Unlock()
	}
	return resolution, err
}

func (h *Handler) resolveProvenance(ctx context.Context, fullName string) (rubygemsResolution, error) {
	matches := make([]rubygemsResolution, 0, 1)
	seenGems := make(map[string]bool, 4)
	for index := 0; index < len(fullName); index++ {
		if fullName[index] != '-' {
			continue
		}
		gem, version := fullName[:index], fullName[index+1:]
		if gem == "" || version == "" || seenGems[gem] {
			continue
		}
		seenGems[gem] = true
		info, err := h.fetchInfo(ctx, gem)
		if err != nil {
			if strings.Contains(err.Error(), "returned 404") {
				continue
			}
			return rubygemsResolution{}, err
		}
		versionToken, cksum, createdAt, found := findInfoVersion(info, version)
		if !found {
			continue
		}
		// A platform artifact (1.17.2-x86_64-linux) is the same release as the
		// platform-less gem version when the compact index lists that base
		// version too. Report the base identity so dataset rows and allow
		// rules written for 1.17.2 also govern the platform artifacts.
		resolution := rubygemsResolution{gem: gem, version: baseVersionToken(info, versionToken), cksum: cksum}
		if cksum != "" && createdAt != "" {
			if published, err := time.Parse(time.RFC3339, createdAt); err == nil {
				resolution.published = published.UTC()
				resolution.ok = true
			}
		}
		matches = append(matches, resolution)
	}
	if len(matches) != 1 {
		return rubygemsResolution{}, fmt.Errorf("%w: %d compact-index candidates for %s", errRubyGemsIndexUnavailable, len(matches), fullName)
	}
	return matches[0], nil
}

// baseVersionToken returns the platform-less version for a compact-index
// version token. The prefix must itself be a listed version of the same gem,
// so a hyphen inside the version is never stripped by guesswork.
func baseVersionToken(info []byte, token string) string {
	if !strings.Contains(token, "-") {
		return token
	}
	listed := make(map[string]bool, 64)
	for _, line := range strings.Split(string(info), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		end := strings.IndexAny(line, " |")
		if end <= 0 {
			continue
		}
		listed[line[:end]] = true
	}
	for index := 0; index < len(token); index++ {
		if token[index] != '-' {
			continue
		}
		if listed[token[:index]] {
			return token[:index]
		}
	}
	return token
}

func (h *Handler) fetchInfo(ctx context.Context, gem string) ([]byte, error) {
	path := "info/" + gem
	result, err := h.cacheMgr.Get(ctx, CacheKey(path), "rubygems", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		ups, err := h.selector.Select(ctx)
		if err != nil {
			return nil, "", 0, "", err
		}
		fetchResult, err := ups.Fetch(ctx, "/"+path)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetchResult.Body, fetchResult.ContentType, fetchResult.Size, ups.Name, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: info/%s: %v", errRubyGemsIndexUnavailable, gem, err)
	}
	defer result.Reader.Close()
	return io.ReadAll(result.Reader)
}

func findInfoVersion(info []byte, wanted string) (version, cksum, createdAt string, ok bool) {
	for _, line := range strings.Split(string(info), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		versionToken, checksum, created, found := parseRubyGemsInfoLine(line)
		if !found || versionToken != wanted {
			continue
		}
		return versionToken, checksum, created, true
	}
	return "", "", "", false
}

// parseRubyGemsInfoLine parses one compact-index line:
//
//	<version> [deps]|checksum:<sha256>,ruby:...,created_at:<RFC3339>
func parseRubyGemsInfoLine(line string) (version, checksum, createdAt string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", "", "", false
	}
	end := strings.IndexAny(line, " |")
	if end <= 0 {
		return "", "", "", false
	}
	version = line[:end]
	pipe := strings.IndexByte(line, '|')
	if pipe < 0 || pipe+1 >= len(line) {
		return "", "", "", false
	}
	for _, attr := range strings.Split(line[pipe+1:], ",") {
		attr = strings.TrimSpace(attr)
		switch {
		case strings.HasPrefix(attr, "checksum:"):
			checksum = strings.TrimPrefix(attr, "checksum:")
		case strings.HasPrefix(attr, "created_at:"):
			createdAt = strings.TrimPrefix(attr, "created_at:")
		}
	}
	return version, checksum, createdAt, true
}

// rubygemsArtifactSourceID names the exact compact-index declaration
// (gem, version, checksum) that provided the publish time.
func rubygemsArtifactSourceID(gem, version, cksum string) string {
	hash := sha256.New()
	for _, value := range []string{
		"depsilo/rubygems-artifact-source/v1",
		gem,
		version,
		strings.ToLower(cksum),
	} {
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return base64.RawURLEncoding.EncodeToString(hash.Sum(nil))
}
