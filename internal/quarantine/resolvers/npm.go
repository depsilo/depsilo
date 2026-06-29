package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// defaultNpmBase is the public registry URL. npm hosts the entire
// "packument" — the full per-package metadata document — at this
// endpoint; `time[version]` carries the publish timestamps we want.
//
// We don't go through whatever mirror the operator configured for
// the npm adapter because mirrors are often metadata-stale (e.g.
// tunafrog updates packuments hourly) and the quarantine decision
// needs the AUTHORITATIVE timestamp.
const defaultNpmBase = "https://registry.npmjs.org"

type npmResolver struct {
	client *http.Client
	base   string // overridable for tests
}

// npmPackument is a tiny subset of the npm packument schema. We only
// need the time map; ignoring everything else keeps memory bounded
// for very large packuments (react @ ~3 MB).
type npmPackument struct {
	Time map[string]string `json:"time"`
}

func (r *npmResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	// Scoped packages ("@scope/name") MUST URL-encode the slash —
	// registry treats unencoded slashes as path separators. The
	// rest of the package name follows the same percent-encoding
	// rules as RFC 3986 path segments.
	encoded := strings.ReplaceAll(pkg, "/", "%2F")
	url := fmt.Sprintf("%s/%s", r.base, encoded)

	var doc npmPackument
	if err := fetchJSON(ctx, r.client, url, &doc); err != nil {
		return time.Time{}, err
	}
	raw, ok := doc.Time[version]
	if !ok || raw == "" {
		return time.Time{}, ErrNotFound
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse npm time %q: %v", ErrUpstreamUnavailable, raw, err)
	}
	return t.UTC(), nil
}
