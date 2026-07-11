package quarantine

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	cases := []struct {
		in    string
		want  time.Duration
		isErr bool
	}{
		// Empty + zero → zero, no error.
		{"", 0, false},
		{"0", 0, false},

		// Day / week suffix — the whole reason ParseDuration exists.
		{"1d", 24 * time.Hour, false},
		{"3d", 72 * time.Hour, false},
		{"7d", 168 * time.Hour, false},
		{"1w", 168 * time.Hour, false},
		{"2w", 336 * time.Hour, false},

		// Stdlib durations still work.
		{"24h", 24 * time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"72h0m0s", 72 * time.Hour, false},

		// Whitespace tolerance.
		{" 3d ", 72 * time.Hour, false},

		// Invalid.
		{"-1d", 0, true},
		{"3", 0, true}, // no unit
		{"abc", 0, true},
		{"3days", 0, true}, // unsupported suffix
		{"-24h", 0, true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := ParseDuration(c.in)
			if c.isErr {
				if err == nil {
					t.Fatalf("expected error for %q, got nil (=%v)", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("ParseDuration(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestNewPolicy_Defaults(t *testing.T) {
	p, err := NewPolicy(Config{})
	if err != nil {
		t.Fatalf("NewPolicy(empty): %v", err)
	}

	// Locked-in defaults from DefaultThresholds() must survive an
	// empty operator config — running depsilo with NO supply_chain
	// section still gives quarantine for the major ecosystems.
	cases := []struct {
		eco  string
		want time.Duration
	}{
		{"pypi", 3 * 24 * time.Hour},
		{"npm", 7 * 24 * time.Hour},
		{"go", 0},
		{"cargo", 3 * 24 * time.Hour},
		{"maven", 3 * 24 * time.Hour},
		{"apt", 0},
	}
	for _, c := range cases {
		if got := p.Threshold(c.eco); got != c.want {
			t.Errorf("default %s threshold = %v, want %v", c.eco, got, c.want)
		}
	}

	if p.Mode != ModeBlock {
		t.Errorf("default mode = %v, want %v", p.Mode, ModeBlock)
	}
	if !p.FailClosed {
		t.Errorf("default fail_closed = false, want true (the safer default)")
	}
}

func TestNewPolicy_Overrides(t *testing.T) {
	failClosed := false
	p, err := NewPolicy(Config{
		MinReleaseAge: map[string]string{
			"default": "1d",
			"npm":     "0",  // explicitly disable
			"pypi":    "2w", // override the default
			"NPM":     "0",  // case-insensitivity (later wins after lowercase)
		},
		Mode:       "serve_last_eligible",
		FailClosed: &failClosed,
	})
	if err != nil {
		t.Fatalf("NewPolicy(override): %v", err)
	}

	if got, want := p.Default, 24*time.Hour; got != want {
		t.Errorf("default threshold = %v, want %v", got, want)
	}
	if got, want := p.Threshold("npm"), time.Duration(0); got != want {
		t.Errorf("npm threshold = %v, want %v (overridden to 0)", got, want)
	}
	if got, want := p.Threshold("pypi"), 14*24*time.Hour; got != want {
		t.Errorf("pypi threshold = %v, want %v", got, want)
	}
	if p.Mode != ModeServeLastEligible {
		t.Errorf("mode = %v, want serve_last_eligible", p.Mode)
	}
	if p.FailClosed {
		t.Errorf("fail_closed = true, want false (explicit override)")
	}

	// Unknown ecosystem falls back to the explicit default.
	if got, want := p.Threshold("nonexistent"), 24*time.Hour; got != want {
		t.Errorf("unknown ecosystem threshold = %v, want %v (default)", got, want)
	}
}

func TestNewPolicy_InvalidValues(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"bad duration", Config{MinReleaseAge: map[string]string{"npm": "forever"}}},
		{"negative duration", Config{MinReleaseAge: map[string]string{"npm": "-3d"}}},
		{"bad mode", Config{Mode: "lenient"}},
		{"bad allow rule", Config{Allow: []string{"this-has-no-colon-prefix"}}},
		{"empty ecosystem prefix", Config{Allow: []string{":requests==2.32.3"}}},
		{"empty rule body", Config{Allow: []string{"pip:"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewPolicy(c.cfg); err == nil {
				t.Fatalf("expected error for %s", c.name)
			}
		})
	}
}

func TestPolicy_Enabled(t *testing.T) {
	p, err := NewPolicy(Config{
		MinReleaseAge: map[string]string{
			"npm":  "0",
			"pypi": "3d",
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if p.Enabled("npm") {
		t.Errorf("Enabled(npm) = true, want false (threshold 0)")
	}
	if !p.Enabled("pypi") {
		t.Errorf("Enabled(pypi) = false, want true (3d)")
	}
}

func TestAllowList_Glob(t *testing.T) {
	a, err := ParseAllowList([]string{
		"npm:@scope/internal-*",
		"pypi:my-private-*",
	})
	if err != nil {
		t.Fatalf("ParseAllowList: %v", err)
	}

	cases := []struct {
		eco, pkg, ver string
		want          bool
	}{
		{"npm", "@scope/internal-utils", "1.0.0", true},
		{"npm", "@scope/internal-utils", "", true}, // glob works without version
		{"npm", "@scope/external-utils", "1.0.0", false},
		{"NPM", "@scope/internal-utils", "1.0.0", true}, // case-insensitive ecosystem
		{"pypi", "my-private-helper", "0.1", true},
		{"pypi", "requests", "2.32.3", false},
	}
	for _, c := range cases {
		got := a.Match(c.eco, c.pkg, c.ver)
		if got != c.want {
			t.Errorf("Match(%q,%q,%q) = %v, want %v", c.eco, c.pkg, c.ver, got, c.want)
		}
	}
}

func TestAllowList_Exact(t *testing.T) {
	a, _ := ParseAllowList([]string{"pypi:requests==2.32.3"})

	cases := []struct {
		eco, pkg, ver string
		want          bool
	}{
		{"pypi", "requests", "2.32.3", true},
		{"pypi", "requests", "2.32.4", false}, // wrong version
		{"pypi", "requests", "", false},       // exact needs a version
		{"pypi", "other", "2.32.3", false},    // wrong package
		{"npm", "requests", "2.32.3", false},  // wrong ecosystem
	}
	for _, c := range cases {
		got := a.Match(c.eco, c.pkg, c.ver)
		if got != c.want {
			t.Errorf("Match(%q,%q,%q) = %v, want %v", c.eco, c.pkg, c.ver, got, c.want)
		}
	}
}

func TestAllowList_Range(t *testing.T) {
	a, err := ParseAllowList([]string{
		"npm:react>=18.0.0",
		"npm:lodash<5.0.0",
		"pip:django>3.0",
	})
	if err != nil {
		t.Fatalf("ParseAllowList: %v", err)
	}

	cases := []struct {
		eco, pkg, ver string
		want          bool
	}{
		// react >= 18.0.0
		{"npm", "react", "18.0.0", true},
		{"npm", "react", "18.2.1", true},
		{"npm", "react", "19.0.0", true},
		{"npm", "react", "17.0.2", false},
		// lodash < 5.0.0
		{"npm", "lodash", "4.17.21", true},
		{"npm", "lodash", "5.0.0", false},
		// django > 3.0
		{"pip", "django", "3.0.1", true},
		{"pip", "django", "4.2", true},
		{"pip", "django", "3.0", false}, // boundary excluded
		{"pip", "django", "2.2", false},
		// Range needs a version.
		{"npm", "react", "", false},
	}
	for _, c := range cases {
		got := a.Match(c.eco, c.pkg, c.ver)
		if got != c.want {
			t.Errorf("Match(%q,%q,%q) = %v, want %v", c.eco, c.pkg, c.ver, got, c.want)
		}
	}
}

func TestAllowList_NilAndEmpty(t *testing.T) {
	// Nil receiver and empty rule set must both report no-match
	// without panicking — the checker dereferences these on every
	// call in the hot path.
	var nilList *AllowList
	if nilList.Match("npm", "react", "18") {
		t.Errorf("nil AllowList.Match should always return false")
	}
	empty, _ := ParseAllowList(nil)
	if empty.Match("npm", "react", "18") {
		t.Errorf("empty AllowList.Match should always return false")
	}
	whitespaceOnly, _ := ParseAllowList([]string{"", "  ", "\t"})
	if whitespaceOnly.Match("npm", "react", "18") {
		t.Errorf("whitespace-only AllowList should match nothing")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"2.0.0", "1.99.99", 1},
		{"v2.0.0", "1.99.99", 1},    // "v" prefix stripped
		{"10.0.0", "9.0.0", 1},      // numeric compare not lexical
		{"1.0.0-rc.1", "1.0.0", -1}, // pre-release < release
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-rc.1", "1.0.0-rc.2", -1},
		{"1.0", "1.0.0", 0}, // 1.0 == 1.0 (missing third compares zero)
	}
	for _, c := range cases {
		t.Run(c.a+" vs "+c.b, func(t *testing.T) {
			got := compareVersions(c.a, c.b)
			if got != c.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
			}
		})
	}
}
