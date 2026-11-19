package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helpCommandNames returns the command names listed in the "Commands:" block
// of the top-level help: the first word of every line indented by exactly
// four spaces, up to the blank line that ends the block.
func helpCommandNames(help string) []string {
	_, block, found := strings.Cut(help, "\nCommands:\n")
	if !found {
		return nil
	}
	var names []string
	for _, line := range strings.Split(block, "\n") {
		if strings.TrimSpace(line) == "" {
			break
		}
		entry, indented := strings.CutPrefix(line, "    ")
		if !indented || strings.HasPrefix(entry, " ") {
			continue // continuation of the previous description
		}
		names = append(names, strings.Fields(entry)[0])
	}
	return names
}

func TestHelpCommandsMatchTheCommandList(t *testing.T) {
	names := helpCommandNames(helpText)
	if len(names) == 0 {
		t.Fatal("help text has no Commands block")
	}
	listed := make(map[string]bool, len(names))
	for _, name := range names {
		listed[name] = true
		if _, ok := lookupCommand(name); !ok {
			t.Errorf("help advertises %q, which the command list cannot dispatch", name)
		}
	}
	for _, c := range commands {
		if !listed[c.name] {
			t.Errorf("command %q is dispatchable but missing from the help Commands block", c.name)
		}
	}
}

func TestHelpRequestStopsAtTheArgumentTerminator(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{args: nil, want: false},
		{args: []string{"--help"}, want: true},
		{args: []string{"-h"}, want: true},
		{args: []string{"--out", "file", "--json", "-h"}, want: true},
		{args: []string{"token", "--help", "--", "x"}, want: true},
		{args: []string{"--", "--help"}, want: false},
		{args: []string{"pypi", "--", "-h"}, want: false},
		{args: []string{"--helpful", "-help", "--host", "h"}, want: false},
	} {
		if got := helpRequested(test.args); got != test.want {
			t.Errorf("helpRequested(%q) = %v, want %v", test.args, got, test.want)
		}
	}
}

// A command added to the table without a usage text still answers --help
// from its entry in the top-level help, and its handler does not run.
func TestDispatchAnswersHelpBeforeTheHandlerRuns(t *testing.T) {
	ran := false
	handler := func([]string) int { ran = true; return 7 }

	output, exitCode := captureCLIStdout(t, func() int {
		return dispatch(command{name: "prompt", run: handler}, []string{"--url", "x", "-h"})
	})
	want := "Usage:\n" +
		"    depsilo prompt [--url ...]          Print the project-integration prompt for an\n" +
		"                                    AI coding agent (Dockerfile/CI/build rewrite)\n"
	if ran || exitCode != 0 || output != want {
		t.Fatalf("help without a usage text: ran=%v exit=%d output=%q, want %q", ran, exitCode, output, want)
	}

	output, exitCode = captureCLIStdout(t, func() int {
		return dispatch(command{name: "prompt", run: handler, usage: func() { fmt.Println("own usage") }}, []string{"--help"})
	})
	if ran || exitCode != 0 || output != "own usage\n" {
		t.Fatalf("help with a usage text: ran=%v exit=%d output=%q", ran, exitCode, output)
	}

	if exitCode := dispatch(command{name: "prompt", run: handler}, []string{"--", "--help"}); !ran || exitCode != 7 {
		t.Fatalf("arguments after the terminator: ran=%v exit=%d, want the handler's 7", ran, exitCode)
	}
}

// The --json line of the help once named three commands while eleven accepted
// the flag. The expected list is kept by hand: it was read off the handlers
// that call stripJSONFlag, and changes when one gains or loses the flag.
func TestHelpListsTheCommandsThatAcceptJSON(t *testing.T) {
	_, rest, found := strings.Cut(helpText, "--json, -j")
	if !found {
		t.Fatal("help text has no --json line")
	}
	paragraph, _, _ := strings.Cut(rest, "\n\n")
	_, list, found := strings.Cut(paragraph, "Accepted by:")
	if !found {
		t.Fatalf("--json help does not say which commands accept it: %q", paragraph)
	}
	got := strings.Join(strings.Fields(list), " ")
	want := "status, doctor, diagnose, init-agent, version, warmup, flush, backup, restore, distribution, audit verify, cascade"
	if got != want {
		t.Fatalf("--json help lists %q, want %q", got, want)
	}
	for _, entry := range strings.Split(got, ", ") {
		name := strings.Fields(entry)[0]
		if _, ok := lookupCommand(name); !ok {
			t.Errorf("--json help names %q, which is not a command", name)
		}
	}
}

func TestHelpUsesProductAndBootstrapTokenWording(t *testing.T) {
	if !strings.HasPrefix(helpText, "Depsilo - self-hosted dependency cache and distribution service\n") {
		t.Errorf("help does not open with the product description: %q", strings.SplitN(helpText, "\n", 2)[0])
	}
	if !strings.Contains(helpText, "setup token [flags]         Print the running instance's bootstrap token\n") {
		t.Error("help does not describe `setup token` as printing the bootstrap token")
	}
}

// A token file created with an editor or `echo` ends in a newline. Sent
// untrimmed it is an invalid Authorization header, and every command failed
// before reaching the server.
func TestTokenWhitespaceIsTrimmedBeforeUse(t *testing.T) {
	var authorization string
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"message":"cleanup complete","deleted":0}`))
	}))
	defer service.Close()
	t.Setenv("DEPSILO_URL", service.URL)

	for _, test := range []struct {
		name, env, file string
	}{
		{name: "token file ending in a newline", file: "file-token\n"},
		{name: "environment variable with surrounding whitespace", env: " env-token\r\n", file: "file-token\n"},
		{name: "blank environment variable falls back to the file", env: " \n", file: "\tfile-token \n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			tokenPath := filepath.Join(home, ".config", "depsilo", "token")
			if err := os.MkdirAll(filepath.Dir(tokenPath), 0o700); err != nil {
				t.Fatalf("create token directory: %v", err)
			}
			if err := os.WriteFile(tokenPath, []byte(test.file), 0o600); err != nil {
				t.Fatalf("write token file: %v", err)
			}
			t.Setenv("HOME", home)
			t.Setenv("DEPSILO_TOKEN", test.env)
			want := "Bearer file-token"
			if strings.TrimSpace(test.env) != "" {
				want = "Bearer env-token"
			}

			authorization = ""
			output, exitCode := captureCLIStdout(t, func() int {
				return Run("flush", []string{"--json"})
			})
			if exitCode != 0 {
				t.Fatalf("flush exit code = %d, want 0; output=%s", exitCode, output)
			}
			if authorization != want {
				t.Fatalf("Authorization = %q, want %q", authorization, want)
			}
		})
	}
}
