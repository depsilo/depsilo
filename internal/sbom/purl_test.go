package sbom

import (
	"encoding/json"
	"testing"
)

// The expected strings follow the registered purl type definitions
// (github.com/package-url/purl-spec/types) and the ECMA-427 character
// encoding: only A-Z a-z 0-9 . - _ ~ and ':' stay unencoded.
func TestFormatPURLFollowsTheTypeDefinitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ecosystem, name, version, want string
	}{
		// Depsilo names a Maven package "group:artifact"; the groupId is the
		// purl namespace and the artifactId its name.
		{"maven", "org.apache.xmlgraphics:batik-anim", "1.9.1", "pkg:maven/org.apache.xmlgraphics/batik-anim@1.9.1"},
		{"maven", "com.example:core", "1.0+build.7", "pkg:maven/com.example/core@1.0%2Bbuild.7"},
		{"npm", "@angular/animation", "12.3.1", "pkg:npm/%40angular/animation@12.3.1"},
		{"npm", "left-pad", "1.3.0", "pkg:npm/left-pad@1.3.0"},
		{"npm", "foo", "1.0.0+build", "pkg:npm/foo@1.0.0%2Bbuild"},
		// PyPI names are lowercased and runs of '-', '_' and '.' become '-'.
		{"pypi", "Typing_Extensions", "4.12.2", "pkg:pypi/typing-extensions@4.12.2"},
		{"pypi", "zope.interface", "6.0", "pkg:pypi/zope-interface@6.0"},
		{"pypi", "a._-b", "1", "pkg:pypi/a-b@1"},
		{"go", "github.com/gorilla/context", "v1.1.1", "pkg:golang/github.com/gorilla/context@v1.1.1"},
		{"go", "golang.org/x/net", "v0.0.0-20220225172249-27dd8689420f", "pkg:golang/golang.org/x/net@v0.0.0-20220225172249-27dd8689420f"},
		{"cargo", "rand", "0.7.2", "pkg:cargo/rand@0.7.2"},
		{"rubygems", "jruby-launcher", "1.1.2", "pkg:gem/jruby-launcher@1.1.2"},
		// Composer vendor and name are case-insensitive and lowercased.
		{"composer", "Laravel/Laravel", "5.5.0", "pkg:composer/laravel/laravel@5.5.0"},
		{"nuget", "EnterpriseLibrary.Common", "6.0.1304", "pkg:nuget/EnterpriseLibrary.Common@6.0.1304"},
		{"conda", "absl-py", "0.4.1", "pkg:conda/absl-py@0.4.1"},
		{"cran", "rJava", "1.0-4", "pkg:cran/rJava@1.0-4"},
		// Alpine packages are the apk type under the alpine vendor.
		{"alpine", "Py3-Foo", "1.2.3-r0", "pkg:apk/alpine/py3-foo@1.2.3-r0"},
		// Debian versions keep the epoch colon and encode '+'.
		{"apt", "attr", "1:2.4.47-2+b1", "pkg:deb/debian/attr@1:2.4.47-2%2Bb1"},
		{"docker", "library/nginx", "1.25", "pkg:docker/library/nginx@1.25"},
		{"docker", "smartentry/debian", "sha256:dc437cc87d10", "pkg:docker/smartentry/debian@sha256:dc437cc87d10"},
		{"huggingface", "distilbert/distilbert-base-uncased", "043235d6088ecd3dd5fb5ca3592b6913fd516027", "pkg:huggingface/distilbert/distilbert-base-uncased@043235d6088ecd3dd5fb5ca3592b6913fd516027"},
		{"helm", "nginx", "15.0.0", "pkg:helm/nginx@15.0.0"},
		// Characters outside the permitted set are percent-encoded.
		{"cargo", "name with space", "1.0", "pkg:cargo/name%20with%20space@1.0"},
		{"unknown", "thing", "", "pkg:generic/thing@unknown"},
	}
	for _, tc := range cases {
		if got := FormatPURL(tc.ecosystem, tc.name, tc.version); got != tc.want {
			t.Errorf("FormatPURL(%q, %q, %q) = %q, want %q", tc.ecosystem, tc.name, tc.version, got, tc.want)
		}
	}
}

// Normalization can give two recorded spellings of one package the same
// purl. CycloneDX requires unique bom-refs, so the second keeps a
// Depsilo-scoped reference instead of a duplicate.
func TestCycloneDXCRAKeepsBomRefsUniqueWhenPURLsCollide(t *testing.T) {
	t.Parallel()
	components := []Component{
		{Ecosystem: "pypi", Name: "Typing_Extensions", Version: "4.12.2"},
		{Ecosystem: "pypi", Name: "typing-extensions", Version: "4.12.2"},
	}
	raw, err := NewGenerator(nil).GenerateCycloneDX(sbomTestProject(), components, Options{CRA: true})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components []struct {
			PURL   string `json:"purl"`
			BomRef string `json:"bom-ref"`
		} `json:"components"`
		Dependencies []struct {
			DependsOn []string `json:"dependsOn"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Components) != 2 || doc.Components[0].PURL != "pkg:pypi/typing-extensions@4.12.2" || doc.Components[1].PURL != doc.Components[0].PURL {
		t.Fatalf("components = %+v", doc.Components)
	}
	if doc.Components[0].BomRef != doc.Components[0].PURL || doc.Components[1].BomRef == doc.Components[0].BomRef || doc.Components[1].BomRef == "" {
		t.Fatalf("bom-refs = %q, %q; want the purl then a distinct reference", doc.Components[0].BomRef, doc.Components[1].BomRef)
	}
	if got := doc.Dependencies[0].DependsOn; len(got) != 2 || got[0] != doc.Components[0].BomRef || got[1] != doc.Components[1].BomRef {
		t.Fatalf("dependsOn = %v", got)
	}
}
