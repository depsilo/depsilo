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
		"Brand discipline",
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
	if !strings.Contains(out, "You are integrating a private package mirror") {
		t.Errorf("rendered prompt missing opening instruction line")
	}
}
