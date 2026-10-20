package cli

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"depsilo/internal/config"
)

const setupTestToken = "private-bootstrap-token-0123456789abcdef0123456789"

func TestSetupTokenCLIReadsRunningInstanceWithoutCreatingSigningKey(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/api/v1/setup/status" || r.Header.Get("Authorization") != "" {
			t.Error("setup token must only request public setup status without administrator credentials")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = fmt.Fprint(w, `{"needs_setup":true,"token_required":true}`)
	}))
	defer server.Close()
	var remoteRequests atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		remoteRequests.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer remote.Close()

	for _, selection := range []string{"flag", "environment", "fresh-default"} {
		t.Run(selection, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			configPath := filepath.Join(root, "instance", "config.toml")
			if selection == "fresh-default" {
				configPath = filepath.Join(home, ".depsilo", "config.toml")
			} else {
				writeSetupCLIConfig(t, configPath, setupServerPort(t, server))
			}
			tokenPath := writeSetupCLIToken(t, configPath)
			env := []string{
				"HOME=" + home, "USERPROFILE=" + home,
				"DEPSILO_URL=" + remote.URL,
				"DEPSILO_TOKEN=must-not-be-sent",
				// Read-only retrieval must ignore this value instead of generating
				// or validating a new bootstrap token unrelated to the server.
				"DEPSILO_BOOTSTRAP_TOKEN=short",
			}
			args := []string{"setup", "token"}
			switch selection {
			case "flag":
				env = append(env, "DEPSILO_CONFIG="+filepath.Join(root, "wrong-config.toml"))
				args = append(args, "--config", configPath)
			case "environment":
				env = append(env, "DEPSILO_CONFIG="+configPath)
			case "fresh-default":
				env = append(env, "DEPSILO_SERVER_HOST=0.0.0.0", "DEPSILO_SERVER_PORT="+strconv.Itoa(setupServerPort(t, server)))
			}
			stdout, stderr, err := runSetupCLI(t, root, env, args...)
			if err != nil || stdout != setupTestToken+"\n" || stderr != "" {
				t.Fatalf("setup token did not print only the current token: error=%v, stdout length=%d, stderr=%q", err, len(stdout), stderr)
			}
			content, err := os.ReadFile(tokenPath)
			if err != nil || string(content) != setupTestToken+"\n" {
				t.Fatal("setup token changed the server's bootstrap token")
			}
			keyPath, err := config.ManagedJWTSecretPath(configPath)
			if err != nil {
				t.Fatal(err)
			}
			assertNoCLIManagedKey(t, keyPath)
			if selection == "fresh-default" {
				if _, err := os.Stat(configPath); !os.IsNotExist(err) {
					t.Fatal("read-only token retrieval created a configuration file")
				}
			}
		})
	}
	if requests.Load() != 3 || remoteRequests.Load() != 0 {
		t.Fatalf("setup token used the wrong instance: local requests=%d, remote requests=%d", requests.Load(), remoteRequests.Load())
	}
}

func TestSetupTokenCLIRejectsStaleOrUnavailableToken(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		body       string
		missing    bool
		wantError  string
	}{
		{"completed", http.StatusOK, `{"needs_setup":false,"token_required":false}`, false, "already complete"},
		{"completed-without-file", http.StatusOK, `{"needs_setup":false,"token_required":false}`, true, "already complete"},
		{"token-not-required", http.StatusOK, `{"needs_setup":true,"token_required":false}`, false, "does not require"},
		{"unreachable", 0, "", false, "cannot contact"},
		{"unavailable", http.StatusServiceUnavailable, setupTestToken, false, "HTTP 503"},
		{"invalid-status", http.StatusOK, setupTestToken, false, "invalid setup status"},
		{"missing-status-fields", http.StatusOK, `{}`, false, "invalid setup status"},
		{"missing-file", http.StatusOK, `{"needs_setup":true,"token_required":true}`, true, "cannot read the current bootstrap token"},
		{"redirect", http.StatusFound, setupTestToken, false, "HTTP 302"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var redirectRequests atomic.Int32
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				redirectRequests.Add(1)
				_, _ = fmt.Fprint(w, `{"needs_setup":true,"token_required":true}`)
			}))
			defer redirect.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.statusCode == http.StatusFound {
					w.Header().Set("Location", redirect.URL)
				}
				w.WriteHeader(test.statusCode)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			port := setupServerPort(t, server)
			if test.statusCode == 0 {
				server.Close()
			}
			root := t.TempDir()
			configPath := filepath.Join(root, "config.toml")
			writeSetupCLIConfig(t, configPath, port)
			if !test.missing {
				writeSetupCLIToken(t, configPath)
			}
			stdout, stderr, err := runSetupCLI(t, root, nil, "setup", "token", "--config", configPath)
			if err == nil || stdout != "" || !strings.Contains(stderr, test.wantError) {
				t.Fatalf("setup token did not fail safely: error=%v, stdout length=%d, stderr=%q", err, len(stdout), stderr)
			}
			if strings.Contains(stderr, setupTestToken) || redirectRequests.Load() != 0 {
				t.Fatal("setup token exposed a token in an error or followed a status redirect")
			}
		})
	}
}

func TestSetupTokenCLIHelpAndInvalidArguments(t *testing.T) {
	for _, test := range []struct {
		args    []string
		success bool
	}{
		{[]string{"setup", "--help"}, true},
		{[]string{"setup", "token", "--help"}, true},
		{[]string{"setup"}, false},
		{[]string{"setup", "unknown"}, false},
		{[]string{"setup", "token", "--config"}, false},
		{[]string{"setup", "token", "--config="}, false},
		{[]string{"setup", "token", "unexpected"}, false},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			stdout, stderr, err := runSetupCLI(t, t.TempDir(), nil, test.args...)
			if (err == nil) != test.success || stdout != "" || stderr == "" {
				t.Fatalf("unexpected argument handling: error=%v, stdout=%q, stderr=%q", err, stdout, stderr)
			}
		})
	}
}

func TestSetupTokenReadOnlyConfigHasNoGeneratedBootstrapToken(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "DEPSILO_") {
			t.Setenv(key, "")
		}
	}
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("DEPSILO_BOOTSTRAP_TOKEN", "short")
	cfg, err := config.LoadForRestore()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BootstrapToken != "" || cfg.BootstrapTokenGenerated {
		t.Fatal("read-only config resolution generated a token unrelated to the running instance")
	}
	if _, err := os.Stat(filepath.Join(root, ".depsilo")); !os.IsNotExist(err) {
		t.Fatal("read-only config resolution created instance state")
	}
}

func writeSetupCLIConfig(t *testing.T, path string, port int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("config_version = 1\n[server]\nhost = '0.0.0.0'\nport = %d\n", port)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeSetupCLIToken(t *testing.T, configPath string) string {
	t.Helper()
	path := filepath.Join(filepath.Dir(configPath), "data", "secrets", "bootstrap.token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(setupTestToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func setupServerPort(t *testing.T, server *httptest.Server) int {
	t.Helper()
	return server.Listener.Addr().(*net.TCPAddr).Port
}

func runSetupCLI(t *testing.T, directory string, env []string, args ...string) (string, string, error) {
	t.Helper()
	processArgs := append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)
	command := exec.Command(os.Args[0], processArgs...)
	command.Dir = directory
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "DEPSILO_") && !strings.HasPrefix(value, cliHelperEnv+"=") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, cliHelperEnv+"=1", "HOME="+directory, "USERPROFILE="+directory)
	command.Env = append(command.Env, env...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
