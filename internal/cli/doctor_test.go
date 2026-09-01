package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDoctorFailsWhenServiceIsNotReady(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/health":
			_, _ = io.WriteString(w, `{"status":"healthy","version":"dev","uptime":"1m"}`)
		case "/ready":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"status":"not_ready","checks":{"database":"ready","storage":"unavailable"}}`)
		case "/api/v1/admin/cache/distribution":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"code":"unauthorized","message":"authentication required"}`)
		case "/api/v1/stats":
			_, _ = io.WriteString(w, `{"service":{"uptime_seconds":60},"upstreams":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer service.Close()

	t.Setenv("DEPSILO_URL", service.URL)
	t.Setenv("DEPSILO_TOKEN", "")

	output, exitCode := captureCLIStdout(t, func() int {
		return Run("doctor", []string{"--json"})
	})
	if exitCode != 1 {
		t.Fatalf("doctor exit code = %d, want 1; output=%s", exitCode, output)
	}

	var report struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name    string `json:"name"`
			Level   string `json:"level"`
			Message string `json:"message"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("decode doctor JSON: %v; output=%s", err, output)
	}
	if report.OK {
		t.Fatal("doctor reported ok=true for a service whose storage dependency is unavailable")
	}
	for _, check := range report.Checks {
		if check.Name == "Service readiness" {
			if check.Level != "fail" {
				t.Fatalf("readiness level = %q, want fail", check.Level)
			}
			if !strings.Contains(check.Message, "storage=unavailable") {
				t.Fatalf("readiness message = %q, want storage failure detail", check.Message)
			}
			return
		}
	}
	t.Fatal("doctor JSON omitted the Service readiness check")
}

func captureCLIStdout(t *testing.T, run func() int) (string, int) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	original := os.Stdout
	os.Stdout = writer
	exitCode := run()
	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout pipe: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(output), exitCode
}
