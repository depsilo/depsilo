package server

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"depsilo/internal/api"
)

type startupSummary struct {
	Version        string
	PortalURL      string
	Port           int
	SetupRequired  bool
	BootstrapToken string
	TokenGenerated bool
	// SetupReopened marks a pending setup that will replace an existing
	// config.toml, as opposed to the first run of a blank instance.
	SetupReopened bool
	// AdminEnvironmentIgnored marks a pending setup that found the headless
	// administrator variables, which do nothing without a configuration file.
	AdminEnvironmentIgnored bool
	// GeneratedAdminUsername and GeneratedAdminPassword describe an
	// administrator this start created with a random password. They are never
	// set for a password the operator supplied.
	GeneratedAdminUsername string
	GeneratedAdminPassword string
}

// reportInitialAdmin carries the headless administrator outcome into the
// console summary, so neither notice is left only in the structured log.
func (summary *startupSummary) reportInitialAdmin(outcome api.InitialAdminOutcome) {
	summary.AdminEnvironmentIgnored = outcome.EnvironmentIgnored
	summary.GeneratedAdminUsername = outcome.GeneratedUsername
	summary.GeneratedAdminPassword = outcome.GeneratedPassword
}

func newStartupSummary(version, bindHost string, port int, setupRequired bool, bootstrapToken string, tokenGenerated bool) startupSummary {
	return startupSummary{
		Version:        version,
		PortalURL:      startupPortalURL(bindHost, port),
		Port:           port,
		SetupRequired:  setupRequired,
		BootstrapToken: bootstrapToken,
		TokenGenerated: tokenGenerated,
	}
}

func startupPortalURL(bindHost string, port int) string {
	host := strings.Trim(strings.TrimSpace(bindHost), "[]")
	ip := net.ParseIP(host)
	if host == "" || ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
	}
	return (&url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(host, fmt.Sprintf("%d", port)),
	}).String()
}

func startupDisplayVersion(version string) string {
	if version == "" {
		return "unknown"
	}
	if version == "dev" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// writeStartupSummary emits one small, human-readable block in a single write
// so concurrent structured logs cannot split the bootstrap token from its
// instructions. It deliberately sits outside the configured zap level: this is
// first-run console output, not an error or a verbose operational event.
func writeStartupSummary(writer io.Writer, summary startupSummary) error {
	var output strings.Builder
	fmt.Fprintf(&output, "\nDepsilo %s\n\n", startupDisplayVersion(summary.Version))
	output.WriteString("✓ Server ready\n")
	output.WriteString("✓ Database ready\n")
	output.WriteString("✓ Cache ready\n\n")
	fmt.Fprintf(&output, "Portal:\n  %s\n", summary.PortalURL)
	if summary.SetupRequired {
		if summary.SetupReopened {
			output.WriteString("\nSetup reopened:\n")
			output.WriteString("  A configuration file exists, but no administrator can sign in.\n")
			output.WriteString("  Finishing setup replaces config.toml and keeps the previous file beside it as a .bak copy.\n\n")
		} else {
			output.WriteString("\nFirst-time setup:\n")
		}
		if summary.TokenGenerated {
			fmt.Fprintf(&output, "  Bootstrap token: %s\n\n", summary.BootstrapToken)
		} else {
			output.WriteString("  Bootstrap token: configured via DEPSILO_BOOTSTRAP_TOKEN\n\n")
		}
		// The bare command asks the default port; on any other port it would
		// report a different instance, so print the command that reaches this one.
		fmt.Fprintf(&output, "  Retrieve the current bootstrap token: %s\n\n", api.SetupTokenCommand(summary.Port))
		if summary.AdminEnvironmentIgnored {
			output.WriteString("  DEPSILO_ADMIN_USERNAME and DEPSILO_ADMIN_PASSWORD are ignored: they apply only\n")
			output.WriteString("  when a configuration file exists, so they created no administrator. Finish setup\n")
			output.WriteString("  in the Portal, or see docs/deployment.md#scripted-setup.\n\n")
		}
		output.WriteString("Open the Portal to finish setup.\n")
	} else if summary.GeneratedAdminPassword != "" {
		output.WriteString("\nInitial administrator:\n")
		fmt.Fprintf(&output, "  Username: %s\n", summary.GeneratedAdminUsername)
		fmt.Fprintf(&output, "  Generated password: %s\n\n", summary.GeneratedAdminPassword)
		output.WriteString("  This password is shown only this once and stays valid until you change it.\n")
		output.WriteString("  Sign in and change it.\n")
	}
	_, err := io.WriteString(writer, output.String())
	return err
}

func listenerPort(address net.Addr, fallback int) int {
	if tcpAddress, ok := address.(*net.TCPAddr); ok {
		return tcpAddress.Port
	}
	return fallback
}
