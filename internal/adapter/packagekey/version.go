package packagekey

import (
	"strings"

	"depsilo/internal/gomoduleidentity"
)

// ParsePypiFilename extracts (package, version) only from PyPI artifact
// filenames whose identity fields are unambiguous: PEP 427 wheels and strict
// PEP 625 `<normalized-name>-<normalized-version>.tar.gz` sdists.
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
		// Distribution and version are normalized to single tokens. A wheel has
		// exactly five fields, or six when the optional build tag is present.
		if len(parts) != 5 && len(parts) != 6 {
			return "", ""
		}
		for _, part := range parts {
			if part == "" {
				return "", ""
			}
		}
		if parts[1][0] < '0' || parts[1][0] > '9' {
			return "", ""
		}
		if len(parts) == 6 && (parts[2][0] < '0' || parts[2][0] > '9') {
			return "", ""
		}
		return parts[0], parts[1]
	}

	// PEP 625's normalized distribution and version do not contain hyphens,
	// making the one separator below authoritative. Legacy sdists and other
	// archive formats require index provenance and deliberately return empty.
	if strings.HasSuffix(filename, ".tar.gz") {
		base := strings.TrimSuffix(filename, ".tar.gz")
		if strings.Count(base, "-") != 1 {
			return "", ""
		}
		pkg, version, _ = strings.Cut(base, "-")
		if pkg == "" || version == "" {
			return "", ""
		}
		return pkg, version
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
// The extension is deliberately not enumerated: Maven packaging types may use
// arbitrary extensions. Metadata and malformed layout paths return "", "".
func ParseMavenPath(path string) (coord, version string) {
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	// Need at least <g>/<a>/<v>/<file>.
	if len(parts) < 4 {
		return "", ""
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ""
		}
	}
	filename := parts[len(parts)-1]
	version = parts[len(parts)-2]
	artifact := parts[len(parts)-3]
	if filename == "maven-metadata.xml" || strings.HasSuffix(filename, ".lastUpdated") ||
		filename == "_remote.repositories" {
		return "", ""
	}
	prefix := artifact + "-"
	if artifact == "" || version == "" || !strings.HasPrefix(filename, prefix) {
		return "", ""
	}
	artifactSuffix := strings.TrimPrefix(filename, prefix)
	if !mavenArtifactSuffixMatchesVersion(artifactSuffix, version) {
		return "", ""
	}
	group := strings.Join(parts[:len(parts)-3], ".")
	if group == "" {
		return "", ""
	}
	return group + ":" + artifact, version
}

func mavenArtifactSuffixMatchesVersion(suffix, baseVersion string) bool {
	if suffix == "" || baseVersion == "" {
		return false
	}

	// The directory version is authoritative.  Once it has been removed from
	// the filename, only the artifact extension or a classifier followed by an
	// extension may remain.  In particular, accepting any separator here would
	// make `.../1.0/app-1.0.0.jar` look like a 1.0 artifact (the `.0` is a
	// version continuation, not an extension).
	if strings.HasPrefix(suffix, baseVersion) {
		return mavenArtifactRemainderIsValid(strings.TrimPrefix(suffix, baseVersion))
	}

	// Unique Maven snapshots replace the literal SNAPSHOT token in the
	// filename with yyyyMMdd.HHmmss-buildNumber while retaining the directory
	// baseVersion (for example, 1.0-SNAPSHOT → 1.0-20260901.010203-7.jar).
	if !strings.HasSuffix(baseVersion, "-SNAPSHOT") {
		return false
	}
	base := strings.TrimSuffix(baseVersion, "SNAPSHOT")
	if !strings.HasPrefix(suffix, base) {
		return false
	}
	unique := strings.TrimPrefix(suffix, base)
	if len(unique) < len("20060102.150405-1.x") {
		return false
	}
	for index := 0; index < 8; index++ {
		if !mavenASCIIDigit(unique[index]) {
			return false
		}
	}
	if unique[8] != '.' {
		return false
	}
	for index := 9; index < 15; index++ {
		if !mavenASCIIDigit(unique[index]) {
			return false
		}
	}
	if unique[15] != '-' {
		return false
	}
	index := 16
	for index < len(unique) && mavenASCIIDigit(unique[index]) {
		index++
	}
	if index == 16 || index == len(unique) {
		return false
	}
	return mavenArtifactRemainderIsValid(unique[index:])
}

// mavenArtifactRemainderIsValid validates the part after an exact directory
// version. Maven's repository layout permits either `.extension` or
// `-classifier.extension`; extension chains such as `.tar.gz` and sidecar
// suffixes such as `.jar.sha256` are retained. A leading digit (or a known
// version qualifier) in the first extension/classifier token is rejected so a
// longer version cannot be accepted merely because it has the directory
// version as a prefix.
func mavenArtifactRemainderIsValid(remainder string) bool {
	if len(remainder) < 2 || (remainder[0] != '.' && remainder[0] != '-') {
		return false
	}

	if remainder[0] == '.' {
		extension := remainder[1:]
		parts := strings.Split(extension, ".")
		if len(parts) == 0 || mavenVersionLikeMavenToken(parts[0]) {
			return false
		}
		for _, part := range parts {
			if !mavenArtifactTokenIsValid(part) {
				return false
			}
		}
		return true
	}

	// Use the final dot as the extension boundary. This keeps classifiers such
	// as `linux.x86_64` valid while still requiring a non-empty extension.
	extensionIndex := strings.LastIndexByte(remainder, '.')
	if extensionIndex <= 1 || extensionIndex == len(remainder)-1 {
		return false
	}
	classifier := remainder[1:extensionIndex]
	extension := remainder[extensionIndex+1:]
	if mavenVersionLikeMavenToken(classifier) || !mavenClassifierIsValid(classifier) {
		return false
	}
	return mavenArtifactTokenIsValid(extension)
}

func mavenClassifierIsValid(classifier string) bool {
	if classifier == "" {
		return false
	}
	for _, part := range strings.Split(classifier, ".") {
		if !mavenArtifactTokenIsValid(part) {
			return false
		}
	}
	return true
}

func mavenArtifactTokenIsValid(token string) bool {
	if token == "" {
		return false
	}
	for index := 0; index < len(token); index++ {
		character := token[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' || character == '+' {
			continue
		}
		return false
	}
	return true
}

func mavenASCIIDigit(character byte) bool {
	return character >= '0' && character <= '9'
}

func mavenVersionLikeMavenToken(token string) bool {
	if token == "" || mavenASCIIDigit(token[0]) {
		return true
	}
	lower := strings.ToLower(token)
	for _, qualifier := range []string{
		"alpha", "a", "beta", "b", "milestone", "m", "rc", "cr",
		"snapshot", "final", "ga", "release", "sp",
	} {
		if lower == qualifier {
			return true
		}
		if strings.HasPrefix(lower, qualifier) {
			suffix := lower[len(qualifier):]
			if suffix != "" {
				allDigits := true
				for index := 0; index < len(suffix); index++ {
					if !mavenASCIIDigit(suffix[index]) {
						allDigits = false
						break
					}
				}
				if allDigits {
					return true
				}
			}
		}
	}
	return false
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
	if len(parts) != 4 {
		return "", ""
	}
	for _, part := range parts {
		if part == "" {
			return "", ""
		}
	}
	wantFilename := parts[1] + "." + parts[2] + ".nupkg"
	if !strings.EqualFold(parts[3], wantFilename) {
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
	channelParts := parts[:len(parts)-2]
	for _, part := range channelParts {
		if part == "" {
			return "", ""
		}
	}
	channel := strings.Join(channelParts, "/")
	if channel == "" || parts[len(parts)-2] == "" {
		return "", ""
	}
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
// Recognized shapes:
//
//	/src/contrib/<pkg>_<ver>.tar.gz                   — source
//	/src/contrib/Archive/<pkg>/<pkg>_<ver>.tar.gz     — archived source
//	/bin/windows/contrib/<r>/<pkg>_<ver>.zip          — Windows binary
//	/bin/macosx/.../contrib/<r>/<pkg>_<ver>.tgz       — macOS binary
//
// Underscore is the CRAN convention separating package and version,
// distinct from PyPI's hyphen.
func ParseCranPath(path string) (pkg, version string) {
	p := strings.TrimPrefix(path, "/")
	parts := strings.Split(p, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", ""
		}
	}

	extension := ""
	archivePackage := ""
	switch {
	case len(parts) == 3 && parts[0] == "src" && parts[1] == "contrib":
		extension = ".tar.gz"
	case len(parts) == 5 && parts[0] == "src" && parts[1] == "contrib" && parts[2] == "Archive":
		extension = ".tar.gz"
		archivePackage = parts[3]
	case len(parts) == 5 && parts[0] == "bin" && parts[1] == "windows" && parts[2] == "contrib":
		extension = ".zip"
	case len(parts) >= 5 && parts[0] == "bin" && parts[1] == "macosx" && parts[len(parts)-3] == "contrib":
		extension = ".tgz"
	default:
		return "", ""
	}

	filename := parts[len(parts)-1]
	if !strings.HasSuffix(filename, extension) {
		return "", ""
	}
	base := strings.TrimSuffix(filename, extension)
	if strings.Count(base, "_") != 1 {
		return "", ""
	}
	u := strings.Index(base, "_")
	if u <= 0 || u == len(base)-1 {
		return "", ""
	}
	pkg, version = base[:u], base[u+1:]
	if archivePackage != "" && pkg != archivePackage {
		return "", ""
	}
	return pkg, version
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
	escapedVersion := strings.TrimSuffix(path[i+len("/@v/"):], ".zip")
	module, err := gomoduleidentity.DecodeProxyPath(path[:i])
	if err != nil {
		return "", ""
	}
	version, err = gomoduleidentity.DecodeProxyVersion(escapedVersion)
	if err != nil {
		return "", ""
	}
	return module, version
}
