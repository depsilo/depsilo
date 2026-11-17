package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The warmup endpoint queues a job and answers 202 Accepted; the CLI must
// report that as success and surface the job the server created.
func TestWarmupAcceptsQueuedJob(t *testing.T) {
	var request struct {
		Ecosystem string   `json:"ecosystem"`
		Packages  []string `json:"packages"`
	}
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin/cache/warmup" {
			http.NotFound(response, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusAccepted)
		_, _ = response.Write([]byte(`{"message":"warmup started","packages":2,"job_id":"0123abcd","status":"queued"}`))
	}))
	defer service.Close()
	t.Setenv("DEPSILO_URL", service.URL)
	t.Setenv("DEPSILO_TOKEN", "test-token")

	output, exitCode := captureCLIStdout(t, func() int {
		return Run("warmup", []string{"--json", "pypi", "requests", "numpy"})
	})
	if exitCode != 0 {
		t.Fatalf("warmup --json exit code = %d, want 0; output=%s", exitCode, output)
	}
	if request.Ecosystem != "pypi" || strings.Join(request.Packages, ",") != "requests,numpy" {
		t.Fatalf("warmup request = %+v, want pypi requests,numpy", request)
	}
	var report struct {
		OK        bool   `json:"ok"`
		Ecosystem string `json:"ecosystem"`
		Packages  int    `json:"packages"`
		Message   string `json:"message"`
		JobID     string `json:"job_id"`
		Status    string `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("parse warmup JSON: %v; output=%s", err, output)
	}
	if !report.OK || report.Ecosystem != "pypi" || report.Packages != 2 ||
		report.Message != "warmup started" || report.JobID != "0123abcd" || report.Status != "queued" {
		t.Fatalf("warmup JSON = %+v, want ok with the server's job fields", report)
	}

	output, exitCode = captureCLIStdout(t, func() int {
		return Run("warmup", []string{"pypi", "requests", "numpy"})
	})
	if exitCode != 0 {
		t.Fatalf("warmup exit code = %d, want 0; output=%s", exitCode, output)
	}
	if !strings.Contains(output, "Warmup started for 2 package(s) in pypi") ||
		!strings.Contains(output, "Job 0123abcd: queued") {
		t.Fatalf("warmup output omitted the job id or status; output=%s", output)
	}
}
