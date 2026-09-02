package rules

import (
	"fmt"
	"path"
	"strings"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/adapter/pypi"
	ecosystemcatalog "depsilo/internal/ecosystem"
	"depsilo/internal/gomoduleidentity"
	"depsilo/internal/packagepolicy"
)

// requestTarget is the package identity that can be established from a proxy
// request before the adapter runs. Version is intentionally empty for package
// metadata whose path does not identify a particular release. An
// AmbiguousArtifact target identifies only an ecosystem artifact route; its
// package and version are deliberately empty.
type requestTarget struct {
	Ecosystem         string
	PackageName       string
	Version           string
	AmbiguousArtifact bool
}

// PyPIRouteDescriptor is one validated, configuration-owned PyPI-compatible
// proxy route. Its fields are private so callers cannot bypass construction
// validation or silently change route semantics after middleware creation.
type PyPIRouteDescriptor struct {
	path          string
	channelFamily bool
}

// NewPyPIRouteDescriptor creates a literal route descriptor from a normalized
// extra-index path. A channel-family route accepts exactly the same channel
// segments as the PyTorch adapter before its /simple and /files subtrees.
func NewPyPIRouteDescriptor(configuredPath string, channelFamily bool) (PyPIRouteDescriptor, error) {
	if !validConfiguredRoutePath(configuredPath) {
		return PyPIRouteDescriptor{}, fmt.Errorf("invalid configured PyPI route %q", configuredPath)
	}
	return PyPIRouteDescriptor{path: "/" + configuredPath, channelFamily: channelFamily}, nil
}

// extractRequestTarget recognizes both global proxy routes and their
// project-scoped /p/:slug counterparts. Standard route ownership comes from
// the ecosystem catalog; config-owned PyPI routes must be injected explicitly
// so the middleware never infers an extra-index namespace from request data.
func extractRequestTarget(requestPath string, extraPyPIRoutes ...PyPIRouteDescriptor) (requestTarget, bool) {
	requestPath = stripProjectPrefix(requestPath)

	for _, definition := range ecosystemcatalog.RuleDefinitions() {
		relative, ok := trimRoute(requestPath, definition.Route)
		if !ok {
			continue
		}
		return extractEcosystemTarget(definition.Name, relative)
	}
	for _, route := range extraPyPIRoutes {
		if target, ok := route.extract(requestPath); ok {
			return target, true
		}
	}

	return requestTarget{}, false
}

func (route PyPIRouteDescriptor) extract(requestPath string) (requestTarget, bool) {
	// The exported descriptor's zero value is intentionally inert. Callers in
	// other packages can name that value even though only the constructor can
	// populate its private fields.
	if route.path == "" {
		return requestTarget{}, false
	}
	relative, ok := trimRoute(requestPath, route.path)
	if !ok {
		return requestTarget{}, false
	}
	if !route.channelFamily {
		return extractPyPITarget(relative)
	}
	channel, relative, ok := strings.Cut(strings.TrimPrefix(relative, "/"), "/")
	if !ok || !pypi.ValidIndexChannel(channel) {
		return requestTarget{}, false
	}
	return extractPyPITarget(relative)
}

func validConfiguredRoutePath(value string) bool {
	if value == "" || value != strings.Trim(value, "/") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			if character >= 'a' && character <= 'z' ||
				character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' ||
				character == '.' || character == '_' || character == '-' {
				continue
			}
			return false
		}
	}
	return true
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
		// npm metadata must remain discoverable so an End User can obtain the
		// authenticated artifact URL. The npm Adapter evaluates every artifact
		// rule only after verifying that URL's exact package/version provenance;
		// legacy URLs are rejected there without any policy inference here.
		return requestTarget{}, false
	case "go":
		return extractGoTarget(relative)
	case "cargo":
		return extractCargoTarget(relative)
	case "maven":
		return extractMavenTarget(relative)
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
	default:
		// Ecosystems without RuleEnforcement are never passed here. In
		// particular, Docker and Hugging Face use configuration-dependent
		// identities, while RubyGems and Helm artifact names are ambiguous.
		// Guessing any of those keys could apply a global rule to the wrong
		// object.
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
		filename := path.Base(relative)
		if isPyPISidecar(filename) {
			// PEP 658 metadata is attached to an artifact but is not itself an
			// installable artifact, so it remains outside Package Rules.
			return requestTarget{}, false
		}
		name, version := packagekey.ParsePypiFilename(filename)
		if validPyPIArtifactIdentity(name, version) {
			return artifactTarget("pypi", name, version)
		}
		// Every non-sidecar file in the PyPI artifact subtree is potentially an
		// artifact. The filename does not establish one authoritative
		// package/version pair, so keep that state explicit rather than letting
		// an unfamiliar archive format bypass Package Rules.
		return requestTarget{Ecosystem: "pypi", AmbiguousArtifact: true}, true
	}
	return requestTarget{}, false
}

func validPyPIArtifactIdentity(name, version string) bool {
	if name == "" || version == "" {
		return false
	}
	dialect, err := packagepolicy.DialectFor("pypi")
	if err != nil {
		return false
	}
	if _, err := dialect.NormalizePackageName(name); err != nil {
		return false
	}
	return dialect.ValidateVersion(version) == nil
}

func isPyPISidecar(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".metadata")
}

func extractAPTTarget(relative string) (requestTarget, bool) {
	if !hasAnySuffix(relative, ".deb", ".udeb") {
		// APT indices contain many packages, so no single package identity can
		// be inferred safely from their URL.
		return requestTarget{}, false
	}
	key := "apt/" + relative
	// Debian deliberately omits the epoch from .deb filenames. Treat the
	// transport version as unknown until an index-derived Filename -> Version
	// mapping can provide the complete Debian version; comparing it as epoch 0
	// would make range rules unsound.
	return metadataTarget("apt", packagekey.ExtractName("apt", key))
}

func extractGoTarget(relative string) (requestTarget, bool) {
	if strings.HasSuffix(relative, ".zip") {
		name, version := packagekey.ParseGoZipPath(relative)
		return artifactTarget("go", name, version)
	}

	for _, suffix := range []string{"/@v/list", "/@latest"} {
		if strings.HasSuffix(relative, suffix) {
			name, err := gomoduleidentity.DecodeProxyPath(strings.TrimSuffix(relative, suffix))
			if err != nil {
				return requestTarget{}, false
			}
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
			name, err := gomoduleidentity.DecodeProxyPath(relative[:idx])
			if err != nil {
				return requestTarget{}, false
			}
			version, err := gomoduleidentity.DecodeProxyVersion(strings.TrimSuffix(versionFile, suffix))
			if err != nil {
				return requestTarget{}, false
			}
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
	// Sparse-index filenames are lowercase even though the authoritative name
	// in each JSON record is case-sensitive. The pre-adapter middleware cannot
	// recover that original identity from the URL, so it must not guess. Cargo
	// artifact downloads above retain the original crate name and remain fully
	// enforceable.
	return requestTarget{}, false
}

func extractMavenTarget(relative string) (requestTarget, bool) {
	coordinate, version := packagekey.ParseMavenPath(relative)
	return artifactTarget("maven", coordinate, version)
}

func extractComposerTarget(relative string) (requestTarget, bool) {
	name, version, ok := packagekey.ParseComposerRequestPath(relative)
	if !ok {
		return requestTarget{}, false
	}
	if version == "" {
		return metadataTarget("composer", name)
	}
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
	// Package identity and version identity are independent. If a known package
	// has an unfamiliar artifact filename, keep the package identity so a
	// package-wide rule still applies; an empty version never enters a dialect
	// comparator and therefore cannot match an exact/range rule.
	return newTarget(ecosystem, name, version, false)
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
