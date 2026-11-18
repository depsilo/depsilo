package ecosystem

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupDefaultsTS is the wizard's copy of DefaultUpstreams. This test writes
// it when DEPSILO_UPDATE_GENERATED=1 (make generate) and fails when it is stale.
var setupDefaultsTS = filepath.Join("..", "..", "web", "src", "lib", "generated", "setupUpstreamDefaults.ts")

func TestEverySetupEcosystemHasBuiltInDefaultsAndNoOtherDoes(t *testing.T) {
	for _, definition := range All() {
		upstreams := DefaultUpstreams(definition.Name)
		if definition.AvailableInSetup != (len(upstreams) > 0) {
			t.Fatalf("%s: AvailableInSetup=%v but %d default Upstreams", definition.Name, definition.AvailableInSetup, len(upstreams))
		}
		for index, upstream := range upstreams {
			if upstream.Priority != index+1 {
				t.Fatalf("%s/%s: priority %d, want %d (defaults are listed in priority order)", definition.Name, upstream.Name, upstream.Priority, index+1)
			}
			if !strings.HasPrefix(upstream.URL, "https://") {
				t.Fatalf("%s/%s: URL %q is not https", definition.Name, upstream.Name, upstream.URL)
			}
		}
	}
	if len(defaultUpstreams) != len(SetupDefinitions()) {
		t.Fatalf("defaults name %d ecosystems, setup offers %d", len(defaultUpstreams), len(SetupDefinitions()))
	}
}

func TestDefaultUpstreamsReturnsACopy(t *testing.T) {
	DefaultUpstreams("pypi")[0].URL = "https://changed.example"
	if got := DefaultUpstreams("pypi")[0].URL; got == "https://changed.example" {
		t.Fatal("DefaultUpstreams exposed the shared slice")
	}
}

func TestSetupWizardDefaultsAreGeneratedFromTheCatalog(t *testing.T) {
	want := renderSetupDefaultsTS(t)
	if os.Getenv("DEPSILO_UPDATE_GENERATED") == "1" {
		if err := os.WriteFile(setupDefaultsTS, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(setupDefaultsTS)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s is stale; run make generate", setupDefaultsTS)
	}
}

func renderSetupDefaultsTS(t *testing.T) string {
	t.Helper()
	type entry struct {
		Key       string            `json:"key"`
		Upstreams []DefaultUpstream `json:"upstreams"`
	}
	entries := make([]entry, 0, len(SetupDefinitions()))
	for _, definition := range SetupDefinitions() {
		entries = append(entries, entry{Key: definition.Name, Upstreams: DefaultUpstreams(definition.Name)})
	}
	encoded, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return "// Code generated from internal/ecosystem/defaults.go by make generate. DO NOT EDIT.\n\n" +
		"export const setupUpstreamDefaults = " + string(encoded) + " as const\n"
}
