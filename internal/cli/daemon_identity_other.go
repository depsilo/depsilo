//go:build !linux && !darwin && !windows

package cli

import "fmt"

func daemonProcessIdentity(pid int) (string, error) {
	return "", fmt.Errorf("safe daemon process identity is unsupported on this operating system for PID %d", pid)
}
