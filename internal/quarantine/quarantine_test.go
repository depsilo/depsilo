package quarantine

import (
	"strings"
	"testing"
	"time"

	"depsilo/internal/adapter/huggingface"
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

	// Empty configuration must never block a newly published package.
	for _, ecosystem := range []string{"pypi", "npm", "go", "cargo", "maven", "apt", "future"} {
		if got := p.Threshold(ecosystem); got != 0 {
			t.Errorf("default %s threshold = %v, want disabled", ecosystem, got)
		}
	}
	if p.IsAgeGateEnabled() {
		t.Fatal("empty configuration resolved the age gate to enabled")
	}

	if p.Mode != ModeBlock {
		t.Errorf("default mode = %v, want %v", p.Mode, ModeBlock)
	}
	if !p.FailClosed {
		t.Errorf("default fail_closed = false, want true (the safer default)")
	}
}

func TestNewPolicy_ExplicitEnableHasNoSourceBoundThresholds(t *testing.T) {
	enabled := true
	p, err := NewPolicy(Config{MinReleaseAgeEnabled: &enabled})
	if err != nil {
		t.Fatalf("NewPolicy(enabled): %v", err)
	}
	if !p.IsAgeGateEnabled() {
		t.Fatal("explicit true resolved the age gate to disabled")
	}
	for ecosystem, want := range map[string]time.Duration{
		"pypi": 0, "npm": 0, "cargo": 0, "composer": 0, "nuget": 0, "go": 0,
	} {
		if got := p.Threshold(ecosystem); got != want {
			t.Errorf("enabled %s threshold = %v, want %v", ecosystem, got, want)
		}
	}
}

func TestNewPolicy_ExplicitDisableOverridesThresholdTable(t *testing.T) {
	enabled := false
	p, err := NewPolicy(Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"npm": "7d"},
	})
	if err != nil {
		t.Fatalf("NewPolicy(disabled): %v", err)
	}
	if got := p.Threshold("npm"); got != 0 {
		t.Fatalf("disabled npm threshold = %v, want 0", got)
	}
	if p.IsAgeGateEnabled() {
		t.Fatal("explicit false resolved the age gate to enabled")
	}
}

func TestNewPolicy_LegacyPositiveThresholdRequiresSafeDisable(t *testing.T) {
	_, err := NewPolicy(Config{MinReleaseAge: map[string]string{"npm": "2d"}})
	if err == nil || !strings.Contains(err.Error(), "not supported safely") {
		t.Fatalf("NewPolicy(legacy positive threshold) error = %v, want safe startup rejection", err)
	}
}

func TestNewPolicy_Overrides(t *testing.T) {
	failClosed := false
	disabled := false
	p, err := NewPolicy(Config{
		MinReleaseAgeEnabled: &disabled,
		MinReleaseAge: map[string]string{
			"default": "1d",
			"npm":     "0",
			"cargo":   "2w",
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
	if got := p.Threshold("cargo"); got != 0 {
		t.Errorf("disabled cargo threshold = %v, want zero", got)
	}
	if p.Mode != ModeServeLastEligible {
		t.Errorf("mode = %v, want serve_last_eligible", p.Mode)
	}
	if p.FailClosed {
		t.Errorf("fail_closed = true, want false (explicit override)")
	}

	// Unsupported ecosystems remain disabled even with an explicit default.
	if got := p.Threshold("nonexistent"); got != 0 {
		t.Errorf("unknown ecosystem threshold = %v, want disabled", got)
	}
}

func TestNewPolicy_InvalidValues(t *testing.T) {
	disabled := false
	cases := []struct {
		name string
		cfg  Config
	}{
		{"bad duration", Config{MinReleaseAge: map[string]string{"npm": "forever"}}},
		{"bad duration while disabled", Config{MinReleaseAgeEnabled: &disabled, MinReleaseAge: map[string]string{"npm": "forever"}}},
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
	disabled := false
	p, err := NewPolicy(Config{
		MinReleaseAgeEnabled: &disabled,
		MinReleaseAge: map[string]string{
			"npm":   "0",
			"cargo": "3d",
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if p.Enabled("npm") {
		t.Errorf("Enabled(npm) = true, want false (threshold 0)")
	}
	if p.Enabled("cargo") {
		t.Errorf("Enabled(cargo) = true without source provenance")
	}
}

func TestAllowList_Glob(t *testing.T) {
	a, err := ParseAllowList([]string{
		"npm:@scope/internal-*",
		"npm:@Scope/*",
		"pypi:my_private.*",
		"cargo:My_Crate*",
		"go:github.com/Azure/*",
		"composer:Acme/HTTP_*",
		"conda:Conda-Forge/Num*",
		"alpine:v3.21/main/x86_64/py3-*",
		"huggingface:datasets/Acme/*",
		"docker:library/*",
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
		{"npm", "@Scope/Package", "1.0.0", true},
		{"npm", "@scope/Package", "1.0.0", false}, // npm legacy package identity preserves case
		{"pypi", "my-private-helper", "0.1", true},
		{"pypi", "My.Private_Helper", "0.1", true}, // PEP 503 name normalization
		{"pypi", "requests", "2.32.3", false},
		{"cargo", "My_CrateTools", "1.0.0", true},
		{"cargo", "my_cratetools", "1.0.0", false}, // Cargo identity preserves case
		{"go", "github.com/Azure/sdk", "v1.0.0", true},
		{"go", "github.com/azure/sdk", "v1.0.0", false},  // Go module identity preserves case
		{"go", "github.com/!azure/sdk", "v1.0.0", false}, // Policy receives the unescaped module identity
		{"composer", "acme/http_client", "1.0.0", true},
		{"conda", "Conda-Forge/NuMPy", "1.0.0", true},
		{"conda", "conda-forge/numpy", "1.0.0", false}, // Conda channel preserves case
		{"alpine", "v3.21/main/x86_64/py3-Requests", "1.0-r0", true},
		{"alpine", "v3.21/main/x86_64/Py3-Requests", "1.0-r0", false},
		{"huggingface", "datasets/acme/My_Data", "main", true},
		{"docker", "library/alpine", "latest", true},
	}
	for _, c := range cases {
		got := a.Match(c.eco, c.pkg, c.ver)
		if got != c.want {
			t.Errorf("Match(%q,%q,%q) = %v, want %v", c.eco, c.pkg, c.ver, got, c.want)
		}
	}
}

func TestHuggingFaceRequestIdentitiesReachQuarantineAllowList(t *testing.T) {
	allowList, err := ParseAllowList([]string{
		"huggingface:bert-base-*",
		"huggingface:datasets",
		"huggingface:Org/*",
		"huggingface:datasets/squad",
		"huggingface:datasets/Acme/*",
	})
	if err != nil {
		t.Fatalf("ParseAllowList: %v", err)
	}

	for _, test := range []struct {
		path string
		want bool
	}{
		{path: "/bert-base-uncased/resolve/main/config.json", want: true},
		{path: "/datasets/resolve/main/config.json", want: true}, // One-segment model named "datasets".
		{path: "/Org/Model/resolve/main/config.json", want: true},
		{path: "/datasets/squad/resolve/main/data.parquet", want: true},
		{path: "/datasets/Acme/My_Data/resolve/main/data.parquet", want: true},
		{path: "/other/model/resolve/main/config.json", want: false},
	} {
		t.Run(test.path, func(t *testing.T) {
			parsed := huggingface.ParseRequestPath(test.path)
			if parsed.Kind == huggingface.PathUnknown {
				t.Fatalf("ParseRequestPath(%q) returned PathUnknown", test.path)
			}
			if got := allowList.Match("huggingface", parsed.Repo, parsed.Ref); got != test.want {
				t.Fatalf("Match(ParseRequestPath(%q).Repo=%q) = %v, want %v", test.path, parsed.Repo, got, test.want)
			}
		})
	}
}

func TestHuggingFaceAllowListRejectsMalformedParsedRepository(t *testing.T) {
	allowList, err := ParseAllowList([]string{
		"huggingface:*",
		"huggingface:*/*",
		"huggingface:datasets/*/*",
	})
	if err != nil {
		t.Fatalf("ParseAllowList: %v", err)
	}
	parsed := huggingface.ParseRequestPath("/owner/repo--bad/resolve/main/config.json")
	if parsed.Kind == huggingface.PathUnknown {
		t.Fatal("test path did not reach the adapter's recognized request seam")
	}
	if allowList.Match("huggingface", parsed.Repo, parsed.Ref) {
		t.Fatalf("malformed parsed repository %q bypassed quarantine", parsed.Repo)
	}
}

func TestAllowList_Exact(t *testing.T) {
	a, err := ParseAllowList([]string{
		"pypi:zope_interface==1.0rc1",
		"npm:widget==1.0.0+build.1",
		"maven:org.example:Artifact==1.0",
		"go:example.com/mod==v1.2.3",
		"huggingface:Org/Model==0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("ParseAllowList: %v", err)
	}

	cases := []struct {
		eco, pkg, ver string
		want          bool
	}{
		{"pypi", "Zope.Interface", "1.0RC1", true}, // normalized name and PEP 440 version
		{"pypi", "zope-interface", "1.0", false},   // wrong version
		{"pypi", "zope-interface", "", false},      // exact needs a version
		{"pypi", "other", "1.0rc1", false},         // wrong package
		{"npm", "widget", "1.0.0+build.2", true},   // SemVer build metadata has equal precedence
		{"maven", "org.example:Artifact", "1.0", true},
		{"maven", "org.example:artifact", "1.0", false},        // Maven coordinates retain case
		{"go", "example.com/mod", "v1.2.3", true},              // exact-only dialects still allow pins
		{"huggingface", "org/model", "0123456789abcdef", true}, // Hugging Face aliases fold to one policy key
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
		"pypi:django>3.0",
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
		{"npm", "react", "18.0.0-alpha", false},  // SemVer pre-release is below the release
		{"npm", "react", "not-a-version", false}, // invalid request versions never bypass
		// lodash < 5.0.0
		{"npm", "lodash", "4.17.21", true},
		{"npm", "lodash", "5.0.0", false},
		// django > 3.0
		{"pypi", "django", "3.0.1", true},
		{"pypi", "django", "4.2", true},
		{"pypi", "django", "3.0rc1", false},
		{"pypi", "django", "3.0", false}, // boundary excluded
		{"pypi", "django", "2.2", false},
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

func TestAllowList_RejectsUnknownOrUnsafeSelectors(t *testing.T) {
	cases := []string{
		"pip:django>3.0",                  // unknown ecosystem alias
		"go:example.com/mod>=v1.2.3",      // Go policy is exact-only
		"maven:org.example:artifact>=1.2", // Maven ComparableVersion is not transitive
		"apt:demo==1.0-1",                 // .deb paths omit the Debian epoch
		"apt:demo>=1.0-1",                 // APT package policy is package-wide only
		"rubygems:nokogiri==1.16.5",       // platform suffix makes filename versions ambiguous
		"rubygems:nokogiri>=1.16.5",       // RubyGems package policy is package-wide only
		"composer:vendor/package==1.0.0",  // Composer Package Rules and quarantine use different provenance seams
		"composer:vendor/package>=1.0.0",  // no Composer quarantine version bypass without authenticated routing
		"npm:react>=18.0",                 // npm requires strict SemVer
		"cargo:bad.name*",                 // impossible Cargo package-name character
		"composer:vendor",                 // Composer identity requires vendor/package
		"conda:channel//name*",            // Conda channel paths cannot contain empty segments
		"alpine:v3.21/main/x86_64",        // Alpine identity has four path segments
		"huggingface:spaces/owner/*",      // only owner/repo and datasets/owner/repo are policy identities
		"docker:Library/*",                // Docker remote names are lowercase, never silently folded
		"docker:library/../*",             // invalid Docker remote-name grammar
		"npm:react==not-a-version",
		"npm:react==*", // exact pins require a concrete version
		"npm:>=18.0.0", // missing package operand
		"npm:react>=",  // missing version operand
		"pypi:[broken-glob",
	}
	for _, rule := range cases {
		t.Run(rule, func(t *testing.T) {
			if _, err := ParseAllowList([]string{rule}); err == nil {
				t.Fatalf("ParseAllowList(%q) succeeded, want an error", rule)
			}
		})
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
