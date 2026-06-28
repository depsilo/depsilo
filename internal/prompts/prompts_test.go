package prompts_test

import (
	"strings"
	"testing"

	"depsilo/internal/prompts"
)

func TestIntegration_SubstitutesMirrorURL(t *testing.T) {
	out := prompts.Integration("http://10.4.20.52:23333")
	if strings.Contains(out, "{{MIRROR_URL}}") {
		t.Errorf("placeholder still present after rendering")
	}
	if !strings.Contains(out, "http://10.4.20.52:23333") {
		t.Errorf("rendered prompt missing baseURL after substitution")
	}
}

func TestIntegration_StripsTrailingSlash(t *testing.T) {
	out := prompts.Integration("http://example.local/")
	// After trimming, no double-slash should appear right after the URL.
	if strings.Contains(out, "http://example.local//") {
		t.Errorf("trailing slash on baseURL leaked into the output (double slash present)")
	}
	if !strings.Contains(out, "http://example.local") {
		t.Errorf("baseURL missing from rendered output")
	}
}

func TestIntegrationTemplate_HasMandatorySections(t *testing.T) {
	tmpl := prompts.IntegrationTemplate()
	// Sanity guards: if any of these phrases get accidentally removed during
	// future edits, the prompt loses load-bearing safety constraints.
	mustContain := []string{
		// Transparency-by-default discipline. "Identification" replaced the
		// previous "Brand discipline" (stealth) section in T0 #1 — see
		// docs/adr/0003-supply-chain-control-point.md. If a future edit
		// reintroduces brand-neutrality / stealth language this assertion
		// must fail loudly.
		"Identification",
		"Depsilo",
		// Public-registry fallback is non-negotiable — a self-hosted
		// control point that brings the build down when it's offline is
		// worse than no control point.
		"Public-registry fallback",
		"Discover before editing",
		"Hard constraints",
		"Never",
		"HTTP_PROXY",
		"lockfile",
	}
	for _, s := range mustContain {
		if !strings.Contains(tmpl, s) {
			t.Errorf("integration prompt missing section/phrase %q", s)
		}
	}
}

// TestIntegrationTemplate_NoStealthLanguage guards against accidental
// reintroduction of the brand-neutral / stealth framing the prompt used to
// have. A security-positioned tool must not instruct LLMs to hide what it
// changed, so any of these phrases creeping back in is a regression.
func TestIntegrationTemplate_NoStealthLanguage(t *testing.T) {
	tmpl := prompts.IntegrationTemplate()
	mustNotContain := []string{
		"Brand discipline",
		"brand-neutral",
		"no longer depends on direct public CDN",
		"opaque internal address",
		"Do not write the mirror's product name",
	}
	for _, s := range mustNotContain {
		if strings.Contains(tmpl, s) {
			t.Errorf("stealth-language regression: prompt contains forbidden phrase %q", s)
		}
	}
}

// TestIntegration_StripsMaintainerHeader asserts that the doc-comment block
// above the `---` separator (intended for repo maintainers) does NOT leak into
// the rendered LLM-facing payload.
func TestIntegration_StripsMaintainerHeader(t *testing.T) {
	out := prompts.Integration("http://example.local")
	for _, phrase := range []string{
		"This file is the canonical prompt",
		"Placeholders are",
		"Edits here ship to every user",
	} {
		if strings.Contains(out, phrase) {
			t.Errorf("maintainer doc-comment leaked into rendered prompt: contains %q", phrase)
		}
	}
	// Real content from below the separator should still be present.
	if !strings.Contains(out, "You are integrating **Depsilo**") {
		t.Errorf("rendered prompt missing opening instruction line")
	}
}
