package packagekey

import "testing"

func TestExtractNameDoesNotGuessAPTPackageFromRepositoryMetadata(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"apt/debian/debian/dists/bookworm/InRelease",
		"apt/debian/debian/dists/bookworm/main/binary-amd64/Packages.gz",
		"apt/debian/debian/pool/main/o/openssl/openssl_3.0.20-1~deb12u2.dsc",
	} {
		if got := ExtractName("apt", key); got != "" {
			t.Errorf("ExtractName(apt, %q) = %q, want empty metadata identity", key, got)
		}
	}
}

func TestExtractNameUsesOnlyStrictCRANArtifactIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		key  string
		want string
	}{
		{key: "cran/src/contrib/dplyr_1.1.4.tar.gz", want: "dplyr"},
		{key: "cran/bin/windows/contrib/4.5/dplyr_1.1.4.zip", want: "dplyr"},
		{key: "cran/bin/macosx/big-sur-arm64/contrib/4.5/dplyr_1.1.4.tgz", want: "dplyr"},
		{key: "cran/src/contrib/PACKAGES"},
		{key: "cran/src/contrib/PACKAGES.gz"},
		{key: "cran/src/contrib/PACKAGES.rds"},
		{key: "cran/bin/windows/base/R-4.6.1-win.exe"},
		{key: "cran/src/contrib/not_a_package_1.0.tar.gz"},
	} {
		if got := ExtractName("cran", test.key); got != test.want {
			t.Errorf("ExtractName(cran, %q) = %q, want %q", test.key, got, test.want)
		}
	}
}

func TestExtractNameUsesOnlyCargoArtifactIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		key  string
		want string
	}{
		{key: "cargo/crates/Serde/1.0.228.crate", want: "Serde"},
		{key: "cargo/crates/serde/1.0.228.crate", want: "serde"},
		{key: "cargo/crates/serde/index.html"},
		{key: "cargo/crates/serde/.crate"},
		{key: "cargo/crates//1.0.228.crate"},
		{key: "cargo/crates/serde/1.0.228.crate/extra"},
		{key: "crates/serde/1.0.228.crate"},
		{key: "cargo/index/se/rd/serde"},
		{key: "cargo/index//index.html"},
		{key: "cargo/config.json"},
	} {
		if got := ExtractName("cargo", test.key); got != test.want {
			t.Errorf("ExtractName(cargo, %q) = %q, want %q", test.key, got, test.want)
		}
	}
}

func TestExtractNameUsesOnlyStrictComposerIdentity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		key  string
		want string
	}{
		// Composer's per-package metadata route is the authoritative identity.
		{key: "composer/p2/monolog/monolog.json", want: "monolog/monolog"},
		{key: "composer/p2/monolog/monolog~dev.json", want: "monolog/monolog"},
		{key: "composer/p2/Symfony/Console.json", want: "Symfony/Console"},
		// The dist mirror cache key is reversible: the two coordinate segments
		// are retained while the reference and extension identify one artifact.
		{key: "composer/dist/monolog/monolog/abcdef.zip", want: "monolog/monolog"},
		{key: "composer/dist/monolog/monolog/abcdef.tar.gz", want: "monolog/monolog"},
		// Global metadata, incomplete coordinates, and look-alike paths must not
		// become package identities merely because they contain a package-like
		// token.
		{key: "composer/packages.json", want: ""},
		{key: "composer/p2/not-a-package", want: ""},
		{key: "composer/p2/not-a-package.json", want: ""},
		{key: "composer/p2/monolog/monolog.json/extra", want: ""},
		{key: "composer/p2/monolog/.json", want: ""},
		{key: "composer/p2/monolog/monolog.json.json", want: ""},
		{key: "composer/p2/monolog/monolog~dev~dev.json", want: ""},
		{key: "composer/p2/monolog/monolog/extra.json", want: ""},
		{key: "composer/p2//monolog.json", want: ""},
		{key: "composer/p2/monolog/monolog%2Fextra.json", want: ""},
		{key: "composer/dist/monolog/monolog/abcdef", want: ""},
		{key: "composer/dist/monolog/monolog/abcdef.zip/extra", want: ""},
		{key: "composer/dist/not-a-package/abcdef.zip", want: ""},
		{key: "composer/dist/monolog//abcdef.zip", want: ""},
		{key: "composer/dist/monolog/monolog/.zip", want: ""},
	} {
		test := test
		t.Run(test.key, func(t *testing.T) {
			if got := ExtractName("composer", test.key); got != test.want {
				t.Errorf("ExtractName(composer, %q) = %q, want %q", test.key, got, test.want)
			}
		})
	}
}

func TestParseComposerRequestPathUsesOnlyStrictRouteShapes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		path, wantName, wantVersion string
		wantOK                      bool
	}{
		{path: "p2/monolog/monolog.json", wantName: "monolog/monolog", wantOK: true},
		{path: "p2/monolog/monolog~dev.json", wantName: "monolog/monolog", wantOK: true},
		{path: "dist/monolog/monolog/3.8.1.0/0123456789abcdef.zip", wantName: "monolog/monolog", wantVersion: "3.8.1.0", wantOK: true},
		{path: "dist/monolog/monolog/dev-feature/x/0123456789abcdef.zip", wantName: "monolog/monolog", wantVersion: "dev-feature/x", wantOK: true},
		{path: "packages.json"},
		{path: "p2/not-a-package.json"},
		{path: "p2/monolog/monolog.json/extra"},
		{path: "dist/not-a-package//3.8.1.0/0123456789abcdef.zip"},
		{path: "dist/monolog/monolog/3.8.1.0/0123456789abcdef"},
		{path: "dist/monolog/monolog/3.8.1.0//0123456789abcdef.zip"},
		{path: "dist/monolog/monolog/../0123456789abcdef.zip"},
	} {
		test := test
		t.Run(test.path, func(t *testing.T) {
			name, version, ok := ParseComposerRequestPath(test.path)
			if name != test.wantName || version != test.wantVersion || ok != test.wantOK {
				t.Fatalf("ParseComposerRequestPath(%q) = (%q, %q, %v), want (%q, %q, %v)", test.path, name, version, ok, test.wantName, test.wantVersion, test.wantOK)
			}
		})
	}
}
