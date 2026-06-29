package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// defaultCargoBase is the crates.io API v1 endpoint. /api/v1/crates/<pkg>
// returns the crate doc including a `versions` array; each entry
// carries `num` (the version string) and `created_at` (RFC 3339).
const defaultCargoBase = "https://crates.io/api/v1"

type cargoResolver struct {
	client *http.Client
	base   string
}

type cargoCrateDoc struct {
	Versions []struct {
		Num       string `json:"num"`
		CreatedAt string `json:"created_at"`
	} `json:"versions"`
}

func (r *cargoResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	// crates.io requires User-Agent for any API request; util.fetchJSON
	// already sets one. Otherwise this is a clean GET-and-find call.
	url := fmt.Sprintf("%s/crates/%s", r.base, safePathSegment(pkg))

	var doc cargoCrateDoc
	if err := fetchJSON(ctx, r.client, url, &doc); err != nil {
		return time.Time{}, err
	}
	for _, v := range doc.Versions {
		if v.Num != version {
			continue
		}
		if v.CreatedAt == "" {
			return time.Time{}, ErrNotFound
		}
		t, err := time.Parse(time.RFC3339, v.CreatedAt)
		if err != nil {
			return time.Time{}, fmt.Errorf("%w: parse cargo created_at %q: %v", ErrUpstreamUnavailable, v.CreatedAt, err)
		}
		return t.UTC(), nil
	}
	return time.Time{}, ErrNotFound
}
