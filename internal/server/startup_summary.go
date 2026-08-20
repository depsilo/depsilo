package server

import (
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
)

type startupSummary struct {
	Version        string
	PortalURL      string
	SetupRequired  bool
	BootstrapToken string
	TokenGenerated bool
}

func newStartupSummary(version, bindHost string, port int, setupRequired bool, bootstrapToken string, tokenGenerated bool) startupSummary {
	return startupSummary{
		Version:        version,
		PortalURL:      startupPortalURL(bindHost, port),
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
		output.WriteString("\nFirst-time setup:\n")
		if summary.TokenGenerated {
			fmt.Fprintf(&output, "  Bootstrap token: %s\n\n", summary.BootstrapToken)
		} else {
			output.WriteString("  Bootstrap token: configured via DEPSILO_BOOTSTRAP_TOKEN\n\n")
		}
		output.WriteString("Open the Portal to finish setup.\n")
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
