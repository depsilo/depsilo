//go:build !windows

package cli

import "testing"

func TestDaemonStartsInNewSession(t *testing.T) {
	attributes := daemonSysProcAttr()
	if attributes == nil || !attributes.Setsid {
		t.Fatalf("daemon SysProcAttr = %#v, want Setsid=true", attributes)
	}
}
