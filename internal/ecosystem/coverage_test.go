package ecosystem_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/api/public"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/ecosystem"
	"depsilo/internal/quarantine"
)

// The catalog drives routing, setup validation and upstream bootstrap, but
// several per-ecosystem lists elsewhere are written by hand. These tests range
// over the catalog and fail when an ecosystem is missing from one of them, so
// adding an ecosystem to the catalog points at every list that needs an entry.

// cacheKeySamples holds, for each standard-upstream ecosystem, one metadata
// and one artifact cache key in the shape its adapter generates, and the
// package the artifact key names. A new ecosystem needs a row here; the row
// then requires its cases in db.ClassifyCacheKind, packagekey.ExtractName and
// packagekey.IsPackageFile.
var cacheKeySamples = map[string]struct{ metadata, artifact, packageName string }{
	"pypi":        {"pypi/simple/requests/index.html", "pypi/files/packages/ab/cd/requests-2.31.0-py3-none-any.whl", "requests"},
	"apt":         {"apt/ubuntu/dists/jammy/InRelease", "apt/ubuntu/pool/main/c/curl/curl_8.1.0-1_amd64.deb", "curl"},
	"npm":         {"npm-exact-v1/left-pad/metadata.json", "npm-exact-v1/left-pad/-/left-pad-1.3.0.tgz", "left-pad"},
	"go":          {"go/example.com/mod/@v/list", "go/example.com/mod/@v/v1.0.0.zip", "example.com/mod"},
	"cargo":       {"cargo/index/se/rd/serde", "cargo/crates/serde/1.0.0.crate", "serde"},
	"maven":       {"maven/org/example/demo/maven-metadata.xml", "maven/org/example/demo/1.0/demo-1.0.jar", "org.example:demo"},
	"rubygems":    {"rubygems/info/rails", "rubygems/gems/rails-7.1.0.gem", "rails"},
	"composer":    {"composer/p2/monolog/monolog.json", "composer/dist/monolog/monolog/0123456789abcdef.zip", "monolog/monolog"},
	"nuget":       {"nuget/v3/index.json", "nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg", "newtonsoft.json"},
	"conda":       {"conda/pkgs/main/linux-64/repodata.json", "conda/pkgs/main/linux-64/numpy-1.26.0-py311_0.conda", "numpy"},
	"cran":        {"cran/src/contrib/PACKAGES", "cran/src/contrib/dplyr_1.1.4.tar.gz", "dplyr"},
	"alpine":      {"alpine/v3.20/main/x86_64/APKINDEX.tar.gz", "alpine/v3.20/main/x86_64/curl-8.5.0-r0.apk", "curl"},
	"helm":        {"helm/index.yaml", "helm/charts/nginx-1.2.3.tgz", "nginx"},
	"huggingface": {"huggingface/api/models/google/flan-t5-base", "huggingface/google/flan-t5-base/resolve/main/model.safetensors", "google/flan-t5-base"},
}

func TestEveryStandardEcosystemClassifiesItsCacheKeys(t *testing.T) {
	for _, definition := range ecosystem.StandardUpstreamDefinitions() {
		sample, ok := cacheKeySamples[definition.Name]
		if !ok {
			t.Errorf("%s: no cache key sample; add one here and its cases to db.ClassifyCacheKind (internal/db/cache_kind.go) and packagekey.ExtractName (internal/adapter/packagekey/extract.go)", definition.Name)
			continue
		}
		// An ecosystem without a case falls through to "artifact", which would
		// let its index outlive ttl_index and skip synchronous refresh.
		if got := db.ClassifyCacheKind(definition.Name, sample.metadata); got != db.CacheKindMetadata {
			t.Errorf("db.ClassifyCacheKind(%q, %q) = %q, want %q", definition.Name, sample.metadata, got, db.CacheKindMetadata)
		}
		if got := db.ClassifyCacheKind(definition.Name, sample.artifact); got != db.CacheKindArtifact {
			t.Errorf("db.ClassifyCacheKind(%q, %q) = %q, want %q", definition.Name, sample.artifact, got, db.CacheKindArtifact)
		}
	}
}

func TestEveryStandardEcosystemNamesThePackageOfItsArtifactKey(t *testing.T) {
	for _, definition := range ecosystem.StandardUpstreamDefinitions() {
		sample, ok := cacheKeySamples[definition.Name]
		if !ok {
			t.Errorf("%s: no cache key sample", definition.Name)
			continue
		}
		// An ecosystem without a case yields "", which hides its packages from
		// access logs, cache search and security scans.
		if got := packagekey.ExtractName(definition.Name, sample.artifact); got != sample.packageName {
			t.Errorf("packagekey.ExtractName(%q, %q) = %q, want %q", definition.Name, sample.artifact, got, sample.packageName)
		}
		// Without a case in packagekey.IsPackageFile (classify.go) no download
		// of the ecosystem is recorded for a project.
		if !packagekey.IsPackageFile(definition.Name, sample.artifact) || packagekey.IsPackageFile(definition.Name, sample.metadata) {
			t.Errorf("packagekey.IsPackageFile(%q) = %t for artifact %q and %t for metadata %q, want true and false", definition.Name,
				packagekey.IsPackageFile(definition.Name, sample.artifact), sample.artifact,
				packagekey.IsPackageFile(definition.Name, sample.metadata), sample.metadata)
		}
	}
}

func TestEveryEcosystemHasADefaultMinimumReleaseAge(t *testing.T) {
	thresholds := quarantine.DefaultThresholds()
	for _, definition := range ecosystem.All() {
		if _, ok := thresholds[definition.Name]; !ok {
			t.Errorf("%s: missing from quarantine.DefaultThresholds (internal/quarantine/policy.go)", definition.Name)
		}
	}
}

func TestEveryEcosystemHasADiscoverPurpose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var names []string
	for _, definition := range ecosystem.All() {
		names = append(names, definition.Name)
	}
	router := gin.New()
	router.GET("/api/v1/discover", public.NewDiscoverHandler(names).Discover)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/discover", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("discover = %d %s", response.Code, response.Body.String())
	}
	var catalog struct {
		Ecosystems []struct{ Name, Purpose string } `json:"ecosystems"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Ecosystems) != len(names) {
		t.Fatalf("discover lists %d ecosystems, want %d", len(catalog.Ecosystems), len(names))
	}
	for _, listed := range catalog.Ecosystems {
		// Without an entry the handler falls back to the bare name.
		if listed.Purpose == "" || listed.Purpose == listed.Name {
			t.Errorf("%s: missing from ecosystemPurposes (internal/api/public/discover.go)", listed.Name)
		}
	}
}

func TestEveryStandardEcosystemValidatesCascadeReferences(t *testing.T) {
	home := t.TempDir()
	t.Chdir(home)
	t.Setenv("HOME", home)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "test-only-0123456789abcdef0123456789abcdef")
	path := filepath.Join(home, "config.toml")
	t.Setenv("DEPSILO_CONFIG", path)

	for _, definition := range ecosystem.StandardUpstreamDefinitions() {
		// An upstream that egresses through a peer nobody configured must be
		// refused at load. An ecosystem missing from the list of upstream
		// slices in internal/config/cascade.go would load it silently.
		document := fmt.Sprintf(`config_version = 1

[[%s.upstreams]]
name = "probe"
url = "https://upstream.example"
priority = 1
via = "ghost"
`, definition.Name)
		if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := config.Load()
		want := fmt.Sprintf(`%s upstream "probe" references unknown cascade peer "ghost"`, definition.Name)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: config.Load() error = %v, want it to contain %q (add the ecosystem to standardUpstreamConfigSlices in internal/config/cascade.go)", definition.Name, err, want)
		}
	}
}
