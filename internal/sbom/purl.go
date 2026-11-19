package sbom

import (
	"regexp"
	"strings"
)

// FormatPURL creates a Package URL for a package recorded under a Depsilo
// ecosystem name. Each ecosystem maps to its registered purl type
// (github.com/package-url/purl-spec/types) with that type's namespace split
// and normalization, and every component is percent-encoded as ECMA-427
// requires. Depsilo's own package-name conventions (Maven "group:artifact",
// npm "@scope/name") are kept elsewhere because OSV and Package Rules rely on
// them; only this identifier is translated.
func FormatPURL(ecosystem, packageName, version string) string {
	if version == "" {
		version = "unknown"
	}

	purlType, namespace, name := "generic", "", packageName
	switch ecosystem {
	case "pypi":
		purlType, name = "pypi", normalizePyPIName(packageName)
	case "npm":
		purlType = "npm"
		if scope, rest, scoped := strings.Cut(packageName, "/"); scoped && strings.HasPrefix(scope, "@") {
			namespace, name = scope, rest
		}
	case "go":
		purlType = "golang"
		namespace, name = splitLast(packageName)
	case "cargo":
		purlType = "cargo"
	case "maven":
		purlType = "maven"
		if group, artifact, found := strings.Cut(packageName, ":"); found {
			namespace, name = group, artifact
		}
	case "rubygems":
		purlType = "gem"
	case "composer":
		purlType = "composer"
		namespace, name = splitLast(strings.ToLower(packageName))
	case "nuget":
		purlType = "nuget"
	case "conda":
		purlType = "conda"
	case "cran":
		purlType = "cran"
	case "alpine":
		purlType, namespace, name = "apk", "alpine", strings.ToLower(packageName)
	case "apt":
		purlType, namespace, name = "deb", "debian", strings.ToLower(packageName)
	case "helm":
		purlType = "helm"
	case "docker":
		purlType = "docker"
		namespace, name = splitLast(packageName)
	case "huggingface":
		// The huggingface type names a model repository as
		// <owner>/<repository> and lowercases its commit-hash version.
		// Datasets and owner-less legacy repositories have no defined form
		// and stay generic.
		if owner, repository, found := strings.Cut(packageName, "/"); found && owner != "datasets" && !strings.Contains(repository, "/") {
			purlType, namespace, name = "huggingface", owner, repository
			version = strings.ToLower(version)
		} else {
			namespace, name = splitLast(packageName)
		}
	default:
		namespace, name = splitLast(packageName)
	}

	var b strings.Builder
	b.WriteString("pkg:")
	b.WriteString(purlType)
	b.WriteByte('/')
	for _, segment := range strings.Split(namespace, "/") {
		if segment == "" {
			continue
		}
		b.WriteString(purlEscape(segment))
		b.WriteByte('/')
	}
	b.WriteString(purlEscape(strings.Trim(name, "/")))
	b.WriteByte('@')
	b.WriteString(purlEscape(version))
	return b.String()
}

// splitLast splits a slash-separated identity into its namespace and its
// last segment, the purl name.
func splitLast(identity string) (namespace, name string) {
	identity = strings.Trim(identity, "/")
	if i := strings.LastIndex(identity, "/"); i >= 0 {
		return identity[:i], identity[i+1:]
	}
	return "", identity
}

var pypiSeparatorRun = regexp.MustCompile(`[-_.]+`)

// normalizePyPIName applies the PEP 503 normalization the pypi purl type
// requires.
func normalizePyPIName(name string) string {
	return pypiSeparatorRun.ReplaceAllString(strings.ToLower(name), "-")
}

// purlEscape percent-encodes one purl component. ECMA-427 leaves only the
// alphanumerics, the punctuation ".-_~" and the colon unencoded.
func purlEscape(component string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(component); i++ {
		c := component[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '.', c == '-', c == '_', c == '~', c == ':':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
