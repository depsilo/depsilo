package packagepolicy_test

import (
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestDebianDialectValidatesAndNormalizesPackageNames(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("apt")
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := dialect.NormalizePackageName("libc6.1+compat")
	if err != nil {
		t.Fatal(err)
	}
	if normalized != "libc6.1+compat" {
		t.Fatalf("normalized Debian package = %q", normalized)
	}
	for _, name := range []string{"a", "-pkg", "pkg_name", "LibC6", "包名"} {
		if _, err := dialect.NormalizePackageName(name); err == nil {
			t.Errorf("NormalizePackageName(%q) accepted an invalid Debian package name", name)
		}
	}
}
