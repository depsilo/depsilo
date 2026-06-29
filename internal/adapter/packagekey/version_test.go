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
		path                 string
		crate, version       string
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
