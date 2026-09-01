//go:build windows

package cli

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestRequestDaemonShutdownSignalsNamedEvent(t *testing.T) {
	originalOpen := openDaemonEvent
	originalSet := setDaemonEvent
	originalClose := closeDaemonEvent
	t.Cleanup(func() {
		openDaemonEvent = originalOpen
		setDaemonEvent = originalSet
		closeDaemonEvent = originalClose
	})

	wantName := `Local\DepsiloDaemon-0123456789abcdef`
	wantHandle := windows.Handle(42)
	openDaemonEvent = func(access uint32, inherit bool, name *uint16) (windows.Handle, error) {
		if access != windows.EVENT_MODIFY_STATE {
			t.Errorf("OpenEvent access = %#x, want EVENT_MODIFY_STATE", access)
		}
		if inherit {
			t.Error("OpenEvent inherit = true, want false")
		}
		if got := windows.UTF16PtrToString(name); got != wantName {
			t.Errorf("OpenEvent name = %q, want %q", got, wantName)
		}
		return wantHandle, nil
	}
	setCalls := 0
	setDaemonEvent = func(handle windows.Handle) error {
		setCalls++
		if handle != wantHandle {
			t.Errorf("SetEvent handle = %d, want %d", handle, wantHandle)
		}
		return nil
	}
	closeCalls := 0
	closeDaemonEvent = func(handle windows.Handle) error {
		closeCalls++
		return nil
	}

	record := daemonRecord{ShutdownName: wantName}
	if err := requestDaemonShutdown(&os.Process{Pid: 8675}, record); err != nil {
		t.Fatalf("requestDaemonShutdown() error = %v", err)
	}
	if setCalls != 1 || closeCalls != 1 {
		t.Fatalf("event calls: Set=%d Close=%d, want one each", setCalls, closeCalls)
	}
}

func TestRequestDaemonShutdownFailsClosedWithoutEvent(t *testing.T) {
	originalOpen := openDaemonEvent
	t.Cleanup(func() { openDaemonEvent = originalOpen })
	openDaemonEvent = func(uint32, bool, *uint16) (windows.Handle, error) {
		t.Fatal("OpenEvent must not run without an authenticated event name")
		return 0, nil
	}
	err := requestDaemonShutdown(&os.Process{Pid: 8675}, daemonRecord{})
	if err == nil || !strings.Contains(err.Error(), "no Windows shutdown event") {
		t.Fatalf("requestDaemonShutdown() error = %v, want missing-event error", err)
	}
}

func TestRequestDaemonShutdownRetainsOpenEventFailure(t *testing.T) {
	originalOpen := openDaemonEvent
	t.Cleanup(func() { openDaemonEvent = originalOpen })
	wantErr := errors.New("event does not exist")
	openDaemonEvent = func(uint32, bool, *uint16) (windows.Handle, error) {
		return 0, wantErr
	}
	err := requestDaemonShutdown(nil, daemonRecord{ShutdownName: `Local\DepsiloDaemon-test`})
	if !errors.Is(err, wantErr) {
		t.Fatalf("requestDaemonShutdown() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestConfigureDaemonShutdownCreatesNoWindowAndFreshCapability(t *testing.T) {
	t.Setenv(daemonShutdownEventEnv, "stale-parent-value")
	cmd := exec.Command("depsilo.exe", "serve")
	name, err := configureDaemonShutdown(cmd)
	if err != nil {
		t.Fatalf("configureDaemonShutdown() error = %v", err)
	}
	if !strings.HasPrefix(name, `Local\DepsiloDaemon-`) || len(strings.TrimPrefix(name, `Local\DepsiloDaemon-`)) != 64 {
		t.Fatalf("shutdown capability name = %q, want prefix plus 256-bit nonce", name)
	}
	wantEntry := daemonShutdownEventEnv + "=" + name
	count := 0
	for _, entry := range cmd.Env {
		if strings.HasPrefix(entry, daemonShutdownEventEnv+"=") {
			count++
			if entry != wantEntry {
				t.Fatalf("daemon event environment = %q, want %q", entry, wantEntry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("daemon event environment count = %d, want 1", count)
	}
	flags := daemonSysProcAttr().CreationFlags
	for name, flag := range map[string]uint32{
		"CREATE_NEW_PROCESS_GROUP": windows.CREATE_NEW_PROCESS_GROUP,
		"CREATE_NO_WINDOW":         windows.CREATE_NO_WINDOW,
	} {
		if flags&flag == 0 {
			t.Errorf("daemon creation flags %#x omit %s", flags, name)
		}
	}
}

func TestDaemonShutdownContextReceivesNamedEvent(t *testing.T) {
	name := `Local\DepsiloDaemon-test-` + strings.ReplaceAll(t.Name(), "/", "-")
	t.Setenv(daemonShutdownEventEnv, name)
	ctx, cancel, err := daemonShutdownContext(t.Context())
	if err != nil {
		t.Fatalf("daemonShutdownContext() error = %v", err)
	}
	defer cancel()

	namePointer, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatalf("encode event name: %v", err)
	}
	handle, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, namePointer)
	if err != nil {
		t.Fatalf("open created daemon event: %v", err)
	}
	if err := windows.SetEvent(handle); err != nil {
		_ = windows.CloseHandle(handle)
		t.Fatalf("signal daemon event: %v", err)
	}
	_ = windows.CloseHandle(handle)
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("daemon context did not stop after named event was signalled")
	}
}

func TestProcessExistsWaitsOnAndClosesProcessHandle(t *testing.T) {
	originalOpen := openDaemonProcess
	originalWait := waitForDaemonProcess
	originalClose := closeDaemonProcess
	t.Cleanup(func() {
		openDaemonProcess = originalOpen
		waitForDaemonProcess = originalWait
		closeDaemonProcess = originalClose
	})

	wantPID := uint32(8675)
	wantHandle := windows.Handle(42)
	openDaemonProcess = func(access uint32, inherit bool, pid uint32) (windows.Handle, error) {
		if access != windows.SYNCHRONIZE {
			t.Errorf("OpenProcess access = %#x, want SYNCHRONIZE", access)
		}
		if inherit || pid != wantPID {
			t.Errorf("OpenProcess inherit=%v pid=%d, want false/%d", inherit, pid, wantPID)
		}
		return wantHandle, nil
	}
	closeCalls := 0
	closeDaemonProcess = func(handle windows.Handle) error {
		closeCalls++
		return nil
	}
	waitResult := uint32(windows.WAIT_TIMEOUT)
	waitForDaemonProcess = func(handle windows.Handle, milliseconds uint32) (uint32, error) {
		if handle != wantHandle || milliseconds != 0 {
			t.Errorf("WaitForSingleObject handle=%d timeout=%d", handle, milliseconds)
		}
		return waitResult, nil
	}

	if !processExists(int(wantPID)) {
		t.Error("processExists() = false for a running process")
	}
	waitResult = windows.WAIT_OBJECT_0
	if processExists(int(wantPID)) {
		t.Error("processExists() = true for an exited process")
	}
	if closeCalls != 2 {
		t.Errorf("CloseHandle calls = %d, want 2", closeCalls)
	}
}
