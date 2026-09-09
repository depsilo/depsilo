package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"depsilo/internal/version"
)

type diagnosticHealth struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}
type diagnosticReady struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}
type diagnosticReport struct {
	Version       string           `json:"cli_version"`
	Commit        string           `json:"cli_commit"`
	BuildDate     string           `json:"cli_build_date"`
	ServerVersion string           `json:"server_version,omitempty"`
	Health        diagnosticHealth `json:"health"`
	Ready         diagnosticReady  `json:"ready"`
	Capabilities  json.RawMessage  `json:"capabilities,omitempty"`
	Omitted       []string         `json:"omitted"`
}

// runDiagnose writes a bounded, local-only report. It intentionally uses a
// whitelist of response fields and never includes config, tokens, headers,
// package names, client addresses, or arbitrary server error text.
func runDiagnose(args []string) int {
	jsonMode, args := stripJSONFlag(args)
	out := "depsilo-diagnostic.json"
	for i := 0; i < len(args); i++ {
		if args[i] != "--out" && args[i] != "-o" {
			return printDiagnoseError(jsonMode, fmt.Errorf("unknown diagnose argument %q", args[i]))
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
			return printDiagnoseError(jsonMode, fmt.Errorf("%s requires an output path", args[i]))
		}
		out = args[i+1]
		i++
	}

	report := diagnosticReport{Version: version.Version, Commit: version.Commit, BuildDate: version.BuildDate, Omitted: []string{"configuration", "environment", "credentials", "request samples", "package names", "client addresses"}}
	_, healthErr := getJSON(getServerURL()+"/health", &report.Health)
	if healthErr == nil {
		report.ServerVersion = report.Health.Version
	} else {
		report.Omitted = append(report.Omitted, "health response")
	}
	_, _ = getJSONAnyStatus(getServerURL()+"/ready", &report.Ready)
	if status, body, err := getJSONBody(getServerURL() + "/api/v1/admin/capabilities/summary"); err == nil && status == 200 {
		var envelope struct {
			Capabilities json.RawMessage `json:"capabilities"`
		}
		if json.Unmarshal(body, &envelope) == nil {
			report.Capabilities = envelope.Capabilities
		}
	} else {
		report.Omitted = append(report.Omitted, "capability response (authentication or unavailable)")
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err == nil {
		err = os.WriteFile(out, append(data, '\n'), 0600)
	}
	if err != nil {
		return printDiagnoseError(jsonMode, err)
	}
	if jsonMode {
		printJSON(map[string]any{"ok": true, "file": out, "omitted": report.Omitted})
		return 0
	}
	fmt.Printf("Diagnostic report written locally: %s\n", out)
	fmt.Println("Sensitive configuration, credentials, request samples, and package identities were omitted.")
	return 0
}

func printDiagnoseError(jsonMode bool, err error) int {
	if jsonMode {
		printJSON(map[string]any{"ok": false, "error": err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, "diagnose:", err)
	}
	return 1
}
