package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"depsilo/internal/prompts"
)

// When the endpoint cannot be reached init-agent still writes a usable file:
// the bootstrap prompt the server would send, rendered for every ecosystem.
func TestInitAgentFallsBackToTheSharedPromptWhenTheServerIsUnreachable(t *testing.T) {
	server := newInitAgentServer(t, "", "")
	endpoint := server.URL
	server.Close()

	dir := t.TempDir()
	_, stderr, code := captureStdoutStderr(t, func() int {
		return runInitAgent([]string{"--out", dir, "--format", "agentsmd", "--endpoint", endpoint})
	})
	written, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if code != 0 || err != nil {
		t.Fatalf("exit = %d, read err = %v", code, err)
	}
	// The fallback succeeds, but not silently.
	if !strings.Contains(stderr, "could not fetch the prompt from "+endpoint) || !strings.Contains(stderr, "built into this binary") {
		t.Errorf("stderr does not say the embedded text was used: %q", stderr)
	}
	if want := strings.TrimSpace(prompts.BootstrapOffline(endpoint)); !strings.Contains(string(written), want) {
		t.Fatalf("AGENTS.md does not carry the shared offline prompt:\n%s", written)
	}
	for _, expected := range []string{
		"wait for approval",
		endpoint + "/mcp",
		"Authorization: Bearer",
		"read-only API token",
	} {
		if !strings.Contains(string(written), expected) {
			t.Errorf("fallback prompt omitted %q", expected)
		}
	}
}

// An endpoint that does not answer with a prompt gets one request, not two:
// each one can cost a full client timeout before the embedded text is used.
func TestInitAgentDoesNotProbeAServerThatGaveNoPrompt(t *testing.T) {
	var discoverRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/discover" {
			discoverRequests.Add(1)
			_, _ = w.Write([]byte(`{"service":"depsilo"}`))
			return
		}
		// Drop the connection without a response, as a dead address would.
		panic(http.ErrAbortHandler)
	}))
	t.Cleanup(server.Close)

	output, stderr, code := runInitAgentJSON(t, "--out", t.TempDir(), "--format", "agentsmd", "--endpoint", server.URL)
	if code != 0 || !*output.OK || output.Source != "embedded" || !strings.Contains(stderr, "built into this binary") {
		t.Fatalf("exit = %d, output = %+v, stderr = %q", code, output, stderr)
	}
	if got := discoverRequests.Load(); got != 0 {
		t.Fatalf("init-agent made %d discover requests after the prompt request failed, want 0", got)
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

func TestInitAgentWritesTheServerPromptBeforeAndAfterSetup(t *testing.T) {
	for name, discover := range map[string]string{
		"setup pending": `{"service":"depsilo","setup_required":true}`,
		"setup done":    `{"service":"depsilo","setup_required":false}`,
		"older server":  `{"service":"depsilo"}`,
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

type initAgentOutput struct {
	OK      *bool  `json:"ok"`
	Source  string `json:"source"`
	Error   string `json:"error"`
	Results []struct {
		Path    string `json:"path"`
		Action  string `json:"action"`
		Message string `json:"message"`
	} `json:"results"`
}

func runInitAgentJSON(t *testing.T, args ...string) (output initAgentOutput, stderr string, code int) {
	t.Helper()
	stdout, stderr, code := captureStdoutStderr(t, func() int { return runInitAgent(append(args, "--json")) })
	if err := json.Unmarshal([]byte(stdout), &output); err != nil || output.OK == nil {
		t.Fatalf("JSON output = %q (err %v)", stdout, err)
	}
	return output, stderr, code
}

// The JSON output says where the written prompt came from.
func TestInitAgentJSONReportsThePromptSource(t *testing.T) {
	server := newInitAgentServer(t, `{"service":"depsilo"}`, "PROMPT-FROM-SERVER")
	output, stderr, code := runInitAgentJSON(t, "--out", t.TempDir(), "--format", "agentsmd", "--endpoint", server.URL)
	if code != 0 || !*output.OK || output.Source != "server" || stderr != "" {
		t.Fatalf("reachable server: exit = %d, output = %+v, stderr = %q", code, output, stderr)
	}

	server.Close()
	output, stderr, code = runInitAgentJSON(t, "--out", t.TempDir(), "--format", "agentsmd", "--endpoint", server.URL)
	if code != 0 || !*output.OK || output.Source != "embedded" {
		t.Fatalf("unreachable server: exit = %d, output = %+v", code, output)
	}
	if !strings.Contains(stderr, "built into this binary") {
		t.Fatalf("unreachable server: stderr = %q", stderr)
	}
}

// A file that could not be written fails the run even when the others were
// written: ok is false, the exit code is 1 and nothing claims success.
func TestInitAgentFailsWhenAFileCannotBeWritten(t *testing.T) {
	server := newInitAgentServer(t, `{"service":"depsilo"}`, "PROMPT-FROM-SERVER")
	newProject := func() string {
		dir := t.TempDir()
		// A directory where the file belongs cannot be read or replaced,
		// whoever runs the test.
		if err := os.Mkdir(filepath.Join(dir, "CLAUDE.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	dir := newProject()
	output, _, code := runInitAgentJSON(t, "--out", dir, "--format", "all", "--endpoint", server.URL)
	if code != 1 || *output.OK {
		t.Fatalf("JSON: exit = %d, ok = %t, want 1 and false", code, *output.OK)
	}
	if !strings.Contains(output.Error, "1 of 3 files") {
		t.Errorf("JSON error = %q", output.Error)
	}
	actions := map[string]string{}
	for _, result := range output.Results {
		actions[filepath.Base(result.Path)] = result.Action
	}
	if actions["CLAUDE.md"] != "error" || actions["AGENTS.md"] != "created" || actions[".cursorrules"] != "created" {
		t.Errorf("actions = %v", actions)
	}

	dir = newProject()
	stdout, stderr, code := captureStdoutStderr(t, func() int {
		return runInitAgent([]string{"--out", dir, "--format", "all", "--endpoint", server.URL})
	})
	if code != 1 {
		t.Fatalf("text: exit = %d, want 1; stdout = %q", code, stdout)
	}
	if strings.Contains(stdout, "will now see") {
		t.Errorf("text output still claims success: %q", stdout)
	}
	if !strings.Contains(stdout, "error") || !strings.Contains(stderr, "1 of 3 files could not be updated") {
		t.Errorf("text output does not report the failure: stdout = %q, stderr = %q", stdout, stderr)
	}
}

// A start marker whose end marker is gone used to get a second block
// appended, and every later run added another. It is an error for that file,
// which is left as it was.
func TestInitAgentRefusesAStartMarkerWithoutItsEndMarker(t *testing.T) {
	server := newInitAgentServer(t, `{"service":"depsilo"}`, "PROMPT-FROM-SERVER")
	for name, target := range map[string]struct{ format, file, content string }{
		"markdown":    {"agentsmd", "AGENTS.md", "# Notes\n\n<!-- depsilo:start -->\nold text, end marker deleted\n"},
		"cursorrules": {"cursorrules", ".cursorrules", "# depsilo:start\nold text, end marker deleted\n"},
	} {
		t.Run(name, func(t *testing.T) {
			for _, dryRun := range []bool{false, true} {
				dir := t.TempDir()
				path := filepath.Join(dir, target.file)
				if err := os.WriteFile(path, []byte(target.content), 0o644); err != nil {
					t.Fatal(err)
				}
				args := []string{"--out", dir, "--format", target.format, "--endpoint", server.URL}
				if dryRun {
					args = append(args, "--dry-run")
				}
				output, _, code := runInitAgentJSON(t, args...)
				if code != 1 || *output.OK || len(output.Results) != 1 || output.Results[0].Action != "error" {
					t.Fatalf("dry-run=%t: exit = %d, output = %+v", dryRun, code, output)
				}
				if !strings.Contains(output.Results[0].Message, "without a closing") {
					t.Errorf("dry-run=%t: message = %q", dryRun, output.Results[0].Message)
				}
				if after, err := os.ReadFile(path); err != nil || string(after) != target.content {
					t.Errorf("dry-run=%t: file was changed (err %v):\n%s", dryRun, err, after)
				}
			}
		})
	}
}

// A complete block is still replaced in place, and a second run changes
// nothing.
func TestInitAgentReplacesACompleteBlockInPlace(t *testing.T) {
	server := newInitAgentServer(t, `{"service":"depsilo"}`, "PROMPT-FROM-SERVER")
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# Notes\n\n<!-- depsilo:start -->\nold text\n<!-- depsilo:end -->\n\nkept\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	args := []string{"--out", dir, "--format", "agentsmd", "--endpoint", server.URL}

	output, _, code := runInitAgentJSON(t, args...)
	if code != 0 || !*output.OK || output.Results[0].Action != "updated" {
		t.Fatalf("first run: exit = %d, output = %+v", code, output)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	if strings.Count(text, "<!-- depsilo:start -->") != 1 || strings.Count(text, "<!-- depsilo:end -->") != 1 {
		t.Fatalf("markers duplicated:\n%s", text)
	}
	if !strings.HasPrefix(text, "# Notes\n\n") || !strings.HasSuffix(text, "\n\nkept\n") || !strings.Contains(text, "PROMPT-FROM-SERVER") || strings.Contains(text, "old text") {
		t.Fatalf("block was not replaced in place:\n%s", text)
	}

	output, _, code = runInitAgentJSON(t, args...)
	if code != 0 || !*output.OK || output.Results[0].Action != "skipped" {
		t.Fatalf("second run: exit = %d, output = %+v", code, output)
	}
}
