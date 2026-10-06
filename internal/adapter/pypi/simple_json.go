package pypi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	stdhtml "html"
	"net/url"
	"strings"
	"sync"
	"time"
)

// renderSimpleHTML is the handler's renderer seam. Tests replace it to count
// render invocations without changing production behavior.
var renderSimpleHTML = renderSimpleHTMLFromJSON

const (
	renderMemoTTL        = 5 * time.Minute
	renderMemoMaxEntries = 64
	renderMemoMaxBytes   = 8 << 20
)

type renderedHTMLMemo struct {
	body      []byte
	expiresAt time.Time
}

type renderMemoState struct {
	mu    sync.Mutex
	cache map[string]renderedHTMLMemo
	bytes int
}

// cachedRenderedHTML memoizes the HTML rendering of a cached PEP 691 document
// in-process, keyed by the document content hash. A refreshed JSON document
// produces a new key, so a stale rendering cannot outlive its source entry.
func (h *Handler) cachedRenderedHTML(cacheKey string, body []byte) ([]byte, error) {
	memo := h.renderMemo
	if memo == nil {
		return renderSimpleHTML(body)
	}
	digest := sha256.Sum256(body)
	renderKey := cacheKey + "\x00" + hex.EncodeToString(digest[:8])
	now := time.Now()
	memo.mu.Lock()
	if entry, ok := memo.cache[renderKey]; ok && now.Before(entry.expiresAt) {
		rendered := entry.body
		memo.mu.Unlock()
		return rendered, nil
	}
	memo.mu.Unlock()

	rendered, err := renderSimpleHTML(body)
	if err != nil {
		return nil, err
	}
	memo.mu.Lock()
	if memo.cache == nil {
		memo.cache = make(map[string]renderedHTMLMemo)
	}
	memo.bytes += len(rendered)
	memo.cache[renderKey] = renderedHTMLMemo{body: rendered, expiresAt: now.Add(renderMemoTTL)}
	for (len(memo.cache) > renderMemoMaxEntries || memo.bytes > renderMemoMaxBytes) && len(memo.cache) > 0 {
		for cachedKey, entry := range memo.cache {
			memo.bytes -= len(entry.body)
			delete(memo.cache, cachedKey)
			break
		}
	}
	memo.mu.Unlock()
	return rendered, nil
}

// rewriteSignedJSONIndex converts a PEP 691 simple-index document into local
// signed artifact routes. Each file's authenticated claims carry the exact
// upstream that declared the URL plus the registry-provided upload-time, so an
// enabled minimum-release-age gate can decide without a second lookup.
func rewriteSignedJSONIndex(
	data []byte,
	pathPrefix, pageURL, adapterID, sourceID string,
	signingKey []byte,
) ([]byte, error) {
	if len(signingKey) == 0 {
		return nil, errors.New("PyPI JSON index requires an artifact signing key")
	}
	page, err := parseFetchableArtifactURL(pageURL)
	if err != nil {
		return nil, fmt.Errorf("invalid PyPI index response URL: %w", err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode PyPI simple index: %w", err)
	}
	files, ok := document["files"].([]any)
	if !ok {
		return nil, errors.New("PyPI simple index files list is missing or invalid")
	}
	pathPrefix = strings.TrimRight(pathPrefix, "/")

	for _, raw := range files {
		file, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("PyPI simple index file entry is invalid")
		}
		rawURL, ok := file["url"].(string)
		if !ok || rawURL == "" {
			return nil, errors.New("PyPI simple index file URL is missing")
		}
		reference, err := url.Parse(stdhtml.UnescapeString(rawURL))
		if err != nil {
			return nil, errors.New("PyPI simple index file URL is invalid")
		}
		target := page.ResolveReference(reference)
		if !obviousPythonArtifactURL(target) {
			// Leave unusual entries untouched; only recognizable Python
			// artifacts participate in the signed cache route.
			continue
		}
		if page.Scheme == "https" && target.Scheme == "http" {
			return nil, errArtifactSchemeDowngrade
		}
		target.Fragment = ""
		target.RawFragment = ""
		if err := validateArtifactTarget(target); err != nil {
			return nil, err
		}
		filename, err := artifactFilename(target)
		if err != nil {
			return nil, err
		}
		claims := externalArtifactClaims{Target: target.String(), Source: sourceID}
		if uploadTime, ok := file["upload-time"].(string); ok && uploadTime != "" {
			if _, err := time.Parse(time.RFC3339, uploadTime); err != nil {
				return nil, errors.New("PyPI simple index upload-time is invalid")
			}
			claims.UploadTime = uploadTime
		}
		token, err := encodeExternalArtifactToken(signingKey, adapterID, claims)
		if err != nil {
			return nil, err
		}
		file["url"] = pathPrefix + "/files/_external/" + token + "/" + url.PathEscape(filename)
	}

	return json.Marshal(document)
}

// renderSimpleHTMLFromJSON renders the cached PEP 691 document as the legacy
// HTML simple API for clients that did not advertise JSON support. Artifact
// URLs in the cached document are already local signed routes.
func renderSimpleHTMLFromJSON(data []byte) ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode cached PyPI simple index: %w", err)
	}
	files, ok := document["files"].([]any)
	if !ok {
		return nil, errors.New("cached PyPI simple index files list is missing or invalid")
	}

	var builder strings.Builder
	builder.WriteString("<!DOCTYPE html>\n<html>\n  <head>\n")
	builder.WriteString("    <meta name=\"pypi:repository-version\" content=\"1.0\">\n")
	builder.WriteString("  </head>\n  <body>\n")
	for _, raw := range files {
		file, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		href, _ := file["url"].(string)
		if href == "" {
			continue
		}
		filename, _ := file["filename"].(string)
		if filename == "" {
			if parsed, err := url.Parse(href); err == nil {
				filename, _ = artifactFilename(parsed)
			}
		}
		escapedHref := stdhtml.EscapeString(href)
		if hashes, ok := file["hashes"].(map[string]any); ok {
			if sha, ok := hashes["sha256"].(string); ok && sha != "" {
				escapedHref += "#sha256=" + sha
			}
		}
		builder.WriteString(`    <a href="` + escapedHref + `"`)
		if requiresPython, ok := file["requires-python"].(string); ok && requiresPython != "" {
			builder.WriteString(` data-requires-python="` + stdhtml.EscapeString(requiresPython) + `"`)
		}
		switch yanked := file["yanked"].(type) {
		case bool:
			if yanked {
				builder.WriteString(` data-yanked="true"`)
			}
		case string:
			builder.WriteString(` data-yanked="` + stdhtml.EscapeString(yanked) + `"`)
		}
		switch metadata := file["dist-info-metadata"].(type) {
		case bool:
			if metadata {
				builder.WriteString(` data-dist-info-metadata="true"`)
			}
		case map[string]any:
			if sha, ok := metadata["sha256"].(string); ok && sha != "" {
				builder.WriteString(` data-dist-info-metadata="sha256=` + sha + `"`)
			}
		}
		builder.WriteString(`>` + stdhtml.EscapeString(filename) + `</a>` + "\n")
	}
	builder.WriteString("  </body>\n</html>\n")
	return []byte(builder.String()), nil
}
