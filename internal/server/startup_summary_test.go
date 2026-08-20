package server

import (
	"bytes"
	"strings"
	"testing"
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := startupPortalURL(test.host, 23333); got != test.want {
				t.Fatalf("startupPortalURL(%q) = %q, want %q", test.host, got, test.want)
			}
		})
	}
}

func TestWriteStartupSummaryForFirstRun(t *testing.T) {
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
		"First-time setup:",
		"Bootstrap token: bootstrap-token",
		"Open the Portal to finish setup.",
	} {
		if !strings.Contains(got, fragment) {
			t.Errorf("startup summary missing %q:\n%s", fragment, got)
		}
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
	for _, forbidden := range []string{"First-time setup", "Bootstrap token", "must-not-appear", "finish setup"} {
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
