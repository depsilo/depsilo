package public

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/prompts"
)

// IntegrationPromptHandler serves the brand-neutral "rewrite this project to
// use a mirror" prompt that the Portal renders for users to paste into their
// coding LLM (Claude Code / Cursor / Copilot Chat).
//
// Distinct from DiscoverHandler.AgentPrompt — that prompt configures a
// developer's local machine (`pip config set`, `npm config set`); this one
// rewrites a project's BUILD-time files (Dockerfile / CI / build scripts) and
// is intentionally brand-neutral so the URL never appears in the user's
// committed source.
type IntegrationPromptHandler struct{}

func NewIntegrationPromptHandler() *IntegrationPromptHandler {
	return &IntegrationPromptHandler{}
}

// Get returns the rendered prompt as plain text. The {{MIRROR_URL}}
// placeholder is substituted with the request's own scheme+host so the user
// gets a prompt that already references the URL they're looking at.
//
// Optional query param ?url=<override> lets the user pin a specific URL (e.g.
// the internal LAN address when the Portal is reached via a public DNS name).
func (h *IntegrationPromptHandler) Get(c *gin.Context) {
	base := c.Query("url")
	if base == "" {
		scheme := "http"
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		base = fmt.Sprintf("%s://%s", scheme, c.Request.Host)
	}

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.String(http.StatusOK, prompts.Integration(base))
}
