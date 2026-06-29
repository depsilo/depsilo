package resolvers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"depsilo/internal/version"
)

// userAgent identifies us honestly per the transparency commitment in
// docs/adr/0003-supply-chain-control-point.md. We tell every registry
// who's calling so they can rate-limit / monitor / contact us if
// something's misbehaving — a "self-hosted supply-chain control
// point" tool that pretends to be a generic Go client would be
// dishonest in exactly the way we sell against.
func userAgent() string {
	return fmt.Sprintf("Depsilo/%s (+https://depsilo.com)", version.Version)
}

// fetchJSON GETs the URL and decodes the body into out. Wraps the
// sentinel errors so callers get errors.Is matches without each
// resolver re-implementing 404/5xx/network handling.
func fetchJSON(ctx context.Context, client *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrUpstreamUnavailable, err)
	}
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("%w: HTTP %d", ErrUpstreamUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		// 4xx other than 404 (auth, malformed package name, etc.) —
		// treat as not-found so the checker handles it via the
		// fail-closed/open policy rather than blocking on transient
		// upstream-state.
		return ErrNotFound
	}

	// Cap the read to avoid pathological responses pinning memory.
	// 16 MiB is generous — the largest npm packument we've measured
	// is ~3 MB for very long-lived packages.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("%w: read body: %v", ErrUpstreamUnavailable, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decode JSON: %v", ErrUpstreamUnavailable, err)
	}
	return nil
}

// headLastModified does a HEAD on url and returns the Last-Modified
// header parsed as time.Time. Used by resolvers for ecosystems whose
// upstream metadata API is expensive (conda repodata is MB-scale) or
// missing (alpine APKINDEX has no per-package timestamp).
//
// Falls back to GET with Range: bytes=0-0 if the upstream rejects
// HEAD (some CDNs do). The first-byte GET still surfaces Last-Modified
// in the response headers and the 1-byte body is cheap.
func headLastModified(ctx context.Context, client *http.Client, url string) (string, error) {
	tryReq := func(method string, extraHeader [2]string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent())
		if extraHeader[0] != "" {
			req.Header.Set(extraHeader[0], extraHeader[1])
		}
		return client.Do(req)
	}

	resp, err := tryReq(http.MethodHead, [2]string{})
	if err != nil {
		return "", fmt.Errorf("%w: HEAD %s: %v", ErrUpstreamUnavailable, url, err)
	}
	resp.Body.Close()

	// If HEAD is rejected (some CDNs, some Docker registries), fall
	// back to a 1-byte GET. Pretty much every HTTP stack surfaces
	// Last-Modified on a Range response.
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusForbidden {
		resp, err = tryReq(http.MethodGet, [2]string{"Range", "bytes=0-0"})
		if err != nil {
			return "", fmt.Errorf("%w: GET range %s: %v", ErrUpstreamUnavailable, url, err)
		}
		resp.Body.Close()
	}

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotFound
	case resp.StatusCode >= 400:
		return "", fmt.Errorf("%w: HTTP %d on %s", ErrUpstreamUnavailable, resp.StatusCode, url)
	}

	lm := resp.Header.Get("Last-Modified")
	if lm == "" {
		return "", fmt.Errorf("%w: no Last-Modified header on %s", ErrUpstreamUnavailable, url)
	}
	return lm, nil
}

// safePathSegment percent-escapes a path segment so package names
// with characters like '@' / '/' don't break URLs. Conservative —
// we encode anything that isn't [a-zA-Z0-9._-].
func safePathSegment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '.' || c == '_' || c == '-' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}
