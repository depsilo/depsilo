//go:build windows

package cli

import (
	"os"
	"syscall"
)

func daemonSysProcAttr() *syscall.SysProcAttr {
	// CREATE_NEW_PROCESS_GROUP — lets the parent exit without killing the child.
	return &syscall.SysProcAttr{CreationFlags: 0x00000200}
}

func processExists(pid int) bool {
	// On Windows, os.FindProcess opens a real handle; success ⇒ alive.
	_, err := os.FindProcess(pid)
	return err == nil
}
