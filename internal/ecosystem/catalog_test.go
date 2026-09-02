package ecosystem

import (
	"reflect"
	"testing"
)

func definitionNames(definitions []Definition) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return names
}

func TestCatalogCapabilitiesAndStableOrder(t *testing.T) {
	standard := []string{
		"pypi", "apt", "npm", "go", "cargo", "maven", "rubygems",
		"composer", "nuget", "conda", "cran", "alpine", "helm", "huggingface",
	}
	if got := definitionNames(StandardUpstreamDefinitions()); !reflect.DeepEqual(got, standard) {
		t.Fatalf("standard upstream ecosystems = %v, want %v", got, standard)
	}

	setup := []string{
		"pypi", "apt", "npm", "go", "cargo", "maven", "rubygems",
		"composer", "nuget", "conda", "cran", "alpine", "helm",
	}
	if got := definitionNames(SetupDefinitions()); !reflect.DeepEqual(got, setup) {
		t.Fatalf("setup ecosystems = %v, want %v", got, setup)
	}
	rules := []string{
		"pypi", "apt", "npm", "go", "cargo", "maven", "composer",
		"nuget", "conda", "cran", "alpine",
	}
	if got := definitionNames(RuleDefinitions()); !reflect.DeepEqual(got, rules) {
		t.Fatalf("rule ecosystems = %v, want %v", got, rules)
	}

	malicious := []string{"npm", "cargo", "composer", "nuget", "go", "maven"}
	if got := definitionNames(MaliciousDatasetDefinitions()); !reflect.DeepEqual(got, malicious) {
		t.Fatalf("malicious dataset ecosystems = %v, want %v", got, malicious)
	}
}

func TestCatalogDockerAndHuggingFaceUseDifferentCapabilities(t *testing.T) {
	docker, ok := Lookup("docker")
	if !ok {
		t.Fatal("docker is missing")
	}
	if docker.StandardUpstreams || docker.AvailableInSetup || docker.OSVName != "" || docker.Route != "/v2" {
		t.Fatalf("unexpected docker capabilities: %#v", docker)
	}

	huggingFace, ok := Lookup("huggingface")
	if !ok {
		t.Fatal("huggingface is missing")
	}
	if !huggingFace.StandardUpstreams || huggingFace.AvailableInSetup || huggingFace.OSVName != "" || huggingFace.Route != "/huggingface" {
		t.Fatalf("unexpected huggingface capabilities: %#v", huggingFace)
	}
}

func TestOSVMappingRoundTrips(t *testing.T) {
	for _, definition := range All() {
		if definition.OSVName == "" {
			continue
		}
		if got := OSVNameFor(definition.Name); got != definition.OSVName {
			t.Errorf("OSVNameFor(%q) = %q, want %q", definition.Name, got, definition.OSVName)
		}
		if got := NameForOSV(definition.OSVName); got != definition.Name {
			t.Errorf("NameForOSV(%q) = %q, want %q", definition.OSVName, got, definition.Name)
		}
	}
	if got := OSVNameFor("unknown"); got != "" {
		t.Fatalf("OSVNameFor(unknown) = %q, want empty", got)
	}
	if got := NameForOSV("Unknown"); got != "" {
		t.Fatalf("NameForOSV(Unknown) = %q, want empty", got)
	}
}

func TestAllReturnsIndependentSlice(t *testing.T) {
	first := All()
	first[0].Name = "changed"
	if second := All(); second[0].Name != "pypi" {
		t.Fatalf("catalog was mutated through returned slice: %#v", second[0])
	}
}

func TestCatalogDefinitionsHaveUniqueNamesRoutesAndValidCapabilities(t *testing.T) {
	names := make(map[string]bool)
	routes := make(map[string]bool)
	for _, definition := range All() {
		if definition.Name == "" || definition.Route == "" {
			t.Fatalf("catalog definition is missing name or route: %#v", definition)
		}
		if names[definition.Name] {
			t.Fatalf("duplicate ecosystem name %q", definition.Name)
		}
		if routes[definition.Route] {
			t.Fatalf("duplicate ecosystem route %q", definition.Route)
		}
		names[definition.Name] = true
		routes[definition.Route] = true
		if definition.AvailableInSetup && !definition.StandardUpstreams {
			t.Fatalf("setup ecosystem %q cannot be persisted by the shared upstream writer", definition.Name)
		}
		if definition.MaliciousDataset && definition.OSVName == "" {
			t.Fatalf("malicious dataset ecosystem %q has no OSV name", definition.Name)
		}
	}
}
