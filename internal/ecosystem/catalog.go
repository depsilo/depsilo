// Package ecosystem owns the canonical metadata for package ecosystems that
// Depsilo understands. Protocol implementations still live in their adapter
// packages; this catalog keeps capability checks and stable iteration order in
// one dependency-free place.
package ecosystem

// Definition describes one ecosystem and the features that apply to it.
//
// StandardUpstreams means the ecosystem uses config.AdapterConfig and the
// upstream control-plane bootstrap. Docker deliberately does not: registries
// have a different configuration model.
//
// AvailableInSetup describes the ecosystems that the first-run wizard can
// persist. Hugging Face is an advanced configuration option; Docker uses a
// separate registries model that the current setup payload cannot express.
// RuleEnforcement means request paths can be mapped to an unambiguous package
// identity by the package-policy middleware.
type Definition struct {
	Name              string
	Route             string
	StandardUpstreams bool
	AvailableInSetup  bool
	RuleEnforcement   bool
	OSVName           string
	MaliciousDataset  bool
}

var definitions = [...]Definition{
	{Name: "pypi", Route: "/pypi", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "PyPI", MaliciousDataset: true},
	{Name: "apt", Route: "/apt", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "Debian"},
	{Name: "npm", Route: "/npm", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "npm", MaliciousDataset: true},
	{Name: "go", Route: "/go", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "Go", MaliciousDataset: true},
	{Name: "cargo", Route: "/crates", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "crates.io", MaliciousDataset: true},
	{Name: "maven", Route: "/maven", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "Maven", MaliciousDataset: true},
	{Name: "rubygems", Route: "/rubygems", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "RubyGems", MaliciousDataset: true},
	{Name: "composer", Route: "/composer", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "Packagist", MaliciousDataset: true},
	{Name: "nuget", Route: "/nuget", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "NuGet", MaliciousDataset: true},
	{Name: "conda", Route: "/conda", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true},
	{Name: "cran", Route: "/cran", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true, OSVName: "CRAN"},
	{Name: "alpine", Route: "/alpine", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true},
	{Name: "helm", Route: "/helm", StandardUpstreams: true, AvailableInSetup: true, RuleEnforcement: true},
	{Name: "huggingface", Route: "/huggingface", StandardUpstreams: true},
	{Name: "docker", Route: "/v2"},
}

// The malicious-package status API has historically exposed this order. Keep
// it stable independently of the proxy registration order above.
var maliciousDatasetOrder = [...]string{
	"npm", "pypi", "cargo", "rubygems", "composer", "nuget", "go", "maven",
}

// All returns every known ecosystem in stable proxy registration order.
func All() []Definition {
	out := make([]Definition, len(definitions))
	copy(out, definitions[:])
	return out
}

// Lookup returns the canonical definition for name.
func Lookup(name string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Name == name {
			return definition, true
		}
	}
	return Definition{}, false
}

// StandardUpstreamDefinitions returns ecosystems that participate in the
// shared upstream bootstrap, in stable order.
func StandardUpstreamDefinitions() []Definition {
	return matching(func(definition Definition) bool { return definition.StandardUpstreams })
}

// SetupDefinitions returns ecosystems that the first-run wizard can persist,
// in stable order.
func SetupDefinitions() []Definition {
	return matching(func(definition Definition) bool { return definition.AvailableInSetup })
}

// RuleDefinitions returns ecosystems whose request paths expose a package and
// version identity precise enough for package policy enforcement.
func RuleDefinitions() []Definition {
	return matching(func(definition Definition) bool { return definition.RuleEnforcement })
}

// MaliciousDatasetDefinitions returns ecosystems covered by OSV's malicious
// package archives, preserving the status API's historical order.
func MaliciousDatasetDefinitions() []Definition {
	out := make([]Definition, 0, len(maliciousDatasetOrder))
	for _, name := range maliciousDatasetOrder {
		definition, ok := Lookup(name)
		if ok {
			out = append(out, definition)
		}
	}
	return out
}

// OSVNameFor returns the OSV ecosystem name for a Depsilo adapter name. An
// empty result means OSV queries are not supported for that ecosystem.
func OSVNameFor(name string) string {
	definition, ok := Lookup(name)
	if !ok {
		return ""
	}
	return definition.OSVName
}

// NameForOSV returns the Depsilo adapter name for an OSV ecosystem name.
func NameForOSV(osvName string) string {
	for _, definition := range definitions {
		if definition.OSVName == osvName && osvName != "" {
			return definition.Name
		}
	}
	return ""
}

func matching(include func(Definition) bool) []Definition {
	out := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		if include(definition) {
			out = append(out, definition)
		}
	}
	return out
}
