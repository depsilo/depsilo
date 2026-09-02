package packagekey

import "testing"

func TestParsePypiFilename(t *testing.T) {
	cases := []struct {
		filename string
		pkg, ver string
	}{
		// PEP 625 sdists have exactly one normalized name/version separator.
		{"requests-2.32.3.tar.gz", "requests", "2.32.3"},
		{"friendly_bard-1.0rc1.tar.gz", "friendly_bard", "1.0rc1"},
		{"numpy-2.0.0.tar.gz", "numpy", "2.0.0"},
		// Legacy/multi-hyphen sdists and non-PEP-625 archive formats are not
		// authoritative enough for a package-policy decision.
		{"my-package-1.0.0.tar.gz", "", ""},
		{"package-1.0-1.tar.gz", "", ""},
		{"package-1.0.zip", "", ""},
		{"package-1.0.tar.bz2", "", ""},
		{"package-1.0.tar.xz", "", ""},
		{"package-1.0.tar.zst", "", ""},
		{"package-1.0.tgz", "", ""},
		{"package-1.0.egg", "", ""},
		// PEP 427 wheels, with and without a build tag.
		{"requests-2.32.3-py3-none-any.whl", "requests", "2.32.3"},
		{"numpy-2.0.0-cp312-cp312-manylinux_2_17_x86_64.whl", "numpy", "2.0.0"},
		{"demo_pkg-1.0-2-py3-none-any.whl", "demo_pkg", "1.0"},
		// A wheel cannot have a hyphenated distribution token or an arbitrary
		// number of filename fields.
		{"my-package-1.0-py3-none-any.whl", "", ""},
		{"demo-1.0-py3-none-any-extra.whl", "", ""},
		{"demo-1.0-build-py3-none-any.whl", "", ""},
		// Bad shape.
		{"weird.tar.gz", "", ""},
		{"", "", ""},
		{"unknownsuffix.txt", "", ""},
	}
	for _, c := range cases {
		gotP, gotV := ParsePypiFilename(c.filename)
		if gotP != c.pkg || gotV != c.ver {
			t.Errorf("ParsePypiFilename(%q) = (%q, %q), want (%q, %q)",
				c.filename, gotP, gotV, c.pkg, c.ver)
		}
	}
}

func TestParseCargoCratePath(t *testing.T) {
	cases := []struct {
		path           string
		crate, version string
	}{
		{"/api/v1/crates/serde/1.0.197/download", "serde", "1.0.197"},
		{"api/v1/crates/serde-json/1.0.114/download", "serde-json", "1.0.114"},
		// Wrong shape.
		{"/config.json", "", ""},
		{"/api/v1/crates/serde", "", ""},
		{"/api/v1/somethingelse/foo/1.0/download", "", ""},
	}
	for _, c := range cases {
		gotC, gotV := ParseCargoCratePath(c.path)
		if gotC != c.crate || gotV != c.version {
			t.Errorf("ParseCargoCratePath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotC, gotV, c.crate, c.version)
		}
	}
}

func TestParseRubygemsFilename(t *testing.T) {
	cases := []struct {
		filename string
		gem, ver string
	}{
		{"rails-7.1.0.gem", "rails", "7.1.0"},
		{"my-gem-name-1.0.0.gem", "my-gem-name", "1.0.0"},
		{"weird.gem", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		gotG, gotV := ParseRubygemsFilename(c.filename)
		if gotG != c.gem || gotV != c.ver {
			t.Errorf("ParseRubygemsFilename(%q) = (%q, %q), want (%q, %q)",
				c.filename, gotG, gotV, c.gem, c.ver)
		}
	}
}

func TestParseMavenPath(t *testing.T) {
	cases := []struct {
		path           string
		coord, version string
	}{
		{"/org/apache/commons/commons-lang3/3.14.0/commons-lang3-3.14.0.jar",
			"org.apache.commons:commons-lang3", "3.14.0"},
		{"junit/junit/4.13.2/junit-4.13.2.jar", "junit:junit", "4.13.2"},
		{"junit/junit/4.13.2/junit-4.13.2.pom", "junit:junit", "4.13.2"},
		{"com/android/support/appcompat-v7/28.0.0/appcompat-v7-28.0.0.aar",
			"com.android.support:appcompat-v7", "28.0.0"},
		{"com/acme/app/1.0/app-1.0.war", "com.acme:app", "1.0"},
		{"com/acme/app/1.0/app-1.0-bin.tar.gz", "com.acme:app", "1.0"},
		{"com/acme/app/1.0/app-1.0.war.sha256", "com.acme:app", "1.0"},
		{"com/acme/app/1.0-SNAPSHOT/app-1.0-20260901.010203-7.jar",
			"com.acme:app", "1.0-SNAPSHOT"},
		// maven-metadata.xml shouldn't gate.
		{"junit/junit/4.13.2/maven-metadata.xml", "", ""},
		// jar filename whose version doesn't appear is suspect.
		{"junit/junit/4.13.2/random.jar", "", ""},
		{"junit/junit/4.13.2/junit-4.13.1.jar", "", ""},
		// A filename version that merely has the directory version as a
		// prefix is also a mismatch: the repository layout binds the exact
		// version segment, not an arbitrary longer version.
		{"com/acme/app/1.0/app-1.0.0.jar", "", ""},
		// Too shallow.
		{"only/one.jar", "", ""},
		// Dot segments are not repository coordinates and must not be
		// interpreted as group, artifact, or version components.
		{"com/../app/1.0/app-1.0.jar", "", ""},
		{"com/acme/./1.0/app-1.0.jar", "", ""},
	}
	for _, c := range cases {
		gotC, gotV := ParseMavenPath(c.path)
		if gotC != c.coord || gotV != c.version {
			t.Errorf("ParseMavenPath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotC, gotV, c.coord, c.version)
		}
	}
}

func TestParseNugetPath(t *testing.T) {
	cases := []struct {
		path        string
		id, version string
	}{
		{"/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg",
			"newtonsoft.json", "13.0.3"},
		{"v3-flatcontainer/serilog/3.1.1/serilog.3.1.1.nupkg", "serilog", "3.1.1"},
		// NuGet package ids and versions are case-insensitive on the wire.
		{"v3-flatcontainer/Newtonsoft.Json/13.0.3/NEWTONSOFT.JSON.13.0.3.NUPKG",
			"Newtonsoft.Json", "13.0.3"},
		// Not a nupkg.
		{"/v3-flatcontainer/serilog/3.1.1/serilog.nuspec", "", ""},
		// The filename must bind the same id and version as the path.
		{"/v3-flatcontainer/serilog/3.1.1/other.3.1.1.nupkg", "", ""},
		{"/v3-flatcontainer/serilog/3.1.1/serilog.3.0.0.nupkg", "", ""},
		// Extra path segments are not part of the canonical flat-container route.
		{"/v3-flatcontainer/serilog/3.1.1/serilog.3.1.1.nupkg/extra", "", ""},
		{"/v3-flatcontainer//3.1.1/.3.1.1.nupkg", "", ""},
		// Wrong endpoint.
		{"/v3/registration5-semver1/x/1.0.0.json", "", ""},
	}
	for _, c := range cases {
		gotI, gotV := ParseNugetPath(c.path)
		if gotI != c.id || gotV != c.version {
			t.Errorf("ParseNugetPath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotI, gotV, c.id, c.version)
		}
	}
}

func TestExtractNugetFlatContainerIdentity(t *testing.T) {
	key := "nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg"
	if got := ExtractName("nuget", key); got != "newtonsoft.json" {
		t.Fatalf("ExtractName(nuget, %q) = %q", key, got)
	}
	if got := ExtractVersion("nuget", key); got != "13.0.3" {
		t.Fatalf("ExtractVersion(nuget, %q) = %q", key, got)
	}
}

func TestParseCondaPath(t *testing.T) {
	cases := []struct {
		path         string
		pkg, version string
	}{
		{"/conda-forge/noarch/numpy-2.0.0-py312_0.tar.bz2", "conda-forge/numpy", "2.0.0"},
		{"conda-forge/linux-64/pandas-2.2.0-py312h0_0.conda", "conda-forge/pandas", "2.2.0"},
		{"/pkgs/main/linux-64/numpy-2.0.0-py312_0.conda", "pkgs/main/numpy", "2.0.0"},
		{"/pkgs/r/linux-64/r-base-4.5.1-h123_0.tar.bz2", "pkgs/r/r-base", "4.5.1"},
		// Wrong extension.
		{"/conda-forge/noarch/numpy-2.0.0-py312_0.json", "", ""},
		// Missing pieces.
		{"/foo.tar.bz2", "", ""},
	}
	for _, c := range cases {
		gotP, gotV := ParseCondaPath(c.path)
		if gotP != c.pkg || gotV != c.version {
			t.Errorf("ParseCondaPath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotP, gotV, c.pkg, c.version)
		}
	}
}

func TestParseCranPath(t *testing.T) {
	cases := []struct {
		path         string
		pkg, version string
	}{
		{"/src/contrib/dplyr_1.1.4.tar.gz", "dplyr", "1.1.4"},
		{"src/contrib/Archive/dplyr/dplyr_1.0.10.tar.gz", "dplyr", "1.0.10"},
		{"/bin/windows/contrib/4.5/dplyr_1.1.4.zip", "dplyr", "1.1.4"},
		{"/bin/macosx/big-sur-arm64/contrib/4.5/dplyr_1.1.4.tgz", "dplyr", "1.1.4"},
		// A package-like filename outside a public CRAN artifact route is not
		// authoritative.
		{"/anything/dplyr_1.1.4.zip", "", ""},
		// Archive directories bind the same package name as the artifact.
		{"/src/contrib/Archive/dplyr/other_1.0.10.tar.gz", "", ""},
		// Multiple separators do not establish one unambiguous name/version pair.
		{"/src/contrib/not_a_package_1.0.tar.gz", "", ""},
		// Wrong extension.
		{"/src/contrib/dplyr_1.1.4.exe", "", ""},
		// No underscore.
		{"/src/contrib/badname.tar.gz", "", ""},
	}
	for _, c := range cases {
		gotP, gotV := ParseCranPath(c.path)
		if gotP != c.pkg || gotV != c.version {
			t.Errorf("ParseCranPath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotP, gotV, c.pkg, c.version)
		}
	}
}

func TestParseHelmPath(t *testing.T) {
	cases := []struct {
		path           string
		chart, version string
	}{
		{"/nginx-15.2.1.tgz", "nginx", "15.2.1"},
		{"my-app-2.0.0.tgz", "my-app", "2.0.0"},
		{"index.yaml", "", ""},
		{"weird.tgz", "", ""},
	}
	for _, c := range cases {
		gotC, gotV := ParseHelmPath(c.path)
		if gotC != c.chart || gotV != c.version {
			t.Errorf("ParseHelmPath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotC, gotV, c.chart, c.version)
		}
	}
}

func TestParseAlpinePath(t *testing.T) {
	cases := []struct {
		path         string
		pkg, version string
	}{
		{"/v3.19/main/x86_64/curl-8.5.0-r0.apk", "v3.19/main/x86_64/curl", "8.5.0-r0"},
		{"v3.19/community/aarch64/htop-3.2.2-r1.apk", "v3.19/community/aarch64/htop", "3.2.2-r1"},
		// Wrong extension.
		{"/v3.19/main/x86_64/curl-8.5.0-r0.txt", "", ""},
		// Missing -rN revision suffix → not a valid apk path.
		{"/v3.19/main/x86_64/curl-8.5.0.apk", "", ""},
		// Too few segments.
		{"/v3.19/curl-8.5.0-r0.apk", "", ""},
	}
	for _, c := range cases {
		gotP, gotV := ParseAlpinePath(c.path)
		if gotP != c.pkg || gotV != c.version {
			t.Errorf("ParseAlpinePath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotP, gotV, c.pkg, c.version)
		}
	}
}

func TestParseDockerPath(t *testing.T) {
	cases := []struct {
		path       string
		image, tag string
	}{
		{"/v2/library/alpine/manifests/3.19", "library/alpine", "3.19"},
		{"v2/owner/myimage/manifests/v1.2.3", "owner/myimage", "v1.2.3"},
		// Manifest with digest reference.
		{"/v2/library/alpine/manifests/sha256:abcd", "library/alpine", "sha256:abcd"},
		// Blob request — should not gate.
		{"/v2/library/alpine/blobs/sha256:abcd", "", ""},
		// Non-v2.
		{"/something/else", "", ""},
	}
	for _, c := range cases {
		gotI, gotT := ParseDockerPath(c.path)
		if gotI != c.image || gotT != c.tag {
			t.Errorf("ParseDockerPath(%q) = (%q, %q), want (%q, %q)",
				c.path, gotI, gotT, c.image, c.tag)
		}
	}
}

func TestParseGoZipPath(t *testing.T) {
	cases := []struct{ path, module, version string }{
		{"github.com/user/repo/@v/v1.2.3.zip", "github.com/user/repo", "v1.2.3"},
		{"github.com/!azure/azure-sdk/@v/v0.1.0.zip", "github.com/Azure/azure-sdk", "v0.1.0"},
		{"github.com/user/repo/@v/v1.0.0-!r!c1.zip", "github.com/user/repo", "v1.0.0-RC1"},
		{"example.com/module/v2/@v/v2.1.0.zip", "example.com/module/v2", "v2.1.0"},
		{"github.com/user/repo/@v/v1.2.3.info", "", ""}, // metadata, not artifact
		{"github.com/user/repo/@latest", "", ""},
		{"noatv.zip", "", ""},
		{"github.com/!1zure/sdk/@v/v1.0.0.zip", "", ""},
		{"github.com/!Azure/sdk/@v/v1.0.0.zip", "", ""},
		{"github.com/!!azure/sdk/@v/v1.0.0.zip", "", ""},
		{"github.com/azure!/sdk/@v/v1.0.0.zip", "", ""},
		{"github.com/!\u00e9zure/sdk/@v/v1.0.0.zip", "", ""},
		{"github.com/Azure/sdk/@v/v1.0.0.zip", "", ""},
		{"github.com/user/repo/@v/v1.0.0-RC1.zip", "", ""},
		{"github.com/user/repo/@v/v1.0.0-!R!c1.zip", "", ""},
		{"github.com/user/repo/@v/v1.0.0-!1rc1.zip", "", ""},
		{"example.com/module/v1/@v/v1.0.0.zip", "", ""},
	}
	for _, c := range cases {
		m, v := ParseGoZipPath(c.path)
		if m != c.module || v != c.version {
			t.Errorf("ParseGoZipPath(%q) = (%q, %q), want (%q, %q)", c.path, m, v, c.module, c.version)
		}
	}
}

func TestExtractGoModuleNameUsesCanonicalIdentity(t *testing.T) {
	for _, test := range []struct {
		key  string
		want string
	}{
		{key: "go/github.com/!azure/azure-sdk/@v/v1.0.0.zip", want: "github.com/Azure/azure-sdk"},
		{key: "go/example.com/module/v2/@v/list", want: "example.com/module/v2"},
		{key: "go/github.com/!1zure/sdk/@v/v1.0.0.zip"},
		{key: "go/github.com/!Azure/sdk/@v/list"},
		{key: "go/github.com/!!azure/sdk/@latest"},
		{key: "go/example.com/foo!/@v/v1.0.0.mod"},
		{key: "go/github.com/!\u00e9zure/sdk/@v/v1.0.0.info"},
		{key: "go/example.com/module/v1/@v/list"},
	} {
		if got := ExtractName("go", test.key); got != test.want {
			t.Errorf("ExtractName(go, %q) = %q, want %q", test.key, got, test.want)
		}
	}
}

func TestExtractGoVersionUsesCanonicalIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		key  string
		want string
	}{
		{key: "go/github.com/user/repo/@v/v1.0.0-!r!c1.zip", want: "v1.0.0-RC1"},
		{key: "go/github.com/user/repo/@v/v1.0.0-!!rc1.mod"},
		{key: "go/github.com/user/repo/@v/v1.0.0-RC1.info"},
		{key: "go/github.com/user/repo/@v/v1.0.0-!1rc1.zip"},
	} {
		if got := ExtractVersion("go", test.key); got != test.want {
			t.Errorf("ExtractVersion(go, %q) = %q, want %q", test.key, got, test.want)
		}
	}
}
