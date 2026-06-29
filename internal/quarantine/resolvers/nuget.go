package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// defaultNugetBase is the NuGet V3 catalog/registration root. NuGet's
// registration5-semver1 endpoint exposes per-version "leaf" documents
// whose `catalogEntry.published` field is the publish time. The path
// shape is /<lowercased-id>/<lowercased-version>.json — both must be
// lowercased per the NuGet spec.
const defaultNugetBase = "https://api.nuget.org/v3/registration5-semver1"

type nugetResolver struct {
	client *http.Client
	base   string
}

// nugetLeafDoc represents one published-version leaf. NuGet stores
// `published` in mixed casing across endpoints — this one is
// lowercased.
type nugetLeafDoc struct {
	CatalogEntry struct {
		Published string `json:"published"`
	} `json:"catalogEntry"`
}

func (r *nugetResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	pkgLower := strings.ToLower(pkg)
	verLower := strings.ToLower(version)
	url := fmt.Sprintf("%s/%s/%s.json", r.base, safePathSegment(pkgLower), safePathSegment(verLower))

	var doc nugetLeafDoc
	if err := fetchJSON(ctx, r.client, url, &doc); err != nil {
		return time.Time{}, err
	}
	if doc.CatalogEntry.Published == "" {
		return time.Time{}, ErrNotFound
	}
	// NuGet uses the "0001-01-01T00:00:00Z" sentinel for unlisted
	// (yanked) packages. Treat as not-found so quarantine doesn't
	// see them as "freshly published 2000 years ago".
	if strings.HasPrefix(doc.CatalogEntry.Published, "0001-01-01") {
		return time.Time{}, ErrNotFound
	}
	t, err := time.Parse(time.RFC3339, doc.CatalogEntry.Published)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse nuget published %q: %v", ErrUpstreamUnavailable, doc.CatalogEntry.Published, err)
	}
	return t.UTC(), nil
}
