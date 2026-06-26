// Package packagekey derives package metadata (name, version, file kind)
// from cache keys. The cache layer is intentionally ignorant of ecosystem
// formats — these helpers translate adapter-shaped keys back into the
// human-readable identifiers used by stats, search, and security scanning.
package packagekey

import "strings"

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
			if idx := strings.Index(fname, "-"); idx > 0 {
				return fname[:idx]
			}
			return fname
		}
	case "apt":
		if strings.HasSuffix(key, ".deb") {
			parts := strings.Split(key, "/")
			fname := parts[len(parts)-1]
			if idx := strings.Index(fname, "_"); idx > 0 {
				return fname[:idx]
			}
			return fname
		}
		parts := strings.SplitN(strings.TrimPrefix(key, "apt/"), "/", 2)
		if len(parts) > 0 {
			return parts[0]
		}
	case "npm":
		trimmed := strings.TrimPrefix(key, "npm/")
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
		trimmed := strings.TrimPrefix(key, "go/")
		if idx := strings.Index(trimmed, "/@v/"); idx > 0 {
			return trimmed[:idx]
		}
		if idx := strings.Index(trimmed, "/@latest"); idx > 0 {
			return trimmed[:idx]
		}
	case "cargo":
		trimmed := strings.TrimPrefix(key, "cargo/")
		if strings.HasPrefix(trimmed, "crates/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "crates/"), "/", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
		if strings.HasPrefix(trimmed, "index/") {
			parts := strings.Split(trimmed, "/")
			if len(parts) >= 1 {
				return parts[len(parts)-1]
			}
		}
	case "maven":
		parts := strings.Split(strings.TrimPrefix(key, "maven/"), "/")
		if len(parts) >= 3 {
			return parts[len(parts)-3]
		}
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
		trimmed := strings.TrimPrefix(key, "composer/")
		if strings.HasPrefix(trimmed, "p2/") {
			name := strings.TrimPrefix(trimmed, "p2/")
			name = strings.TrimSuffix(name, ".json")
			name = strings.TrimSuffix(name, "~dev")
			return name
		}
		if strings.HasPrefix(trimmed, "dist/") {
			parts := strings.SplitN(strings.TrimPrefix(trimmed, "dist/"), "/", 3)
			if len(parts) >= 2 {
				return parts[0] + "/" + parts[1]
			}
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
		trimmed := strings.TrimPrefix(key, "cran/")
		parts := strings.Split(trimmed, "/")
		fname := parts[len(parts)-1]
		if idx := strings.Index(fname, "_"); idx > 0 {
			return fname[:idx]
		}
		return strings.TrimSuffix(fname, ".tar.gz")
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
			ver := key[idx+4:]
			for _, ext := range []string{".zip", ".mod", ".info"} {
				ver = strings.TrimSuffix(ver, ext)
			}
			return ver
		}
	case "cargo":
		if !strings.HasSuffix(key, ".crate") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		return strings.TrimSuffix(fname, ".crate")
	case "apt":
		if !strings.HasSuffix(key, ".deb") {
			return ""
		}
		parts := strings.Split(key, "/")
		fname := parts[len(parts)-1]
		fname = strings.TrimSuffix(fname, ".deb")
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
		parts := strings.Split(strings.TrimPrefix(key, "nuget/"), "/")
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
	}
	return ""
}
