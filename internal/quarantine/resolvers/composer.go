package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// defaultComposerBase is the Packagist V2 metadata endpoint. The
// /p2/<vendor>/<pkg>.json shape returns the full per-package metadata
// with a `packages[<vendor/pkg>]` array; each entry is a version
// manifest including a `time` field (RFC 3339).
const defaultComposerBase = "https://repo.packagist.org"

type composerResolver struct {
	client *http.Client
	base   string
}

// composerPackageDoc is intentionally minimal — packagist responses
// for popular packages can be hundreds of KB and most fields are
// composer-specific noise we don't need.
type composerPackageDoc struct {
	Packages map[string][]struct {
		Version string `json:"version"`
		Time    string `json:"time"`
	} `json:"packages"`
}

func (r *composerResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	// composer package names are always "vendor/name" — the slash
	// MUST be preserved (it's the path separator on the wire), so
	// we only escape per-segment, not the whole identifier.
	url := fmt.Sprintf("%s/p2/%s.json", r.base, pkg)

	var doc composerPackageDoc
	if err := fetchJSON(ctx, r.client, url, &doc); err != nil {
		return time.Time{}, err
	}
	entries, ok := doc.Packages[pkg]
	if !ok || len(entries) == 0 {
		return time.Time{}, ErrNotFound
	}
	for _, e := range entries {
		if e.Version != version {
			continue
		}
		if e.Time == "" {
			return time.Time{}, ErrNotFound
		}
		t, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: parse composer time %q: %v", ErrUpstreamUnavailable, e.Time, err)
		}
		return t.UTC(), nil
	}
	return time.Time{}, ErrNotFound
}
