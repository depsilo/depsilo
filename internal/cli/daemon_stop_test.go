package cli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const daemonStopHelperEnv = "GO_WANT_DEPSILO_DAEMON_STOP_HELPER"

func TestDaemonStopHelperProcess(t *testing.T) {
	if os.Getenv(daemonStopHelperEnv) != "1" {
		return
	}
	select {}
}

func TestRunStopNeverTrustsLegacyPIDFileFromWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	workingDirectory := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonStopHelperProcess$")
	cmd.Env = append(os.Environ(), daemonStopHelperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start unrelated helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	pidFile := filepath.Join(workingDirectory, ".server.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatalf("write legacy CWD PID file: %v", err)
	}
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(workingDirectory); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWorkingDirectory) })

	originalRequestDaemonShutdown := requestDaemonShutdown
	t.Cleanup(func() { requestDaemonShutdown = originalRequestDaemonShutdown })
	shutdownRequested := false
	requestDaemonShutdown = func(*os.Process, daemonRecord) error {
		shutdownRequested = true
		return errors.New("test must not signal an unrelated process")
	}

	if exitCode := runStop(nil); exitCode != 1 {
		t.Fatalf("runStop() exit code = %d, want 1", exitCode)
	}
	if shutdownRequested {
		t.Fatal("runStop trusted .server.pid from the working directory")
	}
	if !processExists(cmd.Process.Pid) {
		t.Fatal("runStop terminated the unrelated process from .server.pid")
	}
}

func TestRunStopKeepsPIDWhenGracefulShutdownRequestFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	pidDir := filepath.Join(home, ".local", "share", "depsilo")
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create PID directory: %v", err)
	}
	pidFile := filepath.Join(pidDir, "depsilo.pid")
	wantPID := os.Getpid()
	identity, err := getDaemonProcessIdentity(wantPID)
	if err != nil {
		t.Fatalf("read current process identity: %v", err)
	}
	record := daemonRecord{Version: daemonRecordVersion, PID: wantPID, ProcessIdentity: identity}
	if err := writeDaemonRecord(pidFile, record); err != nil {
		t.Fatalf("write daemon record: %v", err)
	}

	originalRequestDaemonShutdown := requestDaemonShutdown
	t.Cleanup(func() {
		requestDaemonShutdown = originalRequestDaemonShutdown
	})
	wantErr := errors.New("shutdown request failed")
	requestDaemonShutdown = func(process *os.Process, gotRecord daemonRecord) error {
		if process.Pid != wantPID {
			t.Fatalf("shutdown process PID = %d, want %d", process.Pid, wantPID)
		}
		if gotRecord != record {
			t.Fatalf("shutdown record = %#v, want %#v", gotRecord, record)
		}
		return wantErr
	}

	if exitCode := runStop(nil); exitCode != 1 {
		t.Fatalf("runStop() exit code = %d, want 1", exitCode)
	}
	gotPID, err := readPID(pidFile)
	if err != nil {
		t.Fatalf("read retained PID file: %v", err)
	}
	if gotPID != wantPID {
		t.Fatalf("retained PID = %d, want %d", gotPID, wantPID)
	}
}

func TestRunStopRefusesReusedPIDWithDifferentProcessIdentity(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	pidDir := filepath.Join(home, ".local", "share", "depsilo")
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create daemon directory: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonStopHelperProcess$")
	cmd.Env = append(os.Environ(), daemonStopHelperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start unrelated helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	identity, err := getDaemonProcessIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("read helper identity: %v", err)
	}
	pidFile := filepath.Join(pidDir, "depsilo.pid")
	staleRecord := daemonRecord{
		Version:         daemonRecordVersion,
		PID:             cmd.Process.Pid,
		ProcessIdentity: identity + "-different-start",
	}
	if err := writeDaemonRecord(pidFile, staleRecord); err != nil {
		t.Fatalf("write stale daemon record: %v", err)
	}

	originalRequestDaemonShutdown := requestDaemonShutdown
	t.Cleanup(func() { requestDaemonShutdown = originalRequestDaemonShutdown })
	requestDaemonShutdown = func(*os.Process, daemonRecord) error {
		t.Fatal("runStop signalled a process whose creation identity did not match")
		return nil
	}

	if exitCode := runStop(nil); exitCode != 1 {
		t.Fatalf("runStop() exit code = %d, want 1 for stale record", exitCode)
	}
	if !processExists(cmd.Process.Pid) {
		t.Fatal("runStop terminated the unrelated reused-PID process")
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale daemon record was not removed: %v", err)
	}
}

func TestRunStopRejectsLegacyBarePIDRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	pidDir := filepath.Join(home, ".local", "share", "depsilo")
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		t.Fatalf("create daemon directory: %v", err)
	}
	pidFile := filepath.Join(pidDir, "depsilo.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatalf("write legacy PID-only record: %v", err)
	}

	originalRequestDaemonShutdown := requestDaemonShutdown
	t.Cleanup(func() { requestDaemonShutdown = originalRequestDaemonShutdown })
	requestDaemonShutdown = func(*os.Process, daemonRecord) error {
		t.Fatal("runStop signalled a process from an unauthenticated PID-only record")
		return nil
	}

	if exitCode := runStop(nil); exitCode != 1 {
		t.Fatalf("runStop() exit code = %d, want 1", exitCode)
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("unsafe record should be retained for explicit operator inspection: %v", err)
	}
}

func TestStopDaemonChildKillsImmediatelyWhenGracefulRequestFails(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonStopHelperProcess$")
	cmd.Env = append(os.Environ(), daemonStopHelperEnv+"=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon helper: %v", err)
	}
	processDone := make(chan error, 1)
	go func() {
		processDone <- cmd.Wait()
	}()

	originalRequestDaemonShutdown := requestDaemonShutdown
	t.Cleanup(func() {
		requestDaemonShutdown = originalRequestDaemonShutdown
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})
	requestDaemonShutdown = func(*os.Process, daemonRecord) error {
		return errors.New("shutdown request failed")
	}

	stopReturned := make(chan struct{})
	go func() {
		stopDaemonChild(cmd.Process, processDone, daemonRecord{})
		close(stopReturned)
	}()
	select {
	case <-stopReturned:
		if cmd.ProcessState == nil {
			t.Fatalf("daemon helper was not terminated: state=%v", cmd.ProcessState)
		}
	case <-time.After(500 * time.Millisecond):
		_ = cmd.Process.Kill()
		select {
		case <-stopReturned:
		case <-time.After(2 * time.Second):
			t.Fatal("stopDaemonChild did not return after cleanup kill")
		}
		t.Fatal("stopDaemonChild waited for the graceful timeout after notification failed")
	}
}
