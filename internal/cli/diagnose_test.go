package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnoseWritesBoundedLocalReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = io.WriteString(w, `{"status":"healthy","version":"v-test"}`)
		case "/ready":
			_, _ = io.WriteString(w, `{"status":"ready","checks":{"database":"ready"}}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	t.Setenv("DEPSILO_URL", server.URL)
	t.Setenv("DEPSILO_TOKEN", "")
	out := filepath.Join(t.TempDir(), "diagnostic.json")
	if code := Run("diagnose", []string{"--out", out, "--json"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report["health"].(map[string]any)["status"] != "healthy" {
		t.Fatalf("health = %#v", report["health"])
	}
	if _, ok := report["configuration"]; ok {
		t.Fatal("report contains configuration")
	}
}

func TestDiagnoseRedactsCapabilityTextAndRejectsUnsafeOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = io.WriteString(w, `{"status":"healthy","version":"1.2.3"}`)
		case "/ready":
			_, _ = io.WriteString(w, `{"status":"ready","checks":{"database":"ready","storage":"ready"}}`)
		case "/api/v1/admin/capabilities/summary":
			_, _ = io.WriteString(w, `{"capabilities":[{"name":"malicious_blocklist","ecosystem":"npm","support":"supported","mode":"block","data_status":"error","recent_failure":"secret-canary https://user:pass@example.invalid/private"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("DEPSILO_URL", server.URL)
	t.Setenv("DEPSILO_TOKEN", "")
	out := filepath.Join(t.TempDir(), "diagnostic.json")
	if code := Run("diagnose", []string{"--out", out}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || string(data) == "secret-canary" || containsDiagnosticSecret(string(data)) {
		t.Fatalf("diagnostic leaked source text: %s", data)
	}
	t.Setenv("DEPSILO_URL", "http://user:pass@example.invalid/private?token=secret#fragment")
	if code := Run("diagnose", []string{"--out", filepath.Join(t.TempDir(), "unsafe.json")}); code == 0 {
		t.Fatal("unsafe origin was accepted")
	}
}

func containsDiagnosticSecret(s string) bool {
	for _, secret := range []string{"secret-canary", "user:pass", "example.invalid", "private", "token=secret"} {
		if strings.Contains(s, secret) {
			return true
		}
	}
	return false
}
