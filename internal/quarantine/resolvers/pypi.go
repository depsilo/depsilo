package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// defaultPypiBase serves the per-package + per-version JSON metadata
// endpoint. The /<pkg>/<ver>/json shape returns release-level info
// including upload_time_iso_8601 — the timestamp we want. Note that
// /<pkg>/json (without the version) returns the whole project doc
// which is bigger and unnecessary.
const defaultPypiBase = "https://pypi.org/pypi"

type pypiResolver struct {
	client *http.Client
	base   string
}

// pypiVersionDoc mirrors a tiny part of the PyPI JSON release shape.
// urls[] entries each represent a distribution file (sdist, wheel).
// All distributions for a version share the same upload day in
// practice; we take the earliest as the canonical publish time so
// "version was released" matches "first file appeared upstream".
type pypiVersionDoc struct {
	URLs []struct {
		UploadTimeISO string `json:"upload_time_iso_8601"`
	} `json:"urls"`
}

func (r *pypiResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	url := fmt.Sprintf("%s/%s/%s/json", r.base, safePathSegment(pkg), safePathSegment(version))

	var doc pypiVersionDoc
	if err := fetchJSON(ctx, r.client, url, &doc); err != nil {
		return time.Time{}, err
	}
	if len(doc.URLs) == 0 {
		return time.Time{}, ErrNotFound
	}

	// Find the earliest upload across all distributions.
	var earliest time.Time
	for _, u := range doc.URLs {
		if u.UploadTimeISO == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, u.UploadTimeISO)
		if err != nil {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}
	if earliest.IsZero() {
		return time.Time{}, fmt.Errorf("%w: no parseable upload_time on %s", ErrUpstreamUnavailable, url)
	}
	return earliest.UTC(), nil
}
