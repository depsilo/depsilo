package server

import (
	"bytes"
	"strings"
	"testing"

	"depsilo/internal/api"
)

func TestStartupPortalURL(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "IPv4 wildcard", host: "0.0.0.0", want: "http://127.0.0.1:23333"},
		{name: "IPv6 wildcard", host: "::", want: "http://127.0.0.1:23333"},
		{name: "bracketed IPv6 wildcard", host: "[::]", want: "http://127.0.0.1:23333"},
		{name: "empty wildcard", host: "", want: "http://127.0.0.1:23333"},
		{name: "loopback", host: "127.0.0.1", want: "http://127.0.0.1:23333"},
		{name: "hostname", host: "localhost", want: "http://localhost:23333"},
		{name: "IPv6 loopback", host: "::1", want: "http://[::1]:23333"},
		{name: "bracketed IPv6 loopback", host: "[::1]", want: "http://[::1]:23333"},
		{name: "IPv6 address", host: "2001:db8::10", want: "http://[2001:db8::10]:23333"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := startupPortalURL(test.host, 23333); got != test.want {
				t.Fatalf("startupPortalURL(%q) = %q, want %q", test.host, got, test.want)
			}
		})
	}
}

func TestListenAddress(t *testing.T) {
	for _, test := range []struct {
		name string
		host string
		want string
	}{
		{name: "IPv4 loopback", host: "127.0.0.1", want: "127.0.0.1:23333"},
		{name: "IPv4 wildcard", host: "0.0.0.0", want: "0.0.0.0:23333"},
		{name: "empty wildcard", host: "", want: ":23333"},
		{name: "hostname", host: "localhost", want: "localhost:23333"},
		{name: "IPv6 loopback", host: "::1", want: "[::1]:23333"},
		{name: "bracketed IPv6 loopback", host: "[::1]", want: "[::1]:23333"},
		{name: "IPv6 wildcard", host: "::", want: "[::]:23333"},
		{name: "bracketed IPv6 wildcard", host: "[::]", want: "[::]:23333"},
		{name: "IPv6 address", host: "2001:db8::10", want: "[2001:db8::10]:23333"},
		{name: "IPv6 address with a zone", host: "fe80::1%lo0", want: "[fe80::1%lo0]:23333"},
		{name: "surrounding space", host: " ::1 ", want: "[::1]:23333"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := listenAddress(test.host, 23333); got != test.want {
				t.Fatalf("listenAddress(%q) = %q, want %q", test.host, got, test.want)
			}
		})
	}
}

func TestWriteStartupSummaryForFirstRun(t *testing.T) {
	stubRunningInContainer(t, false)
	var output bytes.Buffer
	summary := newStartupSummary("0.9.0", "0.0.0.0", 23333, true, "bootstrap-token", true)
	if err := writeStartupSummary(&output, summary); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, fragment := range []string{
		"Depsilo v0.9.0",
		"✓ Server ready",
		"✓ Database ready",
		"✓ Cache ready",
		"Portal:\n  http://127.0.0.1:23333",
		"No administrator yet:\n  Package routes already work. Admin needs an administrator first.\n",
		"Opened on this machine, Admin creates one without the token below.",
		"Bootstrap token: bootstrap-token",
		"Retrieve the current bootstrap token: depsilo setup token\n",
		"Open http://127.0.0.1:23333/admin to create the administrator.",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("startup summary missing %q:\n%s", fragment, got)
		}
	}
	if strings.Contains(got, "Setup reopened") || strings.Contains(got, ".bak") {
		t.Errorf("first-run startup summary announced a replaced config:\n%s", got)
	}
	if strings.Contains(got, "DEPSILO_ADMIN") {
		t.Errorf("first-run startup summary mentioned administrator variables that were not set:\n%s", got)
	}
}

// "depsilo setup token" asks port 23333 unless told otherwise. On another port
// the summary must print the command that reaches this instance.
func TestWriteStartupSummaryNamesNonDefaultPortInTokenCommand(t *testing.T) {
	stubRunningInContainer(t, false)
	var output bytes.Buffer
	summary := newStartupSummary("0.9.0", "127.0.0.1", 24646, true, "bootstrap-token", true)
	if err := writeStartupSummary(&output, summary); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "  Retrieve the current bootstrap token: depsilo setup token --port 24646\n") {
		t.Errorf("startup summary omitted the port from the token command:\n%s", got)
	}
	// The release workflow reads the token from this exact line.
	if !strings.Contains(got, "  Bootstrap token: bootstrap-token\n") {
		t.Errorf("startup summary changed the bootstrap token line:\n%s", got)
	}
}

// In the container image "depsilo setup token" runs only inside the
// container, so an operator reading its output on the host is given the
// docker exec form, with the same flags.
func TestWriteStartupSummaryGivesTheContainerTokenCommandInAContainer(t *testing.T) {
	for _, test := range []struct {
		inContainer bool
		port        int
		want        string
	}{
		{true, 23333, "docker exec <container> depsilo setup token"},
		{true, 24646, "docker exec <container> depsilo setup token --port 24646"},
		{false, 23333, "depsilo setup token"},
	} {
		stubRunningInContainer(t, test.inContainer)
		var output bytes.Buffer
		summary := newStartupSummary("0.9.0", "0.0.0.0", test.port, true, "bootstrap-token", true)
		if err := writeStartupSummary(&output, summary); err != nil {
			t.Fatal(err)
		}
		if want := "  Retrieve the current bootstrap token: " + test.want + "\n"; !strings.Contains(output.String(), want) {
			t.Errorf("in container %v, port %d: startup summary missing %q:\n%s", test.inContainer, test.port, want, output.String())
		}
	}
}

// stubRunningInContainer makes the process look as if it runs, or does not
// run, in a container for the rest of the test.
func stubRunningInContainer(t *testing.T, inContainer bool) {
	t.Helper()
	previous := runningInContainer
	runningInContainer = func() bool { return inContainer }
	t.Cleanup(func() { runningInContainer = previous })
}

// "depsilo setup token" asks 127.0.0.1 unless told otherwise. A listener on
// the IPv6 loopback or on one specific address is not there, so the summary
// names the host; a wildcard listener is, and the command stays as it was.
func TestWriteStartupSummaryNamesHostInTokenCommandOnlyWhenLoopbackMissesIt(t *testing.T) {
	stubRunningInContainer(t, false)
	for _, test := range []struct {
		host string
		port int
		want string
	}{
		{"::1", 23333, "depsilo setup token --host ::1"},
		{"[::1]", 24646, "depsilo setup token --port 24646 --host ::1"},
		{"192.0.2.10", 23333, "depsilo setup token --host 192.0.2.10"},
		{"0.0.0.0", 23333, "depsilo setup token"},
		{"::", 23333, "depsilo setup token"},
		{"", 24646, "depsilo setup token --port 24646"},
		{"127.0.0.1", 23333, "depsilo setup token"},
	} {
		var output bytes.Buffer
		summary := newStartupSummary("0.9.0", test.host, test.port, true, "bootstrap-token", true)
		if err := writeStartupSummary(&output, summary); err != nil {
			t.Fatal(err)
		}
		if want := "  Retrieve the current bootstrap token: " + test.want + "\n"; !strings.Contains(output.String(), want) {
			t.Errorf("host %q port %d: startup summary missing %q:\n%s", test.host, test.port, want, output.String())
		}
	}
}

func TestWriteStartupSummaryExplainsIgnoredAdministratorEnvironment(t *testing.T) {
	var output bytes.Buffer
	summary := newStartupSummary("0.9.0", "127.0.0.1", 23333, true, "bootstrap-token", true)
	summary.reportInitialAdmin(api.InitialAdminOutcome{EnvironmentIgnored: true})
	if err := writeStartupSummary(&output, summary); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, fragment := range []string{
		"No administrator yet:",
		"DEPSILO_ADMIN_USERNAME and DEPSILO_ADMIN_PASSWORD are ignored: they apply only\n  when a configuration file exists, so they created no administrator.",
		"docs/deployment.md#scripted-setup",
		"/admin to create the administrator.",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("startup summary missing %q:\n%s", fragment, got)
		}
	}
}

func TestWriteStartupSummaryShowsGeneratedAdministratorPassword(t *testing.T) {
	var output bytes.Buffer
	summary := newStartupSummary("0.9.0", "127.0.0.1", 23333, false, "", false)
	summary.reportInitialAdmin(api.InitialAdminOutcome{GeneratedUsername: "ops", GeneratedPassword: "generated-password-value"})
	if err := writeStartupSummary(&output, summary); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, fragment := range []string{
		"Initial administrator:",
		"  Username: ops\n",
		"  Generated password: generated-password-value\n",
		"shown only this once and stays valid until you change it",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("startup summary missing %q:\n%s", fragment, got)
		}
	}
	if strings.Contains(got, "one-time") {
		t.Errorf("startup summary calls a lasting password one-time:\n%s", got)
	}
}

func TestWriteStartupSummaryForReopenedSetupWarnsThatConfigIsReplaced(t *testing.T) {
	var output bytes.Buffer
	summary := newStartupSummary("0.9.0", "127.0.0.1", 23333, true, "bootstrap-token", true)
	summary.SetupReopened = true
	if err := writeStartupSummary(&output, summary); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, fragment := range []string{
		"Setup reopened:",
		"A configuration file exists, but no administrator can sign in.",
		"Finishing setup replaces config.toml and keeps the previous file beside it as a .bak copy.",
		"Bootstrap token: bootstrap-token",
		"/admin to create the administrator.",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("reopened startup summary missing %q:\n%s", fragment, got)
		}
	}
	if strings.Contains(got, "No administrator yet") {
		t.Errorf("reopened setup was announced as a first run:\n%s", got)
	}
}

func TestWriteStartupSummaryForConfiguredServerOmitsBootstrapInstructions(t *testing.T) {
	var output bytes.Buffer
	summary := newStartupSummary("dev", "127.0.0.1", 23333, false, "must-not-appear", true)
	if err := writeStartupSummary(&output, summary); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Depsilo dev") || !strings.Contains(got, "Portal:\n  http://127.0.0.1:23333") {
		t.Fatalf("configured startup summary missing server details:\n%s", got)
	}
	for _, forbidden := range []string{"No administrator yet", "Setup reopened", "Bootstrap token", "must-not-appear", "finish setup", "depsilo setup token", "Initial administrator", "Generated password"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("configured startup summary contains %q:\n%s", forbidden, got)
		}
	}
}

func TestWriteStartupSummaryDoesNotRevealConfiguredBootstrapToken(t *testing.T) {
	var output bytes.Buffer
	summary := newStartupSummary("dev", "127.0.0.1", 23333, true, "configured-secret-must-not-appear", false)
	if err := writeStartupSummary(&output, summary); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Contains(got, "configured-secret-must-not-appear") {
		t.Fatalf("startup summary revealed configured bootstrap token:\n%s", got)
	}
	if !strings.Contains(got, "Bootstrap token: configured via DEPSILO_BOOTSTRAP_TOKEN") {
		t.Fatalf("startup summary omitted configured-token guidance:\n%s", got)
	}
}

// A browser reaches a container through its published port, not loopback, and
// a preset token is always required, so neither summary promises a token-free
// Admin.
func TestWriteStartupSummaryPromisesNoTokenFreeAdminWhereTheTokenIsRequired(t *testing.T) {
	for name, test := range map[string]struct {
		container bool
		generated bool
		host      string
	}{
		"container":    {container: true, generated: true, host: "0.0.0.0"},
		"preset token": {generated: false, host: "127.0.0.1"},
		"LAN address":  {generated: true, host: "192.0.2.10"},
	} {
		t.Run(name, func(t *testing.T) {
			stubRunningInContainer(t, test.container)
			var output bytes.Buffer
			if err := writeStartupSummary(&output, newStartupSummary("0.9.0", test.host, 23333, true, "bootstrap-token", test.generated)); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "without the token") {
				t.Fatalf("summary promises a token-free Admin:\n%s", output.String())
			}
		})
	}
}
