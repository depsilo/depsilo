//go:build darwin

package cli

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func daemonProcessIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID %d", pid)
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", fmt.Errorf("read process creation time: %w", err)
	}
	return fmt.Sprintf("darwin:%d:%d", process.Proc.P_starttime.Sec, process.Proc.P_starttime.Usec), nil
}
