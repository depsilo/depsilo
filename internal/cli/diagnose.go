package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"depsilo/internal/ecosystem"
	"depsilo/internal/version"
)

const diagnosticLimit = 1 << 20

type diagnosticHealth struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
type diagnosticReady struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}
type diagnosticCapability struct {
	Name          string `json:"name"`
	Ecosystem     string `json:"ecosystem"`
	Support       string `json:"support"`
	Mode          string `json:"mode"`
	DataStatus    string `json:"data_status"`
	LastSuccessAt string `json:"last_success_at,omitempty"`
	RecentFailure string `json:"recent_failure,omitempty"`
}
type diagnosticReport struct {
	Version      string                 `json:"cli_version"`
	Commit       string                 `json:"cli_commit"`
	BuildDate    string                 `json:"cli_build_date"`
	Health       diagnosticHealth       `json:"health"`
	Ready        diagnosticReady        `json:"ready"`
	Capabilities []diagnosticCapability `json:"capabilities,omitempty"`
	Omitted      []string               `json:"omitted"`
}

// runDiagnose exports enumerated facts, never an upstream JSON subtree or
// free-form error. Limits cover collection time, response size and output size.
func runDiagnose(args []string) int {
	jsonMode, args := stripJSONFlag(args)
	out := "depsilo-diagnostic.json"
	for i := 0; i < len(args); i++ {
		if args[i] != "--out" && args[i] != "-o" {
			return printDiagnoseError(jsonMode, errors.New("unknown diagnose argument; use --out FILE [--json]"))
		}
		if i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
			return printDiagnoseError(jsonMode, errors.New("--out requires an output path"))
		}
		out = args[i+1]
		i++
	}
	base, err := url.Parse(getServerURL())
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" ||
		base.User != nil || (base.Path != "" && base.Path != "/") || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" {
		return printDiagnoseError(jsonMode, errors.New("DEPSILO_URL must be an HTTP(S) origin without credentials, path, query or fragment"))
	}
	// O_EXCL also rejects symlinks and hard links to existing files. Never
	// truncate an operator's file, even if it already has the default name.
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return printDiagnoseError(jsonMode, errors.New("cannot create diagnostic output; choose a new writable file"))
	}
	committed := false
	defer func() {
		_ = f.Close()
		if !committed {
			_ = os.Remove(out)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	report := collectDiagnostic(ctx, strings.TrimSuffix(base.String(), "/"))
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil || len(data) >= diagnosticLimit {
		return printDiagnoseError(jsonMode, errors.New("diagnostic report exceeds output limits"))
	}
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return printDiagnoseError(jsonMode, errors.New("diagnostic file write failed"))
	}
	committed = true
	if jsonMode {
		printJSON(map[string]any{"ok": true, "file": out, "omitted": report.Omitted})
	} else {
		fmt.Printf("Diagnostic report written locally: %s\nReview it before sharing; it is not a backup.\n", out)
	}
	return 0
}

func collectDiagnostic(ctx context.Context, origin string) diagnosticReport {
	r := diagnosticReport{
		Version: version.Version, Commit: version.Commit, BuildDate: version.BuildDate,
		Health:  diagnosticHealth{Status: "unknown", Version: "unknown"},
		Ready:   diagnosticReady{Status: "unknown"},
		Omitted: []string{"configuration", "environment", "credentials", "request samples", "package names", "client addresses", "upstream names and URLs", "free-form errors", "unrecognized fields and enum values"},
	}
	var health diagnosticHealth
	if diagnosticGET(ctx, origin+"/health", &health, false) == nil {
		r.Health = diagnosticHealth{Status: diagnosticEnum(health.Status, "healthy", "unhealthy", "failed"), Version: "unknown"}
		if diagnosticVersion.MatchString(health.Version) || health.Version == "dev" {
			r.Health.Version = health.Version
		}
	} else {
		r.Omitted = append(r.Omitted, "health response unavailable")
	}
	var ready diagnosticReady
	if diagnosticGET(ctx, origin+"/ready", &ready, true) == nil {
		r.Ready.Status = diagnosticEnum(ready.Status, "ready", "not_ready")
		r.Ready.Checks = map[string]string{}
		for _, name := range []string{"database", "storage"} {
			r.Ready.Checks[name] = diagnosticEnum(ready.Checks[name], "ready", "unavailable")
		}
	} else {
		r.Omitted = append(r.Omitted, "readiness response unavailable")
	}
	var envelope struct {
		Capabilities []diagnosticCapability `json:"capabilities"`
	}
	if diagnosticGET(ctx, origin+"/api/v1/admin/capabilities/summary", &envelope, false) != nil || envelope.Capabilities == nil || len(envelope.Capabilities) > 128 {
		r.Omitted = append(r.Omitted, "capability response unavailable or exceeds limits")
		return r
	}
	for _, c := range envelope.Capabilities {
		if diagnosticEnum(c.Name, "proxy_cache", "manual_rules", "malicious_blocklist", "vulnerability_scanning", "minimum_release_age", "tamper_alerts", "policy_snapshot") == "unknown" {
			continue
		}
		if _, known := ecosystem.Lookup(c.Ecosystem); !known && !(c.Ecosystem == "" && c.Name == "policy_snapshot") {
			continue
		}
		c.Support = diagnosticEnum(c.Support, "supported", "unsupported", "safety_disabled")
		c.Mode = diagnosticEnum(c.Mode, "off", "on", "warn", "block", "alert_only")
		c.DataStatus = diagnosticEnum(c.DataStatus, "never_synced", "fresh", "stale", "error")
		if ts, err := time.Parse(time.RFC3339Nano, c.LastSuccessAt); err == nil {
			c.LastSuccessAt = ts.UTC().Format(time.RFC3339Nano)
		} else {
			c.LastSuccessAt = ""
		}
		if c.RecentFailure != "" {
			c.RecentFailure = "source_reported_failure"
		}
		r.Capabilities = append(r.Capabilities, c)
	}
	return r
}

var diagnosticVersion = regexp.MustCompile(`^v?[0-9]{1,4}\.[0-9]{1,4}\.[0-9]{1,4}$`)

func diagnosticEnum(value string, allowed ...string) string {
	for _, candidate := range allowed {
		if value == candidate {
			return candidate
		}
	}
	return "unknown"
}

// Keep this stricter than interactive doctor: no redirect (including to the
// same host), no response/error passthrough, at most 1 MiB per endpoint.
func diagnosticGET(ctx context.Context, endpoint string, target any, readiness bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if token := getToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Transport: httpClient.Transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && !(readiness && res.StatusCode == http.StatusServiceUnavailable) {
		return errors.New("endpoint unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, diagnosticLimit+1))
	if err != nil {
		return err
	}
	if len(data) > diagnosticLimit {
		return errors.New("response exceeds limit")
	}
	return json.Unmarshal(data, target)
}

func printDiagnoseError(jsonMode bool, err error) int {
	if jsonMode {
		printJSON(map[string]any{"ok": false, "error": err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, "diagnose:", err)
	}
	return 1
}
