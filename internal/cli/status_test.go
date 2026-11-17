package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTrustedHostForPlainHTTP(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{name: "plain HTTP", baseURL: "http://depsilo.local:23333", want: "depsilo.local:23333"},
		{name: "uppercase scheme", baseURL: "HTTP://depsilo.local:23333", want: "depsilo.local:23333"},
		{name: "HTTPS", baseURL: "https://depsilo.example", want: ""},
		{name: "missing scheme", baseURL: "depsilo.local:23333", want: ""},
		{name: "missing host", baseURL: "http://", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := trustedHostForPlainHTTP(tt.baseURL); got != tt.want {
				t.Errorf("trustedHostForPlainHTTP(%q) = %q, want %q", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestStatusReportsPendingInitialSetup(t *testing.T) {
	for _, test := range []struct {
		name        string
		setupStatus string
		wantPending bool
	}{
		{name: "pending", setupStatus: `{"needs_setup":true,"token_required":true}`, wantPending: true},
		{name: "complete", setupStatus: `{"needs_setup":false,"token_required":false}`},
		{name: "endpoint missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			routes := map[string]string{
				"/health":       `{"status":"healthy","version":"dev","uptime":"1m"}`,
				"/api/v1/stats": `{"service":{"uptime_seconds":60},"upstreams":[]}`,
			}
			if test.setupStatus != "" {
				routes["/api/v1/setup/status"] = test.setupStatus
			}
			service := newFakeDepsilo(t, routes)

			output, exitCode := captureCLIStdout(t, func() int { return Run("status", []string{"--json"}) })
			var report struct {
				OK            bool  `json:"ok"`
				SetupRequired *bool `json:"setup_required"`
			}
			if err := json.Unmarshal([]byte(output), &report); err != nil {
				t.Fatalf("decode status JSON: %v; output=%s", err, output)
			}
			if exitCode != 0 || !report.OK || report.SetupRequired == nil || *report.SetupRequired != test.wantPending {
				t.Fatalf("status --json: exit=%d, want setup_required=%v; output=%s", exitCode, test.wantPending, output)
			}

			text, exitCode := captureCLIStdout(t, func() int { return Run("status", nil) })
			pendingLine := "Setup:   pending; an operator must open the Portal at " + service.URL
			if exitCode != 0 || strings.Contains(text, pendingLine) != test.wantPending {
				t.Fatalf("status text: exit=%d, pending line present=%v, want %v; output=%s",
					exitCode, strings.Contains(text, pendingLine), test.wantPending, text)
			}
			if strings.Contains(strings.ToLower(text+output), "bootstrap") {
				t.Fatalf("status output names the bootstrap token; text=%s json=%s", text, output)
			}
		})
	}
}
