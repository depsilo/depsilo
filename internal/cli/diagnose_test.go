package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
