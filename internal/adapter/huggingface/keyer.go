package huggingface

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"sort"
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
	PathUnknown                PathKind = iota
	PathResolve                         // /<repo>/resolve/<ref>/<subpath> — file download (LFS-aware)
	PathRaw                             // /<repo>/raw/<ref>/<subpath> — small file inline content
	PathAPIModelInfo                    // /api/models/<repo>
	PathAPIModelRevision                // /api/models/<repo>/revision/<rev>
	PathAPIModelTree                    // /api/models/<repo>/tree/<rev>
	PathAPIDatasetInfo                  // /api/datasets/<repo>
	PathAPIDatasetRevision              // /api/datasets/<repo>/revision/<rev>
	PathAPIDatasetTree                  // /api/datasets/<repo>/tree/<rev>
	PathAPIModelXetReadToken            // /api/models/<repo>/xet-read-token/<rev>
	PathAPIDatasetXetReadToken          // /api/datasets/<repo>/xet-read-token/<rev>
	PathAPISpaceXetReadToken            // /api/spaces/<repo>/xet-read-token/<rev>
)

// Parsed holds the structured pieces of a HuggingFace request path.
type Parsed struct {
	Kind    PathKind
	Repo    string // "org/name" or "single-token"
	Ref     string // commit SHA, branch, or tag
	Subpath string // path within the repo for resolve/raw/tree kinds

	// Keep the decoded URL-component boundaries internally. Ref names and file
	// names may contain an encoded slash; joining and splitting the public
	// fields would otherwise turn that data into a path separator and create
	// cache-key collisions.
	repoSegments    []string
	subpathSegments []string
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
//	/api/{models,datasets,spaces}/<repo>/xet-read-token/<rev>
//
// Where <repo> is either "owner/name" (two segments) or a single token.
func ParseRequestPath(path string) Parsed {
	p := strings.TrimPrefix(path, "/")
	rawSegments := strings.Split(p, "/")
	segs := make([]string, len(rawSegments))
	for i, segment := range rawSegments {
		decoded, err := url.PathUnescape(segment)
		if err != nil || !validPathComponent(decoded) {
			return Parsed{Kind: PathUnknown}
		}
		segs[i] = decoded
	}

	// /api/{models,datasets,spaces}/...
	if len(segs) >= 3 && segs[0] == "api" {
		return parseAPI(segs[1:])
	}

	// /<repo>/{resolve,raw}/<ref>/<subpath...>
	// Repo can be 1 or 2 segments. Find where "resolve" or "raw" appears.
	maxRepoSegments := 2
	if len(segs) > 0 && segs[0] == "datasets" {
		maxRepoSegments = 3
	}
	if maxRepoSegments >= len(segs) {
		maxRepoSegments = len(segs) - 1
	}
	for i := maxRepoSegments; i >= 1; i-- {
		if i+2 < len(segs) && (segs[i] == "resolve" || segs[i] == "raw") {
			kind := PathResolve
			if segs[i] == "raw" {
				kind = PathRaw
			}
			return Parsed{
				Kind:            kind,
				Repo:            strings.Join(segs[:i], "/"),
				Ref:             segs[i+1],
				Subpath:         strings.Join(segs[i+2:], "/"),
				repoSegments:    append([]string(nil), segs[:i]...),
				subpathSegments: append([]string(nil), segs[i+2:]...),
			}
		}
	}
	return Parsed{Kind: PathUnknown}
}

func validPathComponent(component string) bool {
	return component != "" && component != "." && component != ".."
}

// CacheKeyWithQuery keeps query-bearing metadata pages distinct without
// persisting query values in storage paths, identity columns, or access logs.
// Parameter names, repeated values, and boolean spelling are canonicalized so
// semantically identical queries share one entry. Pagination cursors may still
// appear inside a normalized Link response header because clients require that
// protocol metadata.
func CacheKeyWithQuery(p Parsed, query url.Values) string {
	base := CacheKey(p)
	canonicalQuery := canonicalCacheQuery(p, query)
	canonical := canonicalQuery.Encode()
	if base == "" || canonical == "" {
		return base
	}
	return cacheKeyWithOpaqueQuery(p, base, canonical)
}

func canonicalCacheQuery(p Parsed, query url.Values) url.Values {
	canonical := make(url.Values, len(query))
	for name, values := range query {
		canonical[name] = append([]string(nil), values...)
		switch p.Kind {
		case PathAPIModelInfo, PathAPIModelRevision:
			if name == "blobs" || name == "securityStatus" {
				for index := range canonical[name] {
					canonical[name][index] = strings.ToLower(canonical[name][index])
				}
			}
		case PathAPIDatasetInfo, PathAPIDatasetRevision:
			if name == "blobs" {
				for index := range canonical[name] {
					canonical[name][index] = strings.ToLower(canonical[name][index])
				}
			}
		case PathAPIModelTree, PathAPIDatasetTree:
			if name == "recursive" || name == "expand" {
				for index := range canonical[name] {
					canonical[name][index] = strings.ToLower(canonical[name][index])
				}
			}
		}
		sort.Strings(canonical[name])
	}
	return canonical
}

// CacheKeyForRawQuery parses and canonicalizes an inbound query. Malformed
// queries still receive an opaque, collision-resistant log key but return an
// error so the handler can bypass caching instead of conflating partial parses.
func CacheKeyForRawQuery(p Parsed, rawQuery string) (string, error) {
	base := CacheKey(p)
	if base == "" || rawQuery == "" {
		return base, nil
	}
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return cacheKeyWithOpaqueQuery(p, base, rawQuery), err
	}
	return CacheKeyWithQuery(p, query), nil
}

func cacheKeyWithOpaqueQuery(p Parsed, base, query string) string {
	sum := sha256.Sum256([]byte(base + "\x00" + query))
	kind := "metadata"
	if p.Kind == PathResolve || p.Kind == PathRaw {
		kind = "artifact"
	}
	// This top-level structural namespace cannot be produced by any recognized
	// Hub route: a repository called "__query__" would still require resolve or
	// raw immediately after its one- or two-segment name.
	return "huggingface/__query__/" + kind + "/" +
		hex.EncodeToString(sum[:]) + "/" +
		strings.TrimPrefix(base, "huggingface/")
}

func parseAPI(segs []string) Parsed {
	// segs[0] = "models", "datasets", or "spaces"
	// segs[1] = first repo segment (and possibly only)
	// segs[2] = (optional) second repo segment OR a recognized operation
	// ...
	if len(segs) < 2 {
		return Parsed{Kind: PathUnknown}
	}
	kindBase := segs[0]
	if kindBase != "models" && kindBase != "datasets" && kindBase != "spaces" {
		return Parsed{Kind: PathUnknown}
	}

	// Find an operation at a valid route boundary. Search from the
	// two-segment owner/name boundary first: repository names themselves may
	// legally be named after an operation.
	splitAt := -1
	splitKind := ""
	maxSplit := 3
	if len(segs)-2 < maxSplit {
		maxSplit = len(segs) - 2
	}
	for i := maxSplit; i >= 2; i-- {
		if segs[i] == "revision" || segs[i] == "tree" || segs[i] == "xet-read-token" {
			splitAt = i
			splitKind = segs[i]
			break
		}
	}

	var repoSegments []string
	var ref string
	var subpathSegments []string
	if splitAt == -1 {
		// Hub repository identifiers contain either one segment or owner/name.
		// Longer unqualified paths are not model-info endpoints.
		if len(segs) < 2 || len(segs) > 3 {
			return Parsed{Kind: PathUnknown}
		}
		repoSegments = segs[1:]
	} else {
		repoSegments = segs[1:splitAt]
		if len(repoSegments) < 1 || len(repoSegments) > 2 || splitAt+1 >= len(segs) {
			return Parsed{Kind: PathUnknown}
		}
		ref = segs[splitAt+1]
		switch splitKind {
		case "revision", "xet-read-token":
			if splitAt+2 != len(segs) {
				return Parsed{Kind: PathUnknown}
			}
		case "tree":
			subpathSegments = segs[splitAt+2:]
		}
	}

	out := Parsed{
		Repo:            strings.Join(repoSegments, "/"),
		Ref:             ref,
		Subpath:         strings.Join(subpathSegments, "/"),
		repoSegments:    append([]string(nil), repoSegments...),
		subpathSegments: append([]string(nil), subpathSegments...),
	}
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
	case kindBase == "models" && splitKind == "xet-read-token":
		out.Kind = PathAPIModelXetReadToken
	case kindBase == "datasets" && splitKind == "xet-read-token":
		out.Kind = PathAPIDatasetXetReadToken
	case kindBase == "spaces" && splitKind == "xet-read-token":
		out.Kind = PathAPISpaceXetReadToken
	default:
		out.Kind = PathUnknown
	}
	return out
}

// CacheKey derives the cache.Manager key from a parsed request. The shape
// mirrors the request path with the "huggingface/" prefix.
func CacheKey(p Parsed) string {
	repo, ok := escapedPath(p.Repo, p.repoSegments)
	if !ok {
		return ""
	}
	ref, ok := escapedComponent(p.Ref)
	if p.Ref != "" && !ok {
		return ""
	}
	subpath, ok := escapedPath(p.Subpath, p.subpathSegments)
	if p.Subpath != "" && !ok {
		return ""
	}

	switch p.Kind {
	case PathResolve:
		return "huggingface/" + repo + "/resolve/" + ref + "/" + subpath
	case PathRaw:
		return "huggingface/" + repo + "/raw/" + ref + "/" + subpath
	case PathAPIModelInfo:
		return "huggingface/api/models/" + repo
	case PathAPIModelRevision:
		return "huggingface/api/models/" + repo + "/revision/" + ref
	case PathAPIModelTree:
		return appendSubpath("huggingface/api/models/"+repo+"/tree/"+ref, subpath)
	case PathAPIDatasetInfo:
		return "huggingface/api/datasets/" + repo
	case PathAPIDatasetRevision:
		return "huggingface/api/datasets/" + repo + "/revision/" + ref
	case PathAPIDatasetTree:
		return appendSubpath("huggingface/api/datasets/"+repo+"/tree/"+ref, subpath)
	case PathAPIModelXetReadToken:
		return "huggingface/api/models/" + repo + "/xet-read-token/" + ref
	case PathAPIDatasetXetReadToken:
		return "huggingface/api/datasets/" + repo + "/xet-read-token/" + ref
	case PathAPISpaceXetReadToken:
		return "huggingface/api/spaces/" + repo + "/xet-read-token/" + ref
	default:
		return ""
	}
}

func isXetReadToken(parsed Parsed) bool {
	switch parsed.Kind {
	case PathAPIModelXetReadToken,
		PathAPIDatasetXetReadToken,
		PathAPISpaceXetReadToken:
		return true
	default:
		return false
	}
}

func escapedPath(value string, parsedSegments []string) (string, bool) {
	segments := parsedSegments
	if len(segments) == 0 {
		if value == "" {
			return "", true
		}
		segments = strings.Split(value, "/")
	}
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		var ok bool
		escaped[i], ok = escapedComponent(segment)
		if !ok {
			return "", false
		}
	}
	return strings.Join(escaped, "/"), true
}

func escapedComponent(component string) (string, bool) {
	if !validPathComponent(component) {
		return "", false
	}
	return url.PathEscape(component), true
}

func appendSubpath(base, subpath string) string {
	if subpath == "" {
		return base
	}
	return base + "/" + subpath
}

func mutableRefPinKey(parsed Parsed) (string, bool) {
	if parsed.Kind != PathResolve && parsed.Kind != PathRaw || IsCommitSHA(parsed.Ref) {
		return "", false
	}
	repo, ok := escapedPath(parsed.Repo, parsed.repoSegments)
	if !ok {
		return "", false
	}
	ref, ok := escapedComponent(parsed.Ref)
	if !ok {
		return "", false
	}
	return "huggingface/" + repo + "/ref/" + ref, true
}

func withCommit(parsed Parsed, commit string) (Parsed, bool) {
	if !IsCommitSHA(commit) || (parsed.Kind != PathResolve && parsed.Kind != PathRaw) {
		return Parsed{Kind: PathUnknown}, false
	}
	parsed.Ref = commit
	return parsed, true
}

func requestTarget(parsed Parsed, rawQuery string) (string, bool) {
	key := CacheKey(parsed)
	if key == "" || !strings.HasPrefix(key, "huggingface/") {
		return "", false
	}
	target := "/" + strings.TrimPrefix(key, "huggingface/")
	if rawQuery != "" {
		target += "?" + rawQuery
	}
	return target, true
}
