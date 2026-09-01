//go:build windows

package cli

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

func daemonTempIdentity() string {
	// Windows temp roots are normally already user-specific. Keep the fallback
	// deterministic without embedding a username in the path or logs.
	digest := sha256.Sum256([]byte(os.Getenv("USERDOMAIN") + "\x00" + os.Getenv("USERNAME")))
	return "user-" + hex.EncodeToString(digest[:8])
}

func daemonSysProcAttr() *syscall.SysProcAttr {
	// CREATE_NEW_PROCESS_GROUP detaches Ctrl+C handling from the launcher. The
	// named shutdown event below works from a fresh terminal/console.
	return &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW}
}

func configureDaemonShutdown(cmd *exec.Cmd) (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate shutdown capability: %w", err)
	}
	name := `Local\DepsiloDaemon-` + hex.EncodeToString(nonce)
	cmd.Env = append(daemonBaseEnvironment(), daemonShutdownEventEnv+"="+name)
	return name, nil
}

var (
	createDaemonEvent    = windows.CreateEvent
	openDaemonEvent      = windows.OpenEvent
	setDaemonEvent       = windows.SetEvent
	waitForDaemonEvent   = windows.WaitForSingleObject
	closeDaemonEvent     = windows.CloseHandle
	openDaemonProcess    = windows.OpenProcess
	waitForDaemonProcess = windows.WaitForSingleObject
	closeDaemonProcess   = windows.CloseHandle
)

var requestDaemonShutdown = func(_ *os.Process, record daemonRecord) error {
	if record.ShutdownName == "" {
		return fmt.Errorf("daemon record has no Windows shutdown event")
	}
	name, err := windows.UTF16PtrFromString(record.ShutdownName)
	if err != nil {
		return fmt.Errorf("encode daemon shutdown event name: %w", err)
	}
	handle, err := openDaemonEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return fmt.Errorf("open daemon shutdown event: %w", err)
	}
	defer closeDaemonEvent(handle) //nolint:errcheck // best-effort handle cleanup
	if err := setDaemonEvent(handle); err != nil {
		return fmt.Errorf("signal daemon shutdown event: %w", err)
	}
	return nil
}

func daemonShutdownContext(parent context.Context) (context.Context, context.CancelFunc, error) {
	signalContext, stopSignals := signal.NotifyContext(parent, os.Interrupt)
	nameValue := os.Getenv(daemonShutdownEventEnv)
	if nameValue == "" {
		return signalContext, stopSignals, nil
	}
	name, err := windows.UTF16PtrFromString(nameValue)
	if err != nil {
		stopSignals()
		return nil, nil, fmt.Errorf("encode daemon shutdown event name: %w", err)
	}
	handle, err := createDaemonEvent(nil, 0, 0, name)
	if err != nil {
		stopSignals()
		return nil, nil, fmt.Errorf("create daemon shutdown event: %w", err)
	}

	ctx, cancel := context.WithCancel(signalContext)
	waitDone := make(chan struct{})
	go func() {
		_, _ = waitForDaemonEvent(handle, windows.INFINITE)
		cancel()
		close(waitDone)
	}()
	var cleanupOnce sync.Once
	cleanup := func() {
		cleanupOnce.Do(func() {
			stopSignals()
			cancel()
			// Wake the waiter before closing its handle; closing a handle while a
			// wait is pending has undefined Windows semantics.
			_ = setDaemonEvent(handle)
			<-waitDone
			_ = closeDaemonEvent(handle)
		})
	}
	return ctx, cleanup, nil
}

func daemonProcessIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID %d", pid)
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", fmt.Errorf("open process for creation identity: %w", err)
	}
	defer windows.CloseHandle(handle) //nolint:errcheck // best-effort read-only cleanup
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return "", fmt.Errorf("read process creation identity: %w", err)
	}
	return fmt.Sprintf("windows:%08x%08x", creation.HighDateTime, creation.LowDateTime), nil
}

func processExists(pid int) bool {
	handle, err := openDaemonProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer closeDaemonProcess(handle) //nolint:errcheck // best-effort read-only probe cleanup
	result, err := waitForDaemonProcess(handle, 0)
	return err == nil && result == uint32(windows.WAIT_TIMEOUT)
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
	overlapped := new(windows.Overlapped)
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(windows.Handle(file.Fd()), 0, 1, 0, overlapped)
		_ = file.Close()
	}, nil
}
