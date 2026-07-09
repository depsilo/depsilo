package packagekey

import (
	"strings"
	"unicode"
)

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

// ParseMavenPath extracts (coord, version) from a Maven Central path
// `/g1/g2/.../artifact/version/artifact-version.jar` where coord is
// the canonical "group:artifact" Maven coordinate the resolver uses.
// Returns "", "" for paths that aren't jar/pom artifacts (metadata
// XML, maven-metadata.xml, snapshot manifests, etc.).
func ParseMavenPath(path string) (coord, version string) {
	p := strings.TrimPrefix(path, "/")
	if !(strings.HasSuffix(p, ".jar") || strings.HasSuffix(p, ".pom")) {
		return "", ""
	}
	parts := strings.Split(p, "/")
	// Need at least <g>/<a>/<v>/<file>.
	if len(parts) < 4 {
		return "", ""
	}
	filename := parts[len(parts)-1]
	version = parts[len(parts)-2]
	artifact := parts[len(parts)-3]
	// Filename shape: <artifact>-<version>[-classifier].jar — sanity-
	// check that the version we plucked from the path appears in the
	// filename, otherwise we're dealing with a maven-metadata or some
	// other auxiliary file we shouldn't gate on.
	if !strings.Contains(filename, version) {
		return "", ""
	}
	group := strings.Join(parts[:len(parts)-3], ".")
	if group == "" {
		return "", ""
	}
	return group + ":" + artifact, version
}

// ParseNugetPath extracts (id, version) from
// /v3-flatcontainer/<id>/<version>/<id>.<version>.nupkg — the canonical
// NuGet download shape. NuGet ids are lowercased on the wire; we
// preserve whatever case is in the URL because the resolver itself
// lowercases when needed.
func ParseNugetPath(path string) (id, version string) {
	p := strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(p, "v3-flatcontainer/") {
		return "", ""
	}
	parts := strings.Split(p, "/")
	if len(parts) < 4 {
		return "", ""
	}
	if !strings.HasSuffix(parts[3], ".nupkg") {
		return "", ""
	}
	return parts[1], parts[2]
}

// ParseCondaPath extracts (channel/name, version) from
// /<channel>/<arch>/<name>-<version>-<build>.conda or .tar.bz2.
// The conda resolver uses "<channel>/<name>" as its package key so
// the conda-forge channel for "numpy" is "conda-forge/numpy".
func ParseCondaPath(path string) (pkg, version string) {
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	if len(parts) < 3 {
		return "", ""
	}
	channel := parts[0]
	// arch := parts[1]
	filename := parts[len(parts)-1]
	var base string
	switch {
	case strings.HasSuffix(filename, ".conda"):
		base = strings.TrimSuffix(filename, ".conda")
	case strings.HasSuffix(filename, ".tar.bz2"):
		base = strings.TrimSuffix(filename, ".tar.bz2")
	default:
		return "", ""
	}
	// Conda filenames: <name>-<version>-<build>. We split right-to-left
	// on the last two hyphens; the right one separates the build, the
	// next separates the version, and the rest is the name.
	last := strings.LastIndex(base, "-")
	if last <= 0 {
		return "", ""
	}
	mid := strings.LastIndex(base[:last], "-")
	if mid <= 0 {
		return "", ""
	}
	return channel + "/" + base[:mid], base[mid+1 : last]
}

// ParseCranPath extracts (pkg, version) from a CRAN download path.
// Two shapes:
//
//	/src/contrib/<pkg>_<ver>.tar.gz                 — current
//	/src/contrib/Archive/<pkg>/<pkg>_<ver>.tar.gz   — archived
//
// Underscore is the CRAN convention separating package and version,
// distinct from PyPI's hyphen.
func ParseCranPath(path string) (pkg, version string) {
	p := strings.TrimPrefix(path, "/")
	filename := p[strings.LastIndex(p, "/")+1:]
	if !strings.HasSuffix(filename, ".tar.gz") {
		return "", ""
	}
	base := strings.TrimSuffix(filename, ".tar.gz")
	u := strings.Index(base, "_")
	if u <= 0 || u == len(base)-1 {
		return "", ""
	}
	return base[:u], base[u+1:]
}

// ParseHelmPath extracts (chart, version) from a Helm chart download
// path like /<chart>-<version>.tgz. Helm packages everything in a
// flat namespace per repo, so we just need the chart name + version.
func ParseHelmPath(path string) (chart, version string) {
	p := strings.TrimPrefix(path, "/")
	filename := p[strings.LastIndex(p, "/")+1:]
	if !strings.HasSuffix(filename, ".tgz") {
		return "", ""
	}
	base := strings.TrimSuffix(filename, ".tgz")
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

// ParseAlpinePath extracts ("<branch>/<repo>/<arch>/<name>", version)
// from /<branch>/<repo>/<arch>/<name>-<version>.apk. The combined-key
// form is what the alpine resolver uses to construct the artifact URL.
func ParseAlpinePath(path string) (pkg, version string) {
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	if len(parts) < 4 {
		return "", ""
	}
	branch, repo, arch := parts[0], parts[1], parts[2]
	filename := parts[3]
	if !strings.HasSuffix(filename, ".apk") {
		return "", ""
	}
	base := strings.TrimSuffix(filename, ".apk")
	// APK filenames: <name>-<version>-r<rev>. The version itself can
	// contain hyphens-with-digits — we anchor on the LAST "-rN"
	// (revision) and use everything before as version.
	lastDash := strings.LastIndex(base, "-")
	if lastDash <= 0 {
		return "", ""
	}
	rev := base[lastDash+1:]
	if !strings.HasPrefix(rev, "r") || len(rev) < 2 {
		return "", ""
	}
	beforeRev := base[:lastDash]
	verDash := strings.LastIndex(beforeRev, "-")
	if verDash <= 0 {
		return "", ""
	}
	name := beforeRev[:verDash]
	ver := beforeRev[verDash+1:] + "-" + rev
	return branch + "/" + repo + "/" + arch + "/" + name, ver
}

// ParseDockerPath extracts (image, tag) from /v2/<image>/manifests/<tag>.
// image may contain slashes ("library/alpine" or "owner/myimage").
// Returns "", "" for blob requests or other v2 endpoints.
func ParseDockerPath(path string) (image, tag string) {
	p := strings.TrimPrefix(path, "/")
	if !strings.HasPrefix(p, "v2/") {
		return "", ""
	}
	const sep = "/manifests/"
	idx := strings.Index(p, sep)
	if idx < 0 {
		return "", ""
	}
	image = p[len("v2/"):idx]
	tag = p[idx+len(sep):]
	if image == "" || tag == "" {
		return "", ""
	}
	// Strip everything after the tag (a digest reference in /manifests/
	// would arrive as "sha256:..." which is fine to pass through; a
	// trailing slash or query strings shouldn't reach here).
	return image, tag
}

// ParseGoZipPath extracts (module, version) from a GOPROXY zip
// download path like "github.com/user/repo/@v/v1.2.3.zip". The module
// portion is decoded from the proxy's escaped form ("!a" → 'A', per
// golang.org/x/mod/module) so it matches advisory/registry spellings.
// Only .zip paths parse — .info/.mod are metadata, not artifacts.
func ParseGoZipPath(path string) (module, version string) {
	i := strings.Index(path, "/@v/")
	if i <= 0 || !strings.HasSuffix(path, ".zip") {
		return "", ""
	}
	version = strings.TrimSuffix(path[i+len("/@v/"):], ".zip")
	module = unescapeGoPath(path[:i])
	if module == "" || version == "" || strings.Contains(version, "/") {
		return "", ""
	}
	return module, version
}

// unescapeGoPath reverses the GOPROXY "!lower" escaping for uppercase
// letters ("github.com/!azure/x" → "github.com/Azure/x").
func unescapeGoPath(p string) string {
	if !strings.Contains(p, "!") {
		return p
	}
	var b strings.Builder
	b.Grow(len(p))
	bang := false
	for _, r := range p {
		if bang {
			b.WriteRune(unicode.ToUpper(r))
			bang = false
			continue
		}
		if r == '!' {
			bang = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
