package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// defaultRubygemsBase is the RubyGems API v1 root. The versions
// endpoint returns an array of every published version with
// per-version timestamps. Bigger response than ideal for a one-shot
// lookup (popular gems can have 100+ entries) but the API doesn't
// expose a (gem, version) → timestamp shortcut.
const defaultRubygemsBase = "https://rubygems.org/api/v1"

type rubygemsResolver struct {
	client *http.Client
	base   string
}

type rubygemsVersion struct {
	Number    string `json:"number"`
	CreatedAt string `json:"created_at"`
}

func (r *rubygemsResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	url := fmt.Sprintf("%s/versions/%s.json", r.base, safePathSegment(pkg))

	var versions []rubygemsVersion
	if err := fetchJSON(ctx, r.client, url, &versions); err != nil {
		return time.Time{}, err
	}
	for _, v := range versions {
		if v.Number != version {
			continue
		}
		if v.CreatedAt == "" {
			return time.Time{}, ErrNotFound
		}
		t, err := time.Parse(time.RFC3339, v.CreatedAt)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: parse rubygems created_at %q: %v", ErrUpstreamUnavailable, v.CreatedAt, err)
		}
		return t.UTC(), nil
	}
	return time.Time{}, ErrNotFound
}
