package cli

import (
	"encoding/json"
	"fmt"
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

// newFakeDepsilo serves the given JSON bodies with HTTP 200 and answers every
// other path with 404, like a server that predates the endpoint. It points
// DEPSILO_URL at itself and clears the token unless a test sets one later.
func newFakeDepsilo(t *testing.T, routes map[string]string) *httptest.Server {
	t.Helper()
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(service.Close)
	t.Setenv("DEPSILO_URL", service.URL)
	t.Setenv("DEPSILO_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	return service
}

type doctorReport struct {
	OK            bool  `json:"ok"`
	SetupRequired *bool `json:"setup_required"`
	Checks        []struct {
		Name    string `json:"name"`
		Level   string `json:"level"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"checks"`
}

func runDoctorJSON(t *testing.T) (doctorReport, string, int) {
	t.Helper()
	output, exitCode := captureCLIStdout(t, func() int {
		return Run("doctor", []string{"--json"})
	})
	var report doctorReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		t.Fatalf("decode doctor JSON: %v; output=%s", err, output)
	}
	if report.SetupRequired == nil {
		t.Fatalf("doctor JSON omitted setup_required; output=%s", output)
	}
	return report, output, exitCode
}

func TestDoctorFailsWhileInitialSetupIsPending(t *testing.T) {
	service := newFakeDepsilo(t, map[string]string{
		"/health":              `{"status":"healthy","version":"dev","uptime":"1m"}`,
		"/ready":               `{"status":"ready","checks":{"database":"ready","storage":"ready"}}`,
		"/api/v1/stats":        `{"service":{"uptime_seconds":7200},"upstreams":[]}`,
		"/api/v1/setup/status": `{"needs_setup":true,"token_required":true,"listen_port":23333}`,
	})

	report, output, exitCode := runDoctorJSON(t)
	if exitCode != 1 || report.OK || !*report.SetupRequired {
		t.Fatalf("doctor treated a server that waits for setup as usable: exit=%d output=%s", exitCode, output)
	}
	found := false
	for _, check := range report.Checks {
		if check.Name == "Initial setup" {
			found = true
			if check.Level != "fail" || !strings.Contains(check.Message, "not finished") {
				t.Fatalf("initial setup check = %+v, want a failure saying setup is not finished", check)
			}
			if !strings.Contains(check.Hint, "Portal") || !strings.Contains(check.Hint, service.URL) {
				t.Fatalf("initial setup hint = %q, want it to send an operator to the Portal at the service URL", check.Hint)
			}
		}
		// The stats report two hours of uptime, so the hit-rate check would
		// otherwise suggest configuring a client.
		if strings.Contains(check.Hint, "add upstreams") || strings.Contains(check.Hint, "depsilo activate") {
			t.Fatalf("doctor advised adding upstreams or clients before setup finished: %+v", check)
		}
	}
	if !found {
		t.Fatalf("doctor JSON omitted the Initial setup check; output=%s", output)
	}
	// An agent reading this output must be told that a human acts, not how
	// to obtain the bootstrap token.
	for _, recipe := range []string{"bootstrap", "setup token", "docker", "logs"} {
		if strings.Contains(strings.ToLower(output), recipe) {
			t.Fatalf("doctor output names %q while setup is pending; output=%s", recipe, output)
		}
	}

	text, exitCode := captureCLIStdout(t, func() int { return Run("doctor", nil) })
	if exitCode != 1 || !strings.Contains(text, "Initial setup") || strings.Contains(text, "add upstreams") {
		t.Fatalf("doctor text output did not report pending setup: exit=%d output=%s", exitCode, text)
	}
}

func TestDoctorToleratesMissingOrBrokenSetupStatus(t *testing.T) {
	for name, setupStatus := range map[string]string{
		"endpoint missing": "",
		"setup complete":   `{"needs_setup":false,"token_required":false}`,
		"not JSON":         `<!doctype html><html></html>`,
	} {
		t.Run(name, func(t *testing.T) {
			routes := map[string]string{
				"/health":       `{"status":"healthy","version":"dev","uptime":"1m"}`,
				"/ready":        `{"status":"ready","checks":{"database":"ready","storage":"ready"}}`,
				"/api/v1/stats": `{"service":{"uptime_seconds":7200},"upstreams":[]}`,
			}
			if setupStatus != "" {
				routes["/api/v1/setup/status"] = setupStatus
			}
			newFakeDepsilo(t, routes)

			report, output, exitCode := runDoctorJSON(t)
			if exitCode != 0 || !report.OK || *report.SetupRequired {
				t.Fatalf("doctor failed a server whose setup is not pending: exit=%d output=%s", exitCode, output)
			}
			if !strings.Contains(output, "add upstreams") || !strings.Contains(output, "depsilo activate") {
				t.Fatalf("doctor dropped the upstream or client suggestion although setup is not pending; output=%s", output)
			}
		})
	}
}

// The Portal and "depsilo status" call a healthy upstream degraded once it
// averages a full second. Doctor must draw the line at the same place, or it
// warns about overseas registries that every other surface shows as healthy.
func TestDoctorCallsAnUpstreamSlowAtTheSharedOneSecondLine(t *testing.T) {
	for _, test := range []struct {
		name        string
		latencyMs   int
		wantLevel   string
		wantMessage string
	}{
		{name: "healthy overseas registry", latencyMs: 485, wantLevel: "ok", wantMessage: ""},
		{name: "just under the line", latencyMs: 999, wantLevel: "ok", wantMessage: ""},
		{name: "at the line", latencyMs: 1000, wantLevel: "warn", wantMessage: "1 slow: pypi/official (1000ms)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			newFakeDepsilo(t, map[string]string{
				"/health":              `{"status":"ok"}`,
				"/ready":               `{"status":"ready"}`,
				"/api/v1/setup/status": `{"needs_setup":false,"token_required":false}`,
				"/api/v1/stats": fmt.Sprintf(`{"service":{"uptime_seconds":7200},"upstreams":[`+
					`{"name":"official","adapter":"pypi","healthy":true,"avg_latency_ms":%d},`+
					`{"name":"mirror","adapter":"npm","healthy":true,"avg_latency_ms":40}]}`, test.latencyMs),
			})
			report, output, _ := runDoctorJSON(t)
			for _, check := range report.Checks {
				if check.Name != "Upstream health" {
					continue
				}
				if check.Level != test.wantLevel || !strings.Contains(check.Message, test.wantMessage) {
					t.Fatalf("upstream health = %s %q, want %s containing %q", check.Level, check.Message, test.wantLevel, test.wantMessage)
				}
				return
			}
			t.Fatalf("doctor reported no upstream health check; output=%s", output)
		})
	}
}

func TestDoctorExplainsUpstreamFailures(t *testing.T) {
	const adminToken = "doctor-admin-token"
	const fakeIPUpstream = `{"name":"%s","adapter_type":"pypi","url":"https://pypi.org","healthy":false,` +
		`"last_failure":{"code":"egress_fake_ip","address":"198.18.0.12","range":"198.18.0.0/15"}}`
	adminList := `{"total":5,"items":[` +
		strings.ReplaceAll(fakeIPUpstream, "%s", "pypi-official") + "," +
		strings.ReplaceAll(fakeIPUpstream, "%s", "pypi-mirror") + "," +
		`{"name":"npm-official","adapter_type":"npm","healthy":false,"last_failure":{"code":"timeout"}},` +
		`{"name":"odd","adapter_type":"npm","healthy":false,"last_failure":{"code":"dial 203.0.113.7:443"}},` +
		`{"name":"go-official","adapter_type":"go","healthy":true,"last_failure":null}]}`
	stats := `{"service":{"uptime_seconds":60},"upstreams":[` +
		`{"name":"pypi-official","adapter":"pypi","healthy":false},` +
		`{"name":"pypi-mirror","adapter":"pypi","healthy":false},` +
		`{"name":"npm-official","adapter":"npm","healthy":false},` +
		`{"name":"odd","adapter":"npm","healthy":false},` +
		`{"name":"go-official","adapter":"go","healthy":true}]}`

	for _, test := range []struct {
		name        string
		token       string
		adminList   string
		wantMessage string
		wantHint    []string
		rejectHint  []string
	}{
		{
			name:        "token names the fake-IP case",
			token:       adminToken,
			adminList:   adminList,
			wantMessage: "failure codes: egress_fake_ip x2, other x1, timeout x1",
			// A container operator sets the service's environment rather than
			// a file inside the volume, so both forms are named. Each cause
			// that the code records is named with its remedy.
			wantHint: []string{"egress_fake_ip means DNS answered with fake-IP addresses, as proxy software in fake-IP mode does, " +
				"and Depsilo could not connect to them: nothing carried its connections, [egress] allow_fake_ip is off, " +
				"or the answer mixed fake-IP and other addresses or held a unique-local IPv6 fake-IP address, which Depsilo always refuses. " +
				"Turn on the proxy software's TUN mode, or set upstream_proxy under [egress] in config.toml, " +
				"or DEPSILO_EGRESS_UPSTREAM_PROXY in the service's environment, or check allow_fake_ip; " +
				"for unique-local IPv6 fake-IP addresses, set the proxy software's IPv6 fake-IP range to 2001:2::/48; " +
				"see docs/deployment.md#upstream-network-egress",
				"open /admin/upstreams"},
		},
		{
			name:        "token without a fake-IP failure",
			token:       adminToken,
			adminList:   `{"total":1,"items":[{"name":"npm-official","adapter_type":"npm","healthy":false,"last_failure":{"code":"dns"}}]}`,
			wantMessage: "failure codes: dns x1",
			wantHint:    []string{"open /admin/upstreams or verify network reachability"},
			rejectHint:  []string{"fake-IP", "upstream_proxy", "DEPSILO_EGRESS_UPSTREAM_PROXY"},
		},
		{
			// An upstream can be unhealthy with no failed check on record:
			// an open circuit breaker, or state restored after a restart.
			// The token worked, so the hint must not send the operator to it.
			name:  "token accepted but no failure is recorded",
			token: adminToken,
			adminList: `{"total":2,"items":[{"name":"pypi-official","adapter_type":"pypi","healthy":false,"last_failure":null},` +
				`{"name":"npm-official","adapter_type":"npm","healthy":false}]}`,
			wantHint:   []string{"open /admin/upstreams or verify network reachability", "no failure reason on record"},
			rejectHint: []string{"DEPSILO_TOKEN", "token", "fake-IP"},
		},
		{
			name:      "no token",
			adminList: adminList,
			wantHint: []string{"open /admin/upstreams or verify network reachability",
				"If proxy software in fake-IP mode answers this host's DNS while its TUN mode is off", "docs/deployment.md#upstream-network-egress", "DEPSILO_TOKEN"},
			rejectHint: []string{"egress_fake_ip"},
		},
		{
			name:       "rejected token",
			token:      "expired",
			adminList:  adminList,
			wantHint:   []string{"open /admin/upstreams or verify network reachability", "fake-IP", "docs/deployment.md#upstream-network-egress", "DEPSILO_TOKEN"},
			rejectHint: []string{"egress_fake_ip"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/health":
					_, _ = io.WriteString(w, `{"status":"healthy","version":"dev","uptime":"1m"}`)
				case "/ready":
					_, _ = io.WriteString(w, `{"status":"ready","checks":{"database":"ready"}}`)
				case "/api/v1/setup/status":
					_, _ = io.WriteString(w, `{"needs_setup":false,"token_required":false}`)
				case "/api/v1/stats":
					_, _ = io.WriteString(w, stats)
				case "/api/v1/admin/upstreams":
					if r.Header.Get("Authorization") != "Bearer "+adminToken {
						w.WriteHeader(http.StatusUnauthorized)
						_, _ = io.WriteString(w, `{"code":"UNAUTHORIZED","message":"authentication required"}`)
						return
					}
					_, _ = io.WriteString(w, test.adminList)
				default:
					http.NotFound(w, r)
				}
			}))
			defer service.Close()
			t.Setenv("DEPSILO_URL", service.URL)
			t.Setenv("DEPSILO_TOKEN", test.token)
			t.Setenv("HOME", t.TempDir())

			report, output, exitCode := runDoctorJSON(t)
			if exitCode != 1 || report.OK {
				t.Fatalf("doctor passed with failed upstreams: exit=%d output=%s", exitCode, output)
			}
			// Codes and counts only: the address and range the Admin API
			// returns, and any free text in the code field, stay out.
			for _, leaked := range []string{"198.18", "203.0.113"} {
				if strings.Contains(output, leaked) {
					t.Fatalf("doctor output contains an address (%s); output=%s", leaked, output)
				}
			}
			for _, check := range report.Checks {
				if check.Name != "Upstream health" {
					continue
				}
				if check.Level != "fail" || !strings.Contains(check.Message, "4/5 failed") {
					t.Fatalf("upstream check = %+v, want 4/5 failed", check)
				}
				if strings.Contains(check.Message, "failure codes") != (test.wantMessage != "") ||
					!strings.Contains(check.Message, test.wantMessage) {
					t.Fatalf("upstream message = %q, want failure-code summary %q", check.Message, test.wantMessage)
				}
				for _, want := range test.wantHint {
					if !strings.Contains(check.Hint, want) {
						t.Fatalf("upstream hint = %q, want it to contain %q", check.Hint, want)
					}
				}
				for _, reject := range test.rejectHint {
					if strings.Contains(check.Hint, reject) {
						t.Fatalf("upstream hint = %q, must not contain %q", check.Hint, reject)
					}
				}
				return
			}
			t.Fatalf("doctor JSON omitted the Upstream health check; output=%s", output)
		})
	}
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
