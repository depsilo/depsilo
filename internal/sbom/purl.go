package sbom

import (
	"fmt"
	"strings"
)

// FormatPURL creates a Package URL identifier for a package.
// See https://github.com/package-url/purl-spec
func FormatPURL(ecosystem, packageName, version string) string {
	if version == "" {
		version = "unknown"
	}

	switch ecosystem {
	case "pypi":
		return fmt.Sprintf("pkg:pypi/%s@%s", packageName, version)
	case "npm":
		if strings.HasPrefix(packageName, "@") {
			// Scoped: @scope/name → pkg:npm/%40scope/name@version
			return fmt.Sprintf("pkg:npm/%s@%s", strings.Replace(packageName, "@", "%40", 1), version)
		}
		return fmt.Sprintf("pkg:npm/%s@%s", packageName, version)
	case "go":
		return fmt.Sprintf("pkg:golang/%s@%s", packageName, version)
	case "cargo":
		return fmt.Sprintf("pkg:cargo/%s@%s", packageName, version)
	case "maven":
		// Maven packageName might be "groupId/artifactId" or just "artifactId"
		if strings.Contains(packageName, "/") {
			parts := strings.SplitN(packageName, "/", 2)
			return fmt.Sprintf("pkg:maven/%s/%s@%s", parts[0], parts[1], version)
		}
		return fmt.Sprintf("pkg:maven/%s@%s", packageName, version)
	case "rubygems":
		return fmt.Sprintf("pkg:gem/%s@%s", packageName, version)
	case "composer":
		return fmt.Sprintf("pkg:composer/%s@%s", packageName, version)
	case "nuget":
		return fmt.Sprintf("pkg:nuget/%s@%s", packageName, version)
	case "conda":
		return fmt.Sprintf("pkg:conda/%s@%s", packageName, version)
	case "cran":
		return fmt.Sprintf("pkg:cran/%s@%s", packageName, version)
	case "helm":
		return fmt.Sprintf("pkg:helm/%s@%s", packageName, version)
	case "apt":
		return fmt.Sprintf("pkg:deb/debian/%s@%s", packageName, version)
	case "docker":
		return fmt.Sprintf("pkg:docker/%s@%s", packageName, version)
	default:
		return fmt.Sprintf("pkg:generic/%s@%s", packageName, version)
	}
}
