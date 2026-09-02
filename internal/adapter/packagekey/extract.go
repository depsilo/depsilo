// Package packagekey derives package metadata (name, version, file kind)
// from cache keys. The cache layer is intentionally ignorant of ecosystem
// formats — these helpers translate adapter-shaped keys back into the
// human-readable identifiers used by stats, search, and security scanning.
package packagekey

import (
	"net/url"
	"strings"

	"depsilo/internal/gomoduleidentity"
)

// NPMExactIdentityCachePrefix is a one-way cache namespace revision. Before
// v0.9.1 npm keys case-folded package identities, so reusing the old "npm/"
// namespace could serve an uppercase package's bytes for a lowercase package
// after an in-place upgrade.
const NPMExactIdentityCachePrefix = "npm-exact-v1/"

// ExtractName returns a human-readable package name from a cache key,
// or "" when the key does not denote a recognisable package.
func ExtractName(adapterType, key string) string {
	switch adapterType {
	case "pypi":
		if strings.HasPrefix(key, "pypi/simple/") {
			parts := strings.SplitN(strings.TrimPrefix(key, "pypi/simple/"), "/", 2)
			if len(parts) > 0 {
				return parts[0]
			}
		}
		if strings.HasPrefix(key, "pypi/files/") {
			path := strings.TrimPrefix(key, "pypi/files/")
			parts := strings.Split(path, "/")
			fname := parts[len(parts)-1]
			// Artifact links may use arbitrary filenames. Only the strict
			// PEP 427/625 parser is authoritative enough for cache identity;
			// returning empty prevents background scans from recording a
			// guessed package as clean.
			packageName, _ := ParsePypiFilename(fname)
			return packageName
		}
	case "apt":
		if _, ok := trimDebianPackageExtension(key); ok {
			parts := strings.Split(key, "/")
			fname := parts[len(parts)-1]
			if idx := strings.Index(fname, "_"); idx > 0 {
				return fname[:idx]
			}
			return fname
		}
		// Repository metadata and source-package files do not identify one
		// installable binary package. In particular, the repository selector at
		// the front of the cache key is not a package name.
		return ""
	case "npm":
		trimmed := key
		switch {
		case strings.HasPrefix(key, NPMExactIdentityCachePrefix):
			trimmed = strings.TrimPrefix(key, NPMExactIdentityCachePrefix)
		case strings.HasPrefix(key, "npm/"):
			// Legacy cache rows remain readable to admin/retention tooling, but
			// adapters never generate this ambiguous namespace again.
			trimmed = strings.TrimPrefix(key, "npm/")
		default:
			return ""
		}
		if strings.HasPrefix(trimmed, "@") {
			parts := strings.SplitN(trimmed, "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
		} else {
			parts := strings.SplitN(trimmed, "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
	case "go":
		if !strings.HasPrefix(key, "go/") {
			return ""
		}
		trimmed := strings.TrimPrefix(key, "go/")
		var escaped string
		if idx := strings.Index(trimmed, "/@v/"); idx > 0 {
			escaped = trimmed[:idx]
		} else if idx := strings.Index(trimmed, "/@latest"); idx > 0 {
			escaped = trimmed[:idx]
		}
		if escaped == "" {
			return ""
		}
		modulePath, err := gomoduleidentity.DecodeProxyPath(escaped)
		if err != nil {
			return ""
		}
		return modulePath
	case "cargo":
		const artifactPrefix = "cargo/crates/"
		if strings.HasPrefix(key, artifactPrefix) {
			parts := strings.Split(strings.TrimPrefix(key, artifactPrefix), "/")
			if len(parts) == 2 && parts[0] != "" &&
				strings.HasSuffix(parts[1], ".crate") &&
				strings.TrimSuffix(parts[1], ".crate") != "" {
				return parts[0]
			}
		}
		// Sparse-index filenames are lowercased by the protocol even though the
		// package identity inside the index record is case-sensitive. The cache
		// key alone therefore cannot authenticate a crate name for OSV scanning.
		return ""
	case "maven":
		if !strings.HasPrefix(key, "maven/") {
			return ""
		}
		coordinate, _ := ParseMavenPath(strings.TrimPrefix(key, "maven/"))
		return coordinate
	case "rubygems":
		trimmed := strings.TrimPrefix(key, "rubygems/")
		if strings.HasPrefix(trimmed, "gems/") {
			fname := strings.TrimPrefix(trimmed, "gems/")
			if idx := strings.LastIndex(fname, "-"); idx > 0 {
				return fname[:idx]
			}
			return strings.TrimSuffix(fname, ".gem")
		}
		if strings.HasPrefix(trimmed, "info/") {
			return strings.TrimPrefix(trimmed, "info/")
		}
	case "composer":
		if !strings.HasPrefix(key, "composer/") {
			return ""
		}
		if name, ok := ParseComposerPath(strings.TrimPrefix(key, "composer/")); ok {
			return name
		}
	case "conda":
		trimmed := strings.TrimPrefix(key, "conda/")
		parts := strings.Split(trimmed, "/")
		fname := parts[len(parts)-1]
		if strings.HasSuffix(fname, ".tar.bz2") {
			fname = strings.TrimSuffix(fname, ".tar.bz2")
		} else if strings.HasSuffix(fname, ".conda") {
			fname = strings.TrimSuffix(fname, ".conda")
		} else {
			return fname
		}
		if idx := strings.Index(fname, "-"); idx > 0 {
			return fname[:idx]
		}
		return fname
	case "cran":
		if !strings.HasPrefix(key, "cran/") {
			return ""
		}
		packageName, _ := ParseCranPath(strings.TrimPrefix(key, "cran/"))
		return packageName
	case "alpine":
		// e.g. alpine/v3.19/main/x86_64/py3-foo-1.2.3-r0.apk -> py3-foo
		if !strings.HasSuffix(key, ".apk") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := strings.TrimSuffix(parts[len(parts)-1], ".apk")
		// name may contain dashes; the version segment starts at the first
		// '-' that is followed by a digit.
		for i := 0; i < len(fname)-1; i++ {
			if fname[i] == '-' && fname[i+1] >= '0' && fname[i+1] <= '9' {
				return fname[:i]
			}
		}
		return fname
	case "nuget":
		trimmed := strings.TrimPrefix(key, "nuget/")
		if id, _ := ParseNugetPath(trimmed); id != "" {
			return id
		}
		if strings.HasPrefix(trimmed, "v3/package/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "v3/package/"), "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
		if strings.HasPrefix(trimmed, "v3/registration/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "v3/registration/"), "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
	case "helm":
		trimmed := strings.TrimPrefix(key, "helm/")
		if strings.HasSuffix(trimmed, ".tgz") {
			fname := trimmed
			if idx := strings.LastIndex(fname, "/"); idx >= 0 {
				fname = fname[idx+1:]
			}
			fname = strings.TrimSuffix(fname, ".tgz")
			for i := len(fname) - 1; i >= 0; i-- {
				if fname[i] == '-' && i+1 < len(fname) && fname[i+1] >= '0' && fname[i+1] <= '9' {
					return fname[:i]
				}
			}
			return fname
		}
	case "docker":
		if strings.Contains(key, "/manifests/") {
			parts := strings.SplitN(key, "/manifests/", 2)
			if len(parts) == 2 {
				image := parts[1]
				if idx := strings.LastIndex(image, "/"); idx > 0 {
					return image[:idx]
				}
				return image
			}
		}
		if strings.Contains(key, "/tags/") {
			parts := strings.SplitN(key, "/tags/", 2)
			if len(parts) == 2 {
				return strings.TrimSuffix(parts[1], "/list")
			}
		}
		if strings.Contains(key, "/blobs/") {
			return ""
		}
	case "huggingface":
		repo, _, ok := parseHuggingFaceFileKey(key)
		if ok {
			return repo
		}
		repo, ok = parseHuggingFaceMetadataKey(key)
		if ok {
			return repo
		}
	}
	return ""
}

// ExtractVersion derives a version string from the cache key,
// or "" when no version is encoded.
func ExtractVersion(adapterType, key string) string {
	switch adapterType {
	case "pypi":
		if !strings.HasPrefix(key, "pypi/files/") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		for _, ext := range []string{".whl", ".tar.gz", ".zip", ".egg"} {
			fname = strings.TrimSuffix(fname, ext)
		}
		dashParts := strings.SplitN(fname, "-", 3)
		if len(dashParts) >= 2 {
			return dashParts[1]
		}
	case "npm":
		if !strings.HasSuffix(key, ".tgz") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".tgz")
		if idx := strings.LastIndex(fname, "-"); idx > 0 {
			return fname[idx+1:]
		}
	case "go":
		if idx := strings.Index(key, "/@v/"); idx > 0 {
			escapedVersion := key[idx+4:]
			for _, ext := range []string{".zip", ".mod", ".info"} {
				escapedVersion = strings.TrimSuffix(escapedVersion, ext)
			}
			version, err := gomoduleidentity.DecodeProxyVersion(escapedVersion)
			if err != nil {
				return ""
			}
			return version
		}
	case "cargo":
		if !strings.HasSuffix(key, ".crate") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		return strings.TrimSuffix(fname, ".crate")
	case "apt":
		trimmed, ok := trimDebianPackageExtension(key)
		if !ok {
			return ""
		}
		fname := strings.TrimSuffix(trimmed, "/")
		if slash := strings.LastIndexByte(fname, '/'); slash >= 0 {
			fname = fname[slash+1:]
		}
		underParts := strings.SplitN(fname, "_", 3)
		if len(underParts) >= 2 {
			return underParts[1]
		}
	case "maven":
		parts := strings.Split(strings.TrimPrefix(key, "maven/"), "/")
		if len(parts) >= 3 {
			return parts[len(parts)-2]
		}
	case "rubygems":
		if !strings.HasSuffix(key, ".gem") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".gem")
		if idx := strings.LastIndex(fname, "-"); idx > 0 {
			return fname[idx+1:]
		}
	case "alpine":
		// e.g. alpine/v3.19/main/x86_64/bash-5.2.21-r0.apk -> 5.2.21-r0
		if !strings.HasSuffix(key, ".apk") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := strings.TrimSuffix(parts[len(parts)-1], ".apk")
		for i := 0; i < len(fname)-1; i++ {
			if fname[i] == '-' && fname[i+1] >= '0' && fname[i+1] <= '9' {
				return fname[i+1:]
			}
		}
	case "nuget":
		trimmed := strings.TrimPrefix(key, "nuget/")
		if _, version := ParseNugetPath(trimmed); version != "" {
			return version
		}
		parts := strings.Split(trimmed, "/")
		if len(parts) >= 3 && strings.HasPrefix(parts[0], "v3/package") {
			return parts[2]
		}
		for i, p := range parts {
			if p == "package" && i+2 < len(parts) {
				return parts[i+2]
			}
		}
	case "conda":
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		for _, ext := range []string{".tar.bz2", ".conda"} {
			fname = strings.TrimSuffix(fname, ext)
		}
		dashParts := strings.SplitN(fname, "-", 3)
		if len(dashParts) >= 2 {
			return dashParts[1]
		}
	case "cran":
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".tar.gz")
		fname = strings.TrimSuffix(fname, ".zip")
		fname = strings.TrimSuffix(fname, ".tgz")
		if idx := strings.Index(fname, "_"); idx > 0 {
			return fname[idx+1:]
		}
	case "helm":
		if !strings.HasSuffix(key, ".tgz") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".tgz")
		for i := len(fname) - 1; i >= 0; i-- {
			if fname[i] == '-' && i+1 < len(fname) && fname[i+1] >= '0' && fname[i+1] <= '9' {
				return fname[i+1:]
			}
		}
	case "docker":
		if strings.Contains(key, "/manifests/") {
			parts := strings.SplitN(key, "/manifests/", 2)
			if len(parts) == 2 {
				if idx := strings.LastIndex(parts[1], "/"); idx > 0 {
					return parts[1][idx+1:]
				}
			}
		}
	case "huggingface":
		_, ref, ok := parseHuggingFaceFileKey(key)
		if ok {
			return ref
		}
	}
	return ""
}

func trimDebianPackageExtension(value string) (string, bool) {
	for _, extension := range []string{".udeb", ".deb"} {
		if strings.HasSuffix(value, extension) {
			return strings.TrimSuffix(value, extension), true
		}
	}
	return value, false
}

func parseHuggingFaceFileKey(key string) (repo, ref string, ok bool) {
	const prefix = "huggingface/"
	if !strings.HasPrefix(key, prefix) {
		return "", "", false
	}

	rest := strings.TrimPrefix(key, prefix)
	if strings.HasPrefix(rest, "__query__/") {
		// Query-bearing representations live in an unreachable structural
		// namespace: __query__/{kind}/{hash}/{original-key-without-prefix}.
		queryParts := strings.Split(rest, "/")
		if len(queryParts) >= 4 &&
			(queryParts[1] == "artifact" || queryParts[1] == "metadata") &&
			isLowerHexSHA256(queryParts[2]) {
			if queryParts[1] != "artifact" {
				return "", "", false
			}
			rest = strings.Join(queryParts[3:], "/")
		}
	}

	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "api" {
		return "", "", false
	}

	maxRepoSegments := 2
	if parts[0] == "datasets" {
		maxRepoSegments = 3
	}
	if maxRepoSegments >= len(parts) {
		maxRepoSegments = len(parts) - 1
	}
	for i := maxRepoSegments; i >= 1; i-- {
		if parts[i] != "resolve" && parts[i] != "raw" {
			continue
		}
		if i+2 >= len(parts) || parts[i+1] == "" ||
			strings.Join(parts[i+2:], "/") == "" {
			return "", "", false
		}
		repoParts := make([]string, i)
		for index, part := range parts[:i] {
			decoded, err := url.PathUnescape(part)
			if err != nil || decoded == "" || decoded == "." || decoded == ".." {
				return "", "", false
			}
			repoParts[index] = decoded
		}
		decodedRef, err := url.PathUnescape(parts[i+1])
		if err != nil || decodedRef == "" || decodedRef == "." || decodedRef == ".." {
			return "", "", false
		}
		repo = strings.Join(repoParts, "/")
		if repo == "" {
			return "", "", false
		}
		return repo, decodedRef, true
	}

	return "", "", false
}

func parseHuggingFaceMetadataKey(key string) (repo string, ok bool) {
	const prefix = "huggingface/"
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}

	rest := strings.TrimPrefix(key, prefix)
	if strings.HasPrefix(rest, "__query__/") {
		// Query-bearing metadata retains the original structural key after the
		// opaque hash, so package-level operations can still find every
		// representation without retaining any query values.
		queryParts := strings.Split(rest, "/")
		if len(queryParts) < 4 ||
			queryParts[1] != "metadata" ||
			!isLowerHexSHA256(queryParts[2]) {
			return "", false
		}
		rest = strings.Join(queryParts[3:], "/")
	}

	parts := strings.Split(rest, "/")
	if len(parts) < 3 || parts[0] != "api" ||
		(parts[1] != "models" && parts[1] != "datasets") {
		return "", false
	}

	route := parts[2:]
	decoded := make([]string, len(route))
	for index, part := range route {
		component, err := url.PathUnescape(part)
		if err != nil || component == "" || component == "." || component == ".." {
			return "", false
		}
		decoded[index] = component
	}

	// Hub repository identifiers contain one component or owner/name. Prefer
	// the two-component boundary because a repository itself may legally be
	// named "tree" or "revision".
	splitAt := -1
	maxSplit := 2
	if len(decoded)-2 < maxSplit {
		maxSplit = len(decoded) - 2
	}
	for index := maxSplit; index >= 1; index-- {
		if decoded[index] == "tree" || decoded[index] == "revision" {
			splitAt = index
			break
		}
	}

	var repoParts []string
	if splitAt < 0 {
		if len(decoded) < 1 || len(decoded) > 2 {
			return "", false
		}
		repoParts = decoded
	} else {
		repoParts = decoded[:splitAt]
		if len(repoParts) < 1 || len(repoParts) > 2 || splitAt+1 >= len(decoded) {
			return "", false
		}
		if decoded[splitAt] == "revision" && splitAt+2 != len(decoded) {
			return "", false
		}
	}

	repo = strings.Join(repoParts, "/")
	if parts[1] == "datasets" {
		repo = "datasets/" + repo
	}
	return repo, repo != ""
}

func isLowerHexSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		if (value[i] < '0' || value[i] > '9') && (value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return true
}
