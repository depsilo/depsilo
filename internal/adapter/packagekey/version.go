package packagekey

import "strings"

// ParseNpmFilename extracts the version from an npm tarball filename
// of shape "<pkg>-<version>.tgz". pkg may itself contain hyphens
// ("@scope/internal-utils-1.0.0.tgz" → pkg="@scope/internal-utils",
// version="1.0.0") so we anchor on the canonical "<pkg>-" prefix
// rather than on hyphen position.
//
// The caller passes the package name it already parsed from the URL
// (e.g. "lodash" or "@scope/internal-utils") so we can identify the
// boundary unambiguously. Returns empty string when the filename
// does not match the expected shape — the caller treats that as
// "skip quarantine check" rather than blocking on an unparseable
// filename, which keeps the helper from being a foot-gun if npm
// ever ships a new file layout.
func ParseNpmFilename(pkg, filename string) string {
	// Tarballs are always <basename>-<version>.tgz on npm. For scoped
	// packages the "basename" is the unscoped part: @scope/name →
	// the tarball uses "name-<version>.tgz".
	base := pkg
	if i := strings.Index(pkg, "/"); i >= 0 {
		base = pkg[i+1:]
	}
	prefix := base + "-"
	if !strings.HasPrefix(filename, prefix) {
		return ""
	}
	rest := filename[len(prefix):]
	if i := strings.LastIndex(rest, ".tgz"); i >= 0 {
		return rest[:i]
	}
	return ""
}

// ParsePypiFilename extracts (package, version) from a PyPI artifact
// filename. Handles both sdist (`<pkg>-<version>.tar.gz`) and wheel
// (`<pkg>-<version>-<build>-<py>-<abi>-<platform>.whl`) shapes.
//
// PyPI normalizes underscores and hyphens in package names to a
// single form on the wire ("simple_index" → "simple-index" or the
// other way around depending on era) but the wheel/sdist filename
// uses the package's PEP 427 / 625-normalized form. We accept either
// shape from the URL and reconstruct the version.
//
// Returns "" / "" when parsing fails.
func ParsePypiFilename(filename string) (pkg, version string) {
	// Wheel: <distribution>-<version>(-<build>)?-<python>-<abi>-<platform>.whl
	if strings.HasSuffix(filename, ".whl") {
		base := strings.TrimSuffix(filename, ".whl")
		parts := strings.Split(base, "-")
		// Need at least <pkg>-<version>-<py>-<abi>-<platform> → 5 parts.
		if len(parts) < 5 {
			return "", ""
		}
		// Distribution may itself contain hyphens? PEP 427 forbids it
		// (must be normalized so distribution is a single token), so
		// parts[0] is the package.
		return parts[0], parts[1]
	}

	// sdist: <pkg>-<version>.tar.gz | .zip | .tar.bz2
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".zip"} {
		if !strings.HasSuffix(filename, ext) {
			continue
		}
		base := strings.TrimSuffix(filename, ext)
		// Version starts with a digit per PEP 440 — find the last "-N"
		// boundary that splits at a digit.
		for i := len(base) - 1; i > 0; i-- {
			if base[i] != '-' {
				continue
			}
			rest := base[i+1:]
			if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
				return base[:i], rest
			}
		}
	}
	return "", ""
}

// ParseCargoCratePath extracts (crate, version) from the cargo
// Sparse-Registry download URL: /api/v1/crates/<crate>/<version>/download
// Returns "", "" when the path doesn't match.
func ParseCargoCratePath(path string) (crate, version string) {
	// Strip any leading /, then split.
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	// Expect ["api", "v1", "crates", "<crate>", "<version>", "download"]
	if len(parts) < 6 {
		return "", ""
	}
	if parts[0] != "api" || parts[1] != "v1" || parts[2] != "crates" || parts[5] != "download" {
		return "", ""
	}
	return parts[3], parts[4]
}

// ParseRubygemsFilename extracts (gem, version) from a RubyGems
// download path of shape `/gems/<gem>-<version>.gem`.
func ParseRubygemsFilename(filename string) (gem, version string) {
	if !strings.HasSuffix(filename, ".gem") {
		return "", ""
	}
	base := strings.TrimSuffix(filename, ".gem")
	for i := len(base) - 1; i > 0; i-- {
		if base[i] != '-' {
			continue
		}
		rest := base[i+1:]
		if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
			return base[:i], rest
		}
	}
	return "", ""
}
