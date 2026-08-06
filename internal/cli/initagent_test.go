package cli

import (
	"strings"
	"testing"
)

func TestEmbeddedAgentPromptDocumentsMCPReadAuthentication(t *testing.T) {
	prompt := embeddedAgentPrompt("https://depsilo.example")
	for _, expected := range []string{
		"POST https://depsilo.example/mcp",
		"Authorization: Bearer",
		"read-only API token",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("embedded agent prompt omitted %q", expected)
		}
	}
}
