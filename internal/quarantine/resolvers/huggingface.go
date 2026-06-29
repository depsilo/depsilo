package resolvers

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// defaultHFBase is the Hugging Face Hub API. /api/models/<id> returns
// model metadata including `lastModified` (RFC 3339). HF doesn't have
// the granular per-version metadata concept the package registries do
// — model "versions" are git refs (commits, branches, tags) — so we
// treat the model's lastModified as the quarantine signal. For a
// freshly published model this matches the publish time; for a
// well-aged model that just had a minor README update, the freshness
// window resets, which is the safer reading anyway.
const defaultHFBase = "https://huggingface.co/api"

type hfResolver struct {
	client *http.Client
	base   string
}

type hfModelDoc struct {
	LastModified string `json:"lastModified"`
}

func (r *hfResolver) Lookup(ctx context.Context, pkg, version string) (time.Time, error) {
	// Hugging Face model IDs are "owner/name" — preserve the slash
	// on the wire. `version` is the revision (branch, tag, commit).
	// When a specific revision is supplied we use the /api/models/<id>
	// /revision/<ref> endpoint to get per-revision lastModified. With
	// an empty version, the default model document carries the model-
	// level lastModified, which is what we want.
	var url string
	if version != "" && version != "main" {
		url = fmt.Sprintf("%s/models/%s/revision/%s", r.base, pkg, safePathSegment(version))
	} else {
		url = fmt.Sprintf("%s/models/%s", r.base, pkg)
	}

	var doc hfModelDoc
	if err := fetchJSON(ctx, r.client, url, &doc); err != nil {
		return time.Time{}, err
	}
	if doc.LastModified == "" {
		return time.Time{}, ErrNotFound
	}
	t, err := time.Parse(time.RFC3339, doc.LastModified)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: parse hf lastModified %q: %v", ErrUpstreamUnavailable, doc.LastModified, err)
	}
	return t.UTC(), nil
}
