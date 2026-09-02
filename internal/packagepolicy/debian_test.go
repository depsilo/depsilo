package packagepolicy_test

import (
	"testing"

	"depsilo/internal/packagepolicy"
)

func TestAPTDialectRejectsMalformedEpochAndRevisionBoundaries(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("apt")
	if err != nil {
		t.Fatal(err)
	}

	invalid := []string{
		// An epoch is separated from the upstream version by exactly one
		// colon. A second colon must not be treated as upstream text.
		"1:2:bad",
		"1:2::bad",
		"1:2:bad-1",
		":1.0",
		"+1:1.0",
		"1.0:1",
		// Revisions are non-empty and may not contain separators or other
		// characters outside Debian's revision alphabet.
		"1.0-",
		"1.0--",
		"1.0-1:2",
		"1.0-1_2",
		"1.0-1/2",
		"1.0-1!2",
		// The upstream component is mandatory and has Debian's restricted
		// character set.
		"-1",
		"1.0_1",
		"1.0#1",
		"1.0/1",
		"1.0!1",
	}
	for _, version := range invalid {
		t.Run(version, func(t *testing.T) {
			if err := dialect.ValidateVersion(version); err == nil {
				t.Fatalf("ValidateVersion(%q) accepted malformed Debian version", version)
			}
		})
	}
}

func TestAPTDialectAcceptsEpochRevisionAndTildeBoundaries(t *testing.T) {
	dialect, err := packagepolicy.DialectFor("apt")
	if err != nil {
		t.Fatal(err)
	}

	for _, version := range []string{
		"0",
		"000:1.0",
		"1.0-0",
		"1.0-foo",
		// Hyphens before the final separator belong to upstream_version.
		"1.0-1-2",
		"1.0-~",
		"1.0-+deb1",
		"1.0-1~bpo1",
	} {
		if err := dialect.ValidateVersion(version); err != nil {
			t.Errorf("ValidateVersion(%q) rejected valid Debian version: %v", version, err)
		}
	}

	comparisons := []struct {
		left, right string
		want        int
	}{
		{left: "000:1.0", right: "1.0", want: 0},
		{left: "1.0~beta1", right: "1.0", want: -1},
		{left: "1.0-1~bpo1", right: "1.0-1", want: -1},
		{left: "1.0-1", right: "1.0-2", want: -1},
		{left: "1:1.0-1", right: "2.0-1", want: 1},
		{left: "1.0-~~", right: "1.0-~", want: -1},
		{left: "1.0-~", right: "1.0", want: -1},
		{left: "1.0", right: "1.0-a", want: -1},
	}
	for _, test := range comparisons {
		got, err := dialect.CompareVersions(test.left, test.right)
		if err != nil {
			t.Errorf("CompareVersions(%q, %q): %v", test.left, test.right, err)
			continue
		}
		if debianSign(got) != test.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want sign %d", test.left, test.right, got, test.want)
		}
	}
}

func debianSign(value int) int {
	switch {
	case value < 0:
		return -1
	case value > 0:
		return 1
	default:
		return 0
	}
}
