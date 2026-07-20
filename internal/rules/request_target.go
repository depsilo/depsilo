package rules

import (
	"path"
	"strings"
	"unicode"

	"depsilo/internal/adapter/packagekey"
	ecosystemcatalog "depsilo/internal/ecosystem"
)

// requestTarget is the package identity that can be established from a proxy
// request before the adapter runs. Version is intentionally empty for package
// metadata whose path does not identify a particular release.
type requestTarget struct {
	Ecosystem   string
	PackageName string
	Version     string
}

// extractRequestTarget recognizes both global proxy routes and their
// project-scoped /p/:slug counterparts. Route ownership comes from the
// ecosystem catalog so adding or renaming a proxy route cannot silently leave
// the policy middleware's path switch stale.
func extractRequestTarget(requestPath string) (requestTarget, bool) {
	requestPath = stripProjectPrefix(requestPath)

	for _, definition := range ecosystemcatalog.All() {
		relative, ok := trimRoute(requestPath, definition.Route)
		if !ok {
			continue
		}
		return extractEcosystemTarget(definition.Name, relative)
	}

	return requestTarget{}, false
}

func stripProjectPrefix(requestPath string) string {
	if !strings.HasPrefix(requestPath, "/p/") {
		return requestPath
	}

	rest := strings.TrimPrefix(requestPath, "/p/")
	slash := strings.IndexByte(rest, '/')
	if slash <= 0 {
		return requestPath
	}
	return rest[slash:]
}

func trimRoute(requestPath, route string) (string, bool) {
	if requestPath == route || requestPath == route+"/" {
		return "", true
	}
	prefix := route + "/"
	if !strings.HasPrefix(requestPath, prefix) {
		return "", false
	}
	return strings.TrimPrefix(requestPath, prefix), true
}

func extractEcosystemTarget(ecosystem, relative string) (requestTarget, bool) {
	relative = strings.TrimPrefix(relative, "/")

	switch ecosystem {
	case "pypi":
		return extractPyPITarget(relative)
	case "apt":
		return extractAPTTarget(relative)
	case "npm":
		return extractNPMTarget(relative)
	case "go":
		return extractGoTarget(relative)
	case "cargo":
		return extractCargoTarget(relative)
	case "maven":
		return extractMavenTarget(relative)
	case "rubygems":
		return extractRubyGemsTarget(relative)
	case "composer":
		return extractComposerTarget(relative)
	case "nuget":
		return extractNuGetTarget(relative)
	case "conda":
		name, version := packagekey.ParseCondaPath(relative)
		return artifactTarget(ecosystem, name, version)
	case "cran":
		return extractCRANTarget(relative)
	case "alpine":
		name, version := packagekey.ParseAlpinePath(relative)
		return artifactTarget(ecosystem, name, version)
	case "helm":
		name, version := packagekey.ParseHelmPath(relative)
		return artifactTarget(ecosystem, name, version)
	default:
		// Docker and Hugging Face use configuration-dependent identities and
		// are not exposed by the package-rules UI. Guessing their package key
		// here would make a rule apply to a different object than the adapter.
		return requestTarget{}, false
	}
}

func extractPyPITarget(relative string) (requestTarget, bool) {
	if strings.HasPrefix(relative, "simple/") {
		name := strings.TrimSuffix(strings.TrimPrefix(relative, "simple/"), "/")
		if name != "" && !strings.Contains(name, "/") {
			return metadataTarget("pypi", name)
		}
		return requestTarget{}, false
	}
	if strings.HasPrefix(relative, "files/") {
		name, version := packagekey.ParsePypiFilename(path.Base(relative))
		return artifactTarget("pypi", name, version)
	}
	return requestTarget{}, false
}

func extractAPTTarget(relative string) (requestTarget, bool) {
	if !strings.HasSuffix(relative, ".deb") {
		// APT indices contain many packages, so no single package identity can
		// be inferred safely from their URL.
		return requestTarget{}, false
	}
	key := "apt/" + relative
	return artifactTarget(
		"apt",
		packagekey.ExtractName("apt", key),
		packagekey.ExtractVersion("apt", key),
	)
}

func extractNPMTarget(relative string) (requestTarget, bool) {
	parts := splitPath(relative)
	if len(parts) == 0 {
		return requestTarget{}, false
	}

	var name string
	var artifactAt int
	if strings.HasPrefix(parts[0], "@") {
		if len(parts) < 2 {
			return requestTarget{}, false
		}
		name = parts[0] + "/" + parts[1]
		artifactAt = 2
	} else {
		name = parts[0]
		artifactAt = 1
	}

	if len(parts) == artifactAt {
		return metadataTarget("npm", name)
	}
	if len(parts) == artifactAt+2 && parts[artifactAt] == "-" {
		version := packagekey.ParseNpmFilename(name, parts[artifactAt+1])
		return artifactTarget("npm", name, version)
	}
	return requestTarget{}, false
}

func extractGoTarget(relative string) (requestTarget, bool) {
	if strings.HasSuffix(relative, ".zip") {
		name, version := packagekey.ParseGoZipPath(relative)
		return artifactTarget("go", name, version)
	}

	for _, suffix := range []string{"/@v/list", "/@latest"} {
		if strings.HasSuffix(relative, suffix) {
			name := unescapeGoPath(strings.TrimSuffix(relative, suffix))
			return metadataTarget("go", name)
		}
	}

	idx := strings.LastIndex(relative, "/@v/")
	if idx <= 0 {
		return requestTarget{}, false
	}
	versionFile := relative[idx+len("/@v/"):]
	for _, suffix := range []string{".info", ".mod"} {
		if strings.HasSuffix(versionFile, suffix) {
			name := unescapeGoPath(relative[:idx])
			version := strings.TrimSuffix(versionFile, suffix)
			return versionedTarget("go", name, version)
		}
	}
	return requestTarget{}, false
}

func extractCargoTarget(relative string) (requestTarget, bool) {
	if strings.HasPrefix(relative, "api/v1/crates/") {
		name, version := packagekey.ParseCargoCratePath(relative)
		return artifactTarget("cargo", name, version)
	}
	if relative == "" || relative == "config.json" || strings.HasPrefix(relative, "api/") {
		return requestTarget{}, false
	}
	parts := splitPath(relative)
	if len(parts) == 0 {
		return requestTarget{}, false
	}
	// Sparse-index metadata paths end in the crate name, while the directory
	// prefix is a sharding detail and is not part of the package identity.
	return metadataTarget("cargo", parts[len(parts)-1])
}

func extractMavenTarget(relative string) (requestTarget, bool) {
	parts := splitPath(relative)
	if len(parts) < 3 {
		return requestTarget{}, false
	}

	filename := parts[len(parts)-1]
	if hasAnySuffix(filename, ".jar", ".pom", ".aar") {
		if len(parts) < 4 {
			return requestTarget{}, false
		}
		artifact := parts[len(parts)-3]
		version := parts[len(parts)-2]
		group := strings.Join(parts[:len(parts)-3], ".")
		if group == "" || artifact == "" || version == "" || !strings.HasPrefix(filename, artifact+"-") {
			return requestTarget{}, false
		}
		return artifactTarget("maven", group+":"+artifact, version)
	}

	if filename != "maven-metadata.xml" {
		return requestTarget{}, false
	}
	dirs := parts[:len(parts)-1]
	if len(dirs) < 2 {
		return requestTarget{}, false
	}

	artifactAt := len(dirs) - 1
	version := ""
	if len(dirs) >= 3 && looksLikeMavenVersion(dirs[len(dirs)-1]) {
		version = dirs[len(dirs)-1]
		artifactAt--
	}
	if artifactAt <= 0 {
		return requestTarget{}, false
	}
	group := strings.Join(dirs[:artifactAt], ".")
	name := group + ":" + dirs[artifactAt]
	if version == "" {
		return metadataTarget("maven", name)
	}
	return versionedTarget("maven", name, version)
}

func extractRubyGemsTarget(relative string) (requestTarget, bool) {
	if strings.HasPrefix(relative, "gems/") {
		name, version := packagekey.ParseRubygemsFilename(path.Base(relative))
		return artifactTarget("rubygems", name, version)
	}
	if strings.HasPrefix(relative, "info/") {
		name := strings.Trim(strings.TrimPrefix(relative, "info/"), "/")
		if name != "" && !strings.Contains(name, "/") {
			return metadataTarget("rubygems", name)
		}
	}
	if strings.HasPrefix(relative, "quick/") && strings.HasSuffix(relative, ".gemspec.rz") {
		base := strings.TrimSuffix(path.Base(relative), ".gemspec.rz")
		name, version := splitDashVersion(base)
		return versionedTarget("rubygems", name, version)
	}
	return requestTarget{}, false
}

func extractComposerTarget(relative string) (requestTarget, bool) {
	if strings.HasPrefix(relative, "p2/") && strings.HasSuffix(relative, ".json") {
		name := strings.TrimSuffix(strings.TrimPrefix(relative, "p2/"), ".json")
		name = strings.TrimSuffix(name, "~dev")
		if parts := splitPath(name); len(parts) == 2 {
			return metadataTarget("composer", parts[0]+"/"+parts[1])
		}
		return requestTarget{}, false
	}
	if !strings.HasPrefix(relative, "dist/") {
		return requestTarget{}, false
	}
	parts := splitPath(strings.TrimPrefix(relative, "dist/"))
	if len(parts) < 4 || !strings.Contains(parts[len(parts)-1], ".") {
		return requestTarget{}, false
	}
	name := parts[0] + "/" + parts[1]
	version := strings.Join(parts[2:len(parts)-1], "/")
	return artifactTarget("composer", name, version)
}

func extractNuGetTarget(relative string) (requestTarget, bool) {
	if name, version := packagekey.ParseNugetPath(relative); name != "" || version != "" {
		return artifactTarget("nuget", name, version)
	}

	parts := splitPath(relative)
	if len(parts) >= 2 && parts[0] == "v3-flatcontainer" {
		return metadataTarget("nuget", parts[1])
	}
	if len(parts) >= 3 && parts[0] == "v3" && strings.HasPrefix(parts[1], "registration") {
		name := parts[2]
		if len(parts) >= 4 && parts[3] != "index.json" {
			version := strings.TrimSuffix(parts[3], ".json")
			return versionedTarget("nuget", name, version)
		}
		return metadataTarget("nuget", name)
	}
	return requestTarget{}, false
}

func extractCRANTarget(relative string) (requestTarget, bool) {
	if name, version := packagekey.ParseCranPath(relative); name != "" || version != "" {
		return artifactTarget("cran", name, version)
	}
	parts := splitPath(relative)
	if len(parts) >= 3 && parts[0] == "web" && parts[1] == "packages" {
		return metadataTarget("cran", parts[2])
	}
	return requestTarget{}, false
}

func metadataTarget(ecosystem, name string) (requestTarget, bool) {
	return newTarget(ecosystem, name, "", false)
}

func versionedTarget(ecosystem, name, version string) (requestTarget, bool) {
	return newTarget(ecosystem, name, version, true)
}

func artifactTarget(ecosystem, name, version string) (requestTarget, bool) {
	// Never downgrade a malformed or unfamiliar artifact path to a
	// package-wide check. Without its version, a constrained rule cannot be
	// evaluated accurately and the middleware must fail open.
	return newTarget(ecosystem, name, version, true)
}

func newTarget(ecosystem, name, version string, requireVersion bool) (requestTarget, bool) {
	if ecosystem == "" || name == "" || (requireVersion && version == "") {
		return requestTarget{}, false
	}
	return requestTarget{Ecosystem: ecosystem, PackageName: name, Version: version}, true
}

func splitPath(value string) []string {
	raw := strings.Split(strings.Trim(value, "/"), "/")
	parts := raw[:0]
	for _, part := range raw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func hasAnySuffix(value string, suffixes ...string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(value, suffix) {
			return true
		}
	}
	return false
}

func splitDashVersion(base string) (string, string) {
	for i := len(base) - 1; i > 0; i-- {
		if base[i] == '-' && i+1 < len(base) && base[i+1] >= '0' && base[i+1] <= '9' {
			return base[:i], base[i+1:]
		}
	}
	return "", ""
}

func looksLikeMavenVersion(value string) bool {
	if value == "" {
		return false
	}
	if value[0] >= '0' && value[0] <= '9' {
		return true
	}
	return len(value) > 1 && (value[0] == 'v' || value[0] == 'V') && value[1] >= '0' && value[1] <= '9'
}

// unescapeGoPath mirrors GOPROXY's !lower encoding for uppercase letters.
// The artifact helper already performs this conversion; metadata paths need
// it here so both request shapes address the same rule key.
func unescapeGoPath(value string) string {
	if !strings.Contains(value, "!") {
		return value
	}
	var out strings.Builder
	out.Grow(len(value))
	escaped := false
	for _, char := range value {
		if escaped {
			out.WriteRune(unicode.ToUpper(char))
			escaped = false
			continue
		}
		if char == '!' {
			escaped = true
			continue
		}
		out.WriteRune(char)
	}
	return out.String()
}
