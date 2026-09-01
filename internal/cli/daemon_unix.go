//go:build !windows

package cli

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

func daemonTempIdentity() string { return "uid-" + strconv.Itoa(os.Geteuid()) }

func daemonSysProcAttr() *syscall.SysProcAttr {
	// A new session removes the child from the launcher's controlling terminal,
	// so closing that terminal cannot deliver a daemon-killing SIGHUP.
	return &syscall.SysProcAttr{Setsid: true}
}

func configureDaemonShutdown(cmd *exec.Cmd) (string, error) {
	cmd.Env = daemonBaseEnvironment()
	return "", nil
}

var requestDaemonShutdown = func(process *os.Process, _ daemonRecord) error {
	return process.Signal(syscall.SIGTERM)
}

func daemonShutdownContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	ctx, cancel := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	return ctx, cancel, nil
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func acquireDaemonLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
		_ = file.Close()
	}, nil
}
