package pypi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"io"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/packagepolicy"
)

// errArtifactIdentityUnavailable marks /files artifacts whose package and
// version cannot be proven from the strict PEP 427 / PEP 625 filename or from
// the declaring package's simple index. The gate refuses them while the
// known-malicious dataset or the minimum-release-age gate covers PyPI.
var errArtifactIdentityUnavailable = errors.New("pypi artifact identity is unavailable")

// identityEcosystem is the dialect and blocklist ecosystem every PyPI-shaped
// route (including extra indexes) canonicalizes to.
const identityEcosystem = "pypi"

const (
	// Artifact identities are immutable, so a resolved identity stays valid
	// for a long time; negative results retry quickly.
	artifactIdentityPositiveTTL = time.Hour
	artifactIdentityNegativeTTL = 30 * time.Second
	artifactIdentityCacheLimit  = 4096
	// artifactIndexBodyLimit bounds how much of one project index page is
	// read while verifying a legacy filename.
	artifactIndexBodyLimit = 16 << 20
)

// legacyArtifactSuffixes are the archive shapes whose filenames do not have to
// follow PEP 625, so the package/version boundary needs index verification.
var legacyArtifactSuffixes = []string{
	".tar.gz", ".tar.bz2", ".tar.xz", ".tar.Z", ".tgz", ".zip", ".egg", ".exe", ".msi",
}

var (
	simpleAnchorRe = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')[^>]*>(.*?)</a>`)
	htmlTagRe      = regexp.MustCompile(`(?s)<[^>]*>`)
)

// artifactIdentity is one authoritative (package, version) pair for a /files
// download.
type artifactIdentity struct {
	pkg     string
	version string
}

func (i artifactIdentity) ok() bool {
	return i.pkg != "" && i.version != ""
}

type artifactIdentityMemo struct {
	mu    sync.Mutex
	cache map[string]artifactIdentityEntry
}

type artifactIdentityEntry struct {
	identity  artifactIdentity
	expiresAt time.Time
}

// identityRequired reports whether every served artifact must carry an
// identity: the known-malicious dataset needs one to consult the blocklist,
// and the minimum-release-age gate needs one to age the release.
func (h *Handler) identityRequired() bool {
	return h.provenanceRequired || h.blocklistCovered
}

// SetBlocklistCovered records that the synced known-malicious dataset covers
// PyPI, so identity-less artifacts must be refused instead of bypassing the
// blocklist.
func (h *Handler) SetBlocklistCovered(covered bool) {
	h.blocklistCovered = covered
}

// resolveArtifactIdentity returns the identity for a /files download. PEP 427
// wheels and PEP 625 sdists carry it in the filename; every other archive
// shape is verified against the upstream simple index of each candidate
// package. Results are memoized per filename.
func (h *Handler) resolveArtifactIdentity(ctx context.Context, filename string) (artifactIdentity, error) {
	if identity, ok := strictArtifactIdentity(filename); ok {
		return identity, nil
	}
	if h.selector == nil {
		return artifactIdentity{}, fmt.Errorf("%w: no upstream selector", errArtifactIdentityUnavailable)
	}
	now := time.Now()
	if h.identityMemo != nil {
		h.identityMemo.mu.Lock()
		if entry, found := h.identityMemo.cache[filename]; found && now.Before(entry.expiresAt) {
			identity := entry.identity
			h.identityMemo.mu.Unlock()
			return identity, nil
		}
		h.identityMemo.mu.Unlock()
	}

	identity, err := h.lookupArtifactIdentity(ctx, filename)
	if err != nil {
		zap.L().Warn("pypi artifact identity unavailable; the gate will fail closed",
			zap.String("filename", filename),
			zap.Error(err),
		)
	}
	if h.identityMemo != nil {
		ttl := artifactIdentityPositiveTTL
		if !identity.ok() {
			ttl = artifactIdentityNegativeTTL
		}
		h.identityMemo.mu.Lock()
		if h.identityMemo.cache == nil {
			h.identityMemo.cache = make(map[string]artifactIdentityEntry)
		}
		h.identityMemo.cache[filename] = artifactIdentityEntry{identity: identity, expiresAt: now.Add(ttl)}
		for len(h.identityMemo.cache) > artifactIdentityCacheLimit {
			for cachedKey := range h.identityMemo.cache {
				delete(h.identityMemo.cache, cachedKey)
				break
			}
		}
		h.identityMemo.mu.Unlock()
	}
	return identity, err
}

func (h *Handler) lookupArtifactIdentity(ctx context.Context, filename string) (artifactIdentity, error) {
	base, ok := stripLegacyArtifactSuffix(filename)
	if !ok {
		return artifactIdentity{}, fmt.Errorf("%w: %s is not a recognizable Python artifact", errArtifactIdentityUnavailable, filename)
	}
	matches := make([]artifactIdentity, 0, 1)
	seen := make(map[string]bool, 4)
	dialect, err := packagepolicy.DialectFor(identityEcosystem)
	if err != nil {
		return artifactIdentity{}, fmt.Errorf("%w: %v", errArtifactIdentityUnavailable, err)
	}
	for _, candidate := range legacyCandidateSplits(base) {
		normalizedName, err := dialect.NormalizePackageName(candidate.pkg)
		if err != nil || seen[normalizedName] {
			continue
		}
		seen[normalizedName] = true
		version, err := packagepolicy.NormalizeVersion(identityEcosystem, candidate.version)
		if err != nil {
			continue
		}
		declared, err := h.simpleIndexDeclaresFile(ctx, normalizedName, filename)
		if err != nil {
			return artifactIdentity{}, err
		}
		if declared {
			matches = append(matches, artifactIdentity{pkg: normalizedName, version: version})
		}
	}
	if len(matches) != 1 {
		return artifactIdentity{}, fmt.Errorf("%w: %d simple-index candidates for %s", errArtifactIdentityUnavailable, len(matches), filename)
	}
	return matches[0], nil
}

// simpleIndexDeclaresFile fetches one project index page through the selected
// upstream and reports whether it declares exactly this artifact filename.
func (h *Handler) simpleIndexDeclaresFile(ctx context.Context, pkg, filename string) (bool, error) {
	selected, err := h.selector.Select(ctx)
	if err != nil {
		return false, fmt.Errorf("%w: select upstream: %v", errArtifactIdentityUnavailable, err)
	}
	result, err := selected.FetchWithHeaders(ctx, h.upstreamProjectPath(pkg), map[string]string{
		"Accept": "application/vnd.pypi.simple.v1+json, application/vnd.pypi.simple.v1+html;q=0.5, text/html;q=0.2",
	})
	if err != nil {
		return false, fmt.Errorf("%w: fetch simple index for %s: %v", errArtifactIdentityUnavailable, pkg, err)
	}
	defer result.Body.Close()
	if result.StatusCode == 404 {
		return false, nil
	}
	if result.StatusCode != 200 {
		return false, fmt.Errorf("%w: simple index for %s returned HTTP %d", errArtifactIdentityUnavailable, pkg, result.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(result.Body, artifactIndexBodyLimit+1))
	if err != nil {
		return false, fmt.Errorf("%w: read simple index for %s: %v", errArtifactIdentityUnavailable, pkg, err)
	}
	if len(body) > artifactIndexBodyLimit {
		return false, fmt.Errorf("%w: simple index for %s exceeds %d bytes", errArtifactIdentityUnavailable, pkg, artifactIndexBodyLimit)
	}
	declared, err := simpleIndexDeclaresFile(body, result.ContentType, filename)
	if err != nil {
		return false, fmt.Errorf("%w: %v", errArtifactIdentityUnavailable, err)
	}
	return declared, nil
}

// simpleIndexDeclaresFile inspects a PEP 691 JSON or PEP 503 HTML document for
// the exact artifact filename. Both the declared filename field and the URL
// basename are accepted; HTML pages are matched by anchor text or URL basename.
func simpleIndexDeclaresFile(body []byte, contentType, filename string) (bool, error) {
	trimmed := bytes.TrimSpace(body)
	if strings.Contains(strings.ToLower(contentType), "json") || (len(trimmed) > 0 && trimmed[0] == '{') {
		var document struct {
			Files []struct {
				Filename string `json:"filename"`
				URL      string `json:"url"`
			} `json:"files"`
		}
		if err := json.Unmarshal(trimmed, &document); err != nil {
			return false, fmt.Errorf("decode PyPI simple index: %w", err)
		}
		for _, file := range document.Files {
			if file.Filename == filename {
				return true, nil
			}
			if file.Filename == "" && linkBasename(file.URL) == filename {
				return true, nil
			}
		}
		return false, nil
	}
	for _, match := range simpleAnchorRe.FindAllStringSubmatch(string(body), -1) {
		href := match[1]
		if href == "" {
			href = match[2]
		}
		if linkBasename(href) == filename {
			return true, nil
		}
		text := strings.TrimSpace(stdhtml.UnescapeString(htmlTagRe.ReplaceAllString(match[3], "")))
		if text == filename {
			return true, nil
		}
	}
	return false, nil
}

func strictArtifactIdentity(filename string) (artifactIdentity, bool) {
	pkg, version := packagekey.ParsePypiFilename(filename)
	if pkg == "" || version == "" {
		return artifactIdentity{}, false
	}
	return artifactIdentity{pkg: pkg, version: version}, true
}

func stripLegacyArtifactSuffix(filename string) (string, bool) {
	lower := strings.ToLower(filename)
	for _, suffix := range legacyArtifactSuffixes {
		if !strings.HasSuffix(lower, strings.ToLower(suffix)) {
			continue
		}
		base := filename[:len(filename)-len(suffix)]
		if base == "" {
			return "", false
		}
		return base, true
	}
	return "", false
}

type legacyCandidate struct {
	pkg     string
	version string
}

// legacyCandidateSplits tries every hyphen boundary. Verification against the
// declaring package index rejects the wrong splits, exactly like the RubyGems
// and Helm resolvers.
func legacyCandidateSplits(base string) []legacyCandidate {
	candidates := make([]legacyCandidate, 0, strings.Count(base, "-"))
	for index := 1; index < len(base)-1; index++ {
		if base[index] != '-' {
			continue
		}
		pkg, version := base[:index], base[index+1:]
		if pkg == "" || version == "" {
			continue
		}
		candidates = append(candidates, legacyCandidate{pkg: pkg, version: version})
	}
	return candidates
}

func linkBasename(raw string) string {
	raw = strings.TrimSpace(stdhtml.UnescapeString(raw))
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	escaped := parsed.EscapedPath()
	index := strings.LastIndex(escaped, "/")
	name, err := url.PathUnescape(escaped[index+1:])
	if err != nil {
		return ""
	}
	return name
}
