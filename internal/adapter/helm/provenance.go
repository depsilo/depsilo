package helm

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/upstream"
)

// errHelmIndexUnavailable marks chart downloads whose identity or timestamp
// cannot be proven from the repository index. The minimum-release-age gate
// treats it as missing provenance and refuses to serve.
var errHelmIndexUnavailable = errors.New("helm repository index does not declare the requested chart")

const (
	// Chart versions are immutable, so a resolved identity stays valid for a
	// long time; negative results retry quickly so a transient mirror problem
	// is not pinned for the process lifetime.
	helmIdentityPositiveTTL = time.Hour
	helmIdentityNegativeTTL = 30 * time.Second
	helmIdentityCacheLimit  = 4096

	// helmIndexScanLimit bounds how much of a repository index is scanned for
	// one artifact. Real indexes are a few megabytes; anything beyond this is
	// treated as unavailable provenance instead of buffering it.
	helmIndexScanLimit = 64 << 20
	helmIndexLineLimit = 1 << 20
)

type helmIdentityMemo struct {
	mu    sync.Mutex
	cache map[string]helmIdentityEntry
}

type helmIdentityEntry struct {
	identity  helmChartIdentity
	expiresAt time.Time
}

// helmChartIdentity is the chart coordinate declared by a repository index.
type helmChartIdentity struct {
	chart   string
	version string
}

func (i helmChartIdentity) ok() bool {
	return i.chart != "" && i.version != ""
}

// resolveChartIdentity returns the index-declared identity for a chart
// download and memoizes the result per upstream and path. The request path
// only carries `<name>-<version>.tgz`, and both components may contain dashes,
// so the index entry is the only authoritative source of the boundary.
func (h *Handler) resolveChartIdentity(ctx context.Context, artifactPath string) (helmChartIdentity, error) {
	if h.selector == nil || h.cacheMgr == nil {
		return helmChartIdentity{}, fmt.Errorf("%w: chart index resolution is not configured", errHelmIndexUnavailable)
	}
	selected, err := h.selector.Select(ctx)
	if err != nil {
		return helmChartIdentity{}, fmt.Errorf("%w: select upstream: %v", errHelmIndexUnavailable, err)
	}
	key := selected.Name + "\x00" + strings.ToLower(strings.TrimPrefix(artifactPath, "/"))
	now := time.Now()
	if h.identityMemo != nil {
		h.identityMemo.mu.Lock()
		if entry, found := h.identityMemo.cache[key]; found && now.Before(entry.expiresAt) {
			identity := entry.identity
			h.identityMemo.mu.Unlock()
			return identity, nil
		}
		h.identityMemo.mu.Unlock()
	}

	identity, err := h.lookupChartIdentity(ctx, selected, artifactPath)
	if err != nil {
		zap.L().Warn("helm chart identity unavailable; the age gate will fail closed",
			zap.String("path", artifactPath),
			zap.Error(err),
		)
	}
	if h.identityMemo != nil {
		ttl := helmIdentityPositiveTTL
		if !identity.ok() {
			ttl = helmIdentityNegativeTTL
		}
		h.identityMemo.mu.Lock()
		if h.identityMemo.cache == nil {
			h.identityMemo.cache = make(map[string]helmIdentityEntry)
		}
		h.identityMemo.cache[key] = helmIdentityEntry{identity: identity, expiresAt: now.Add(ttl)}
		for len(h.identityMemo.cache) > helmIdentityCacheLimit {
			for cachedKey := range h.identityMemo.cache {
				delete(h.identityMemo.cache, cachedKey)
				break
			}
		}
		h.identityMemo.mu.Unlock()
	}
	return identity, err
}

func (h *Handler) lookupChartIdentity(ctx context.Context, selected *upstream.Upstream, artifactPath string) (helmChartIdentity, error) {
	trimmed := strings.TrimPrefix(artifactPath, "/")
	base := path.Base(trimmed)
	if base == "" || base == ".tgz" || !strings.HasSuffix(base, ".tgz") {
		return helmChartIdentity{}, fmt.Errorf("%w: %s is not a chart archive", errHelmIndexUnavailable, artifactPath)
	}

	const indexPath = "index.yaml"
	result, err := h.cacheMgr.Get(ctx, CacheKey(indexPath), "helm", h.cfg.TTLIndex, func(ctx context.Context) (io.ReadCloser, string, int64, string, error) {
		fetched, err := selected.Fetch(ctx, "/"+indexPath)
		if err != nil {
			return nil, "", 0, "", err
		}
		return fetched.Body, fetched.ContentType, fetched.Size, selected.Name, nil
	})
	if err != nil {
		return helmChartIdentity{}, fmt.Errorf("%w: index.yaml unavailable: %v", errHelmIndexUnavailable, err)
	}
	defer result.Reader.Close()

	limited := &io.LimitedReader{R: result.Reader, N: helmIndexScanLimit + 1}
	matches, err := scanHelmIndex(limited, trimmed, base)
	if err != nil {
		return helmChartIdentity{}, err
	}
	if limited.N <= 0 {
		return helmChartIdentity{}, fmt.Errorf("%w: index.yaml exceeds %d bytes", errHelmIndexUnavailable, helmIndexScanLimit)
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return helmChartIdentity{}, fmt.Errorf("%w: %s", errHelmIndexUnavailable, artifactPath)
	default:
		return helmChartIdentity{}, fmt.Errorf("%w: %d index entries declare %s", errHelmIndexUnavailable, len(matches), artifactPath)
	}
}

// scanHelmIndex streams a Helm repository index and returns every distinct
// entry whose declared URL matches the requested artifact path and whose
// name/version reconstruct the requested filename.
func scanHelmIndex(reader io.Reader, artifactPath, base string) ([]helmChartIdentity, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), helmIndexLineLimit)

	var (
		inEntries  bool
		sawEntries bool
		chartKey   string
		inURLs     bool
		current    helmIndexEntry
		matches    []helmChartIdentity
	)
	flush := func() {
		if current.chart != "" && current.version != "" &&
			current.chart+"-"+current.version+".tgz" == base {
			for _, declared := range current.urls {
				if helmIndexURLMatches(declared, artifactPath, base) {
					matches = append(matches, helmChartIdentity{chart: current.chart, version: current.version})
					break
				}
			}
		}
		current = helmIndexEntry{}
		inURLs = false
	}
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		body := strings.TrimSpace(line)
		if body == "" || strings.HasPrefix(body, "#") || body == "---" {
			continue
		}
		indent := helmLineIndent(line)
		if indent == 0 {
			if body == "entries:" {
				inEntries, sawEntries = true, true
				continue
			}
			if inEntries {
				// The next top-level key ends the entries section.
				flush()
				break
			}
			continue
		}
		if !inEntries {
			continue
		}
		switch {
		case indent == 2 && helmIsListMarker(body):
			flush()
			current.chart = chartKey
			if rest := strings.TrimSpace(strings.TrimPrefix(body, "-")); rest != "" {
				helmApplyIndexField(&current, rest, &inURLs)
			}
		case indent == 2 && strings.HasSuffix(body, ":"):
			flush()
			chartKey = helmUnquoteYAML(strings.TrimSpace(strings.TrimSuffix(body, ":")))
		case indent == 4 && helmIsListMarker(body):
			// Only URL list items matter; other lists (maintainers, sources,
			// dependencies) are skipped so their nested keys cannot be
			// mistaken for the entry's own fields.
			if inURLs {
				if value := strings.TrimSpace(strings.TrimPrefix(body, "-")); value != "" {
					current.urls = append(current.urls, helmUnquoteYAML(value))
				}
			}
		case indent == 4:
			helmApplyIndexField(&current, body, &inURLs)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("%w: read index.yaml: %v", errHelmIndexUnavailable, err)
	}
	if !sawEntries {
		return nil, fmt.Errorf("%w: index.yaml has no entries section", errHelmIndexUnavailable)
	}
	flush()
	return matches, nil
}

// helmIndexEntry is the subset of one index entry the gate needs.
type helmIndexEntry struct {
	chart   string
	version string
	urls    []string
}

// helmApplyIndexField consumes one `key: value` line of an index entry. Any
// field other than urls ends an open URL list.
func helmApplyIndexField(entry *helmIndexEntry, line string, inURLs *bool) {
	key, value, found := strings.Cut(line, ":")
	if !found {
		*inURLs = false
		return
	}
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	switch key {
	case "name":
		*inURLs = false
		if parsed := helmUnquoteYAML(value); parsed != "" {
			entry.chart = parsed
		}
	case "version":
		*inURLs = false
		if parsed := helmUnquoteYAML(value); parsed != "" {
			entry.version = parsed
		}
	case "urls":
		if strings.HasPrefix(value, "[") {
			*inURLs = false
			inline := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
			for _, item := range strings.Split(inline, ",") {
				if parsed := helmUnquoteYAML(item); parsed != "" {
					entry.urls = append(entry.urls, parsed)
				}
			}
			return
		}
		// Only an empty block value opens a URL list; inline scalars are not
		// trusted because generated indexes always emit a block sequence.
		*inURLs = value == ""
	default:
		*inURLs = false
	}
}

// helmIndexURLMatches reports whether an index-declared URL refers to the
// requested artifact. Absolute and relative declarations are compared by path;
// a relocated mirror may also serve the same filename from another directory.
func helmIndexURLMatches(declared, artifactPath, base string) bool {
	declared = strings.TrimSpace(declared)
	if declared == "" {
		return false
	}
	candidate := declared
	if parsed, err := url.Parse(declared); err == nil && parsed.Path != "" {
		candidate = parsed.Path
	}
	candidate = helmCleanPath(candidate)
	requested := helmCleanPath(artifactPath)
	if candidate == requested {
		return true
	}
	return path.Base(candidate) == base
}

func helmCleanPath(value string) string {
	value = strings.TrimPrefix(value, "./")
	return strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(value, "/")), "/")
}

func helmIsListMarker(body string) bool {
	return body == "-" || strings.HasPrefix(body, "- ")
}

func helmLineIndent(line string) int {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	return indent
}

// helmUnquoteYAML strips the surrounding quotes generated indexes use for
// names and versions that need them. Escape sequences are deliberately not
// decoded: a value that still contains escapes fails the filename
// reconstruction check and the artifact stays refused.
func helmUnquoteYAML(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return strings.TrimSpace(value[1 : len(value)-1])
		}
	}
	return value
}
