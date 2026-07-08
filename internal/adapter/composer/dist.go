package composer

import (
	"encoding/json"
	"strings"
)

// ParseDistPath splits a mirror-template dist request path into its
// components. The path shape is what the mirrors entry injected by
// RewritePackagesJSON produces:
//
//	dist/<vendor>/<name>/<version_normalized>/<reference>.<type>
//
// The normalized version may itself contain slashes (branch versions
// like "dev-feature/x"), so the version is everything between the
// package name and the final segment.
func ParseDistPath(path string) (vendor, pkg, versionNorm, reference, ext string, ok bool) {
	if !strings.HasPrefix(path, "dist/") {
		return "", "", "", "", "", false
	}
	segs := strings.Split(strings.TrimPrefix(path, "dist/"), "/")
	if len(segs) < 4 {
		return "", "", "", "", "", false
	}
	vendor, pkg = segs[0], segs[1]
	versionNorm = strings.Join(segs[2:len(segs)-1], "/")
	last := segs[len(segs)-1]
	dot := strings.Index(last, ".")
	if vendor == "" || pkg == "" || versionNorm == "" || dot <= 0 || dot == len(last)-1 {
		return "", "", "", "", "", false
	}
	return vendor, pkg, versionNorm, last[:dot], last[dot+1:], true
}

// distEntry is the narrow slice of a p2 version manifest the dist
// handler needs: the pretty version (the string the quarantine
// resolver matches against p2 metadata), the normalized version
// (what the mirror URL carries), and the real dist location.
type distEntry struct {
	Version           string `json:"version"`
	VersionNormalized string `json:"version_normalized"`
	Dist              struct {
		URL       string `json:"url"`
		Type      string `json:"type"`
		Reference string `json:"reference"`
	} `json:"dist"`
}

// findDistEntry locates the version manifest matching a dist request
// inside a p2 metadata document. p2 files are "minified" (Composer's
// MetadataMinifier): the first entry is complete and every later
// entry carries only the top-level keys that changed; a key that
// disappeared is emitted with the literal string value "__unset".
// Entries are expanded with the same carry-forward rule Composer's
// MetadataMinifier::expand applies.
//
// A version match alone is NOT sufficient to serve bytes: metadata
// moves (dev-branch pushes, re-tags), so the entry's dist.reference
// must equal the reference the client's lockfile pinned — otherwise
// the proxy would stream a different commit's bytes than a direct
// download of the pinned URL, and cache them under the pinned
// reference's key. On mismatch the request 404s and composer falls
// back to the original dist URL, which is always the correct bytes.
// A reference-only match (normalization drift) is safe for the same
// reason: the reference uniquely identifies the artifact.
func findDistEntry(doc []byte, fullName, versionNorm, reference string) *distEntry {
	var parsed struct {
		Packages map[string][]map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return nil
	}

	cur := map[string]json.RawMessage{}
	var byRef *distEntry
	for _, delta := range parsed.Packages[fullName] {
		for k, v := range delta {
			if string(v) == `"__unset"` {
				delete(cur, k)
				continue
			}
			cur[k] = v
		}

		ent := decodeDistEntry(cur)
		if ent == nil {
			continue
		}
		if ent.VersionNormalized == versionNorm && ent.Dist.Reference == reference {
			return ent
		}
		if byRef == nil && reference != "" && ent.Dist.Reference == reference {
			byRef = ent
		}
	}
	return byRef
}

// decodeDistEntry unmarshals the fields distEntry cares about from an
// expanded manifest. Returns nil when the entry is malformed or has
// no version — such entries can't be matched or gated.
func decodeDistEntry(fields map[string]json.RawMessage) *distEntry {
	ent := &distEntry{}
	if raw, has := fields["version"]; has {
		if json.Unmarshal(raw, &ent.Version) != nil {
			return nil
		}
	}
	if raw, has := fields["version_normalized"]; has {
		if json.Unmarshal(raw, &ent.VersionNormalized) != nil {
			return nil
		}
	}
	if raw, has := fields["dist"]; has {
		if json.Unmarshal(raw, &ent.Dist) != nil {
			return nil
		}
	}
	if ent.Version == "" {
		return nil
	}
	return ent
}
