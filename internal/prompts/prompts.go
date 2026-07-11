// Package prompts serves the canonical "paste this into your AI coding agent"
// prompt that auto-integrates a project with the running Depsilo mirror.
//
// The prompt text is the single source of truth at integration.md (embedded).
// Both the public HTTP endpoint (GET /api/v1/integration-prompt) and the CLI
// (depsilo prompt) render it by substituting placeholders against a base URL.
//
// Distinct from internal/api/public.DiscoverHandler.AgentPrompt — that one
// configures the user's local DEVELOPER MACHINE (`pip config set`, `npm config
// set`); this one rewrites the user's PROJECT SOURCE (Dockerfile / CI / build
// scripts) and is intentionally transparent: generated edits should name
// Depsilo, include the mirror URL, and preserve an explicit public-registry
// recovery path.
package prompts

import (
	_ "embed"
	"strings"
)

//go:embed integration.md
var integrationTemplate string

// Integration returns the rendered integration prompt with {{MIRROR_URL}}
// substituted by baseURL. baseURL should NOT have a trailing slash; callers
// pass either the request's scheme+host or a CLI flag value.
//
// The embedded markdown begins with a doc-comment block aimed at
// maintainers; everything before the first horizontal rule (`---`) is
// stripped so the LLM-facing payload starts cleanly at the first
// instruction line.
func Integration(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	body := integrationTemplate
	if i := strings.Index(body, "\n---\n"); i != -1 {
		body = strings.TrimLeft(body[i+len("\n---\n"):], "\n")
	}
	return strings.ReplaceAll(body, "{{MIRROR_URL}}", baseURL)
}

// IntegrationTemplate returns the raw template (no substitution). Used by
// tests and by callers that want to do their own substitution.
func IntegrationTemplate() string {
	return integrationTemplate
}
