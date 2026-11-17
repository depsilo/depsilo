package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// newInitAgentServer serves the two endpoints init-agent reads.
func newInitAgentServer(t *testing.T, discover, prompt string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/discover":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(discover))
		case "/api/v1/agent-prompt":
			_, _ = w.Write([]byte(prompt))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// While first-run setup is pending the server's prompt is a notice, not
// instructions. init-agent must not write it into a project and report success.
func TestInitAgentWritesNothingWhileSetupIsPending(t *testing.T) {
	server := newInitAgentServer(t, `{"service":"depsilo","setup_required":true}`, "Depsilo has not finished first-run setup.")

	for _, jsonMode := range []bool{false, true} {
		dir := t.TempDir()
		args := []string{"--out", dir, "--format", "all", "--endpoint", server.URL}
		if jsonMode {
			args = append(args, "--json")
		}
		stdout, stderr, code := captureStdoutStderr(t, func() int { return runInitAgent(args) })
		if code != 1 {
			t.Fatalf("json=%t: exit = %d, want 1; stdout = %q", jsonMode, code, stdout)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("json=%t: wrote %v (err %v), want nothing", jsonMode, entries, err)
		}
		message := stderr
		if jsonMode {
			var result struct {
				OK    *bool  `json:"ok"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.OK == nil || *result.OK {
				t.Fatalf("JSON result = %q (err %v), want ok=false", stdout, err)
			}
			message = result.Error
		}
		if !strings.Contains(message, "has not finished first-run setup") || !strings.Contains(message, "Portal at "+server.URL+"/") {
			t.Fatalf("json=%t: message = %q", jsonMode, message)
		}
	}
}

func TestInitAgentWritesTheServerPromptOnceSetupIsDone(t *testing.T) {
	for name, discover := range map[string]string{
		"setup done":   `{"service":"depsilo","setup_required":false}`,
		"older server": `{"service":"depsilo"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := newInitAgentServer(t, discover, "PROMPT-FROM-SERVER")
			dir := t.TempDir()
			_, stderr, code := captureStdoutStderr(t, func() int {
				return runInitAgent([]string{"--out", dir, "--format", "agentsmd", "--endpoint", server.URL})
			})
			written, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
			if code != 0 || err != nil || !strings.Contains(string(written), "PROMPT-FROM-SERVER") {
				t.Fatalf("exit = %d, stderr = %q, read err = %v, AGENTS.md = %q", code, stderr, err, written)
			}
		})
	}
}
