package huggingface

import (
	"regexp"
	"strings"
	"time"
)

// commitSHAPattern matches a canonical Git commit SHA: exactly 40
// lowercase hex characters. HuggingFace clients use these directly as
// refs for immutable downloads.
var commitSHAPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// IsCommitSHA reports whether `ref` is a 40-character lowercase hex string
// that we can treat as immutable (long-TTL cache eligible).
func IsCommitSHA(ref string) bool {
	return commitSHAPattern.MatchString(ref)
}

// TTLForRef returns the cache TTL appropriate for a given ref. Commit SHAs
// are immutable (72h); branch/tag/everything else is mutable (5m) and
// participates in stale-while-revalidate via cache.Manager.
func TTLForRef(ref string) time.Duration {
	if IsCommitSHA(ref) {
		return 72 * time.Hour
	}
	return 5 * time.Minute
}

// PathKind tags the kind of HuggingFace URL a request maps to. The
// resolver/handler dispatches on this.
type PathKind int

const (
	PathUnknown            PathKind = iota
	PathResolve                     // /<repo>/resolve/<ref>/<subpath> — file download (LFS-aware)
	PathRaw                         // /<repo>/raw/<ref>/<subpath> — small file inline content
	PathAPIModelInfo                // /api/models/<repo>
	PathAPIModelRevision            // /api/models/<repo>/revision/<rev>
	PathAPIModelTree                // /api/models/<repo>/tree/<rev>
	PathAPIDatasetInfo              // /api/datasets/<repo>
	PathAPIDatasetRevision          // /api/datasets/<repo>/revision/<rev>
	PathAPIDatasetTree              // /api/datasets/<repo>/tree/<rev>
)

// Parsed holds the structured pieces of a HuggingFace request path.
type Parsed struct {
	Kind    PathKind
	Repo    string // "org/name" or "single-token"
	Ref     string // commit SHA, branch, or tag
	Subpath string // path within the repo for resolve/raw kinds
}

// ParseRequestPath splits a request path under /huggingface/ into its
// components. The leading slash is optional. Returns PathUnknown when no
// recognized pattern matches.
//
// Supported patterns (see spec §3.1):
//
//	/<repo>/resolve/<ref>/<subpath...>
//	/<repo>/raw/<ref>/<subpath...>
//	/api/models/<repo>
//	/api/models/<repo>/revision/<rev>
//	/api/models/<repo>/tree/<rev>
//	/api/datasets/<repo>[/revision|/tree]/<rev>
//
// Where <repo> is either "owner/name" (two segments) or a single token.
func ParseRequestPath(path string) Parsed {
	p := strings.TrimPrefix(path, "/")
	segs := strings.Split(p, "/")

	// /api/{models,datasets}/...
	if len(segs) >= 3 && segs[0] == "api" {
		return parseAPI(segs[1:])
	}

	// /<repo>/{resolve,raw}/<ref>/<subpath...>
	// Repo can be 1 or 2 segments. Find where "resolve" or "raw" appears.
	for i := 1; i <= 2 && i < len(segs); i++ {
		if i+2 < len(segs) && (segs[i] == "resolve" || segs[i] == "raw") {
			kind := PathResolve
			if segs[i] == "raw" {
				kind = PathRaw
			}
			return Parsed{
				Kind:    kind,
				Repo:    strings.Join(segs[:i], "/"),
				Ref:     segs[i+1],
				Subpath: strings.Join(segs[i+2:], "/"),
			}
		}
	}
	return Parsed{Kind: PathUnknown}
}

func parseAPI(segs []string) Parsed {
	// segs[0] = "models" or "datasets"
	// segs[1] = first repo segment (and possibly only)
	// segs[2] = (optional) second repo segment OR "revision" OR "tree"
	// ...
	if len(segs) < 2 {
		return Parsed{Kind: PathUnknown}
	}
	kindBase := segs[0] // "models" or "datasets"
	if kindBase != "models" && kindBase != "datasets" {
		return Parsed{Kind: PathUnknown}
	}

	// Find "revision" or "tree" if present
	splitAt := -1
	splitKind := ""
	for i := 2; i < len(segs); i++ {
		if segs[i] == "revision" || segs[i] == "tree" {
			splitAt = i
			splitKind = segs[i]
			break
		}
	}

	var repo string
	var ref string
	if splitAt == -1 {
		repo = strings.Join(segs[1:], "/")
	} else {
		repo = strings.Join(segs[1:splitAt], "/")
		if splitAt+1 < len(segs) {
			ref = segs[splitAt+1]
		}
	}

	out := Parsed{Repo: repo, Ref: ref}
	switch {
	case kindBase == "models" && splitKind == "":
		out.Kind = PathAPIModelInfo
	case kindBase == "models" && splitKind == "revision":
		out.Kind = PathAPIModelRevision
	case kindBase == "models" && splitKind == "tree":
		out.Kind = PathAPIModelTree
	case kindBase == "datasets" && splitKind == "":
		out.Kind = PathAPIDatasetInfo
	case kindBase == "datasets" && splitKind == "revision":
		out.Kind = PathAPIDatasetRevision
	case kindBase == "datasets" && splitKind == "tree":
		out.Kind = PathAPIDatasetTree
	default:
		out.Kind = PathUnknown
	}
	return out
}

// CacheKey derives the cache.Manager key from a parsed request. The shape
// mirrors the request path with the "huggingface/" prefix.
func CacheKey(p Parsed) string {
	switch p.Kind {
	case PathResolve:
		return "huggingface/" + p.Repo + "/resolve/" + p.Ref + "/" + p.Subpath
	case PathRaw:
		return "huggingface/" + p.Repo + "/raw/" + p.Ref + "/" + p.Subpath
	case PathAPIModelInfo:
		return "huggingface/api/models/" + p.Repo
	case PathAPIModelRevision:
		return "huggingface/api/models/" + p.Repo + "/revision/" + p.Ref
	case PathAPIModelTree:
		return "huggingface/api/models/" + p.Repo + "/tree/" + p.Ref
	case PathAPIDatasetInfo:
		return "huggingface/api/datasets/" + p.Repo
	case PathAPIDatasetRevision:
		return "huggingface/api/datasets/" + p.Repo + "/revision/" + p.Ref
	case PathAPIDatasetTree:
		return "huggingface/api/datasets/" + p.Repo + "/tree/" + p.Ref
	default:
		return ""
	}
}
