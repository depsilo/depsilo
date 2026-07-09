package packagekey

import "testing"

func TestParseNpmFilename(t *testing.T) {
	cases := []struct {
		pkg, filename, want string
	}{
		// Plain packages.
		{"lodash", "lodash-4.17.21.tgz", "4.17.21"},
		{"react", "react-18.0.0.tgz", "18.0.0"},
		// Hyphens in package name.
		{"internal-utils", "internal-utils-1.0.0.tgz", "1.0.0"},
		// Scoped packages: the tarball uses only the unscoped basename.
		{"@scope/internal", "internal-1.0.0.tgz", "1.0.0"},
		{"@scope/internal-utils", "internal-utils-2.3.4.tgz", "2.3.4"},
		// Pre-release suffixes.
		{"react", "react-19.0.0-rc.1.tgz", "19.0.0-rc.1"},
		// Wrong extension or bad shape → empty.
		{"lodash", "lodash-4.17.21.zip", ""},
		{"lodash", "weird.tgz", ""},
		{"lodash", "", ""},
	}
	for _, c := range cases {
		if got := ParseNpmFilename(c.pkg, c.filename); got != c.want {
			t.Errorf("ParseNpmFilename(%q, %q) = %q, want %q", c.pkg, c.filename, got, c.want)
		}
	}
}

func TestParsePypiFilename(t *testing.T) {
	cases := []struct {
		filename string
		pkg, ver string
	}{
		// sdist.
		{"requests-2.32.3.tar.gz", "requests", "2.32.3"},
		{"my-package-1.0.0.tar.gz", "my-package", "1.0.0"},
		{"numpy-2.0.0.tar.gz", "numpy", "2.0.0"},
		{"package-1.0.zip", "package", "1.0"},
		// wheel.
		{"requests-2.32.3-py3-none-any.whl", "requests", "2.32.3"},
		{"numpy-2.0.0-cp312-cp312-manylinux_2_17_x86_64.whl", "numpy", "2.0.0"},
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
		// maven-metadata.xml shouldn't gate.
		{"junit/junit/4.13.2/maven-metadata.xml", "", ""},
		// jar filename whose version doesn't appear is suspect.
		{"junit/junit/4.13.2/random.jar", "", ""},
		// Too shallow.
		{"only/one.jar", "", ""},
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
		// Not a nupkg.
		{"/v3-flatcontainer/serilog/3.1.1/serilog.nuspec", "", ""},
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

func TestParseCondaPath(t *testing.T) {
	cases := []struct {
		path         string
		pkg, version string
	}{
		{"/conda-forge/noarch/numpy-2.0.0-py312_0.tar.bz2", "conda-forge/numpy", "2.0.0"},
		{"conda-forge/linux-64/pandas-2.2.0-py312h0_0.conda", "conda-forge/pandas", "2.2.0"},
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
		// Wrong extension.
		{"/src/contrib/dplyr_1.1.4.zip", "", ""},
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
		{"github.com/user/repo/@v/v1.2.3.info", "", ""}, // metadata, not artifact
		{"github.com/user/repo/@latest", "", ""},
		{"noatv.zip", "", ""},
	}
	for _, c := range cases {
		m, v := ParseGoZipPath(c.path)
		if m != c.module || v != c.version {
			t.Errorf("ParseGoZipPath(%q) = (%q, %q), want (%q, %q)", c.path, m, v, c.module, c.version)
		}
	}
}
