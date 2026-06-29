package resolvers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// defaultCranBase is CRAN's structured src directory. Latest version
// lives at /src/contrib/<pkg>_<ver>.tar.gz with the version DESCRIPTION
// at /web/packages/<pkg>/DESCRIPTION. Older versions go under
// /src/contrib/Archive/<pkg>/<pkg>_<ver>.tar.gz. We rely on the
// DESCRIPTION's `Date/Publication` field, which CRAN updates when the
// archive is published.
const defaultCranBase = "https://cran.r-project.org"

type cranResolver struct {
	client *http.Client
	base   string
}

func (r *cranResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	// The /web/packages/<pkg>/DESCRIPTION endpoint always reflects the
	// LATEST version. For a version match we have to read its value
	// and compare; if the requested version isn't current, we fall
	// back to a HEAD on the archive tarball and use Last-Modified.
	descURL := fmt.Sprintf("%s/web/packages/%s/DESCRIPTION", r.base, safePathSegment(pkg))
	desc, err := fetchText(ctx, r.client, descURL)
	if err == nil {
		descVer := descField(desc, "Version")
		descDate := descField(desc, "Date/Publication")
		if descVer == version && descDate != "" {
			t, err := parseCranDate(descDate)
			if err == nil {
				return t, nil
			}
		}
	}

	// Older version → archive. CRAN serves tarballs with a usable
	// Last-Modified, which approximates the publish time.
	archiveURL := fmt.Sprintf("%s/src/contrib/Archive/%s/%s_%s.tar.gz",
		r.base, safePathSegment(pkg), safePathSegment(pkg), safePathSegment(version))
	lm, err := headLastModified(ctx, r.client, archiveURL)
	if err != nil {
		return time.Time{}, err
	}
	t, err := http.ParseTime(lm)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse CRAN Last-Modified %q: %v", ErrUpstreamUnavailable, lm, err)
	}
	return t.UTC(), nil
}

// fetchText GETs the URL and returns the body as a string. Used for
// CRAN's plain-text DESCRIPTION files.
func fetchText(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpstreamUnavailable, err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", ErrNotFound
	case resp.StatusCode >= 400:
		return "", fmt.Errorf("%w: HTTP %d", ErrUpstreamUnavailable, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("%w: read body: %v", ErrUpstreamUnavailable, err)
	}
	return string(body), nil
}

// descField extracts a value from a DESCRIPTION-format file (RFC 822
// shape: "Key: value", continuation lines start with whitespace).
// Returns empty string if absent.
func descField(text, key string) string {
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

// parseCranDate handles the "YYYY-MM-DD HH:MM:SS UTC" and the bare
// "YYYY-MM-DD" forms CRAN emits. Some packages drop the time + UTC
// suffix; some leave them on.
func parseCranDate(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02 15:04:05 UTC",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized CRAN date format %q", s)
}
