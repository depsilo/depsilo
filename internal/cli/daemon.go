package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"depsilo/internal/config"
)

const (
	daemonRecordVersion           = 1
	daemonShutdownEventEnv        = "DEPSILO_DAEMON_SHUTDOWN_EVENT"
	daemonReadinessTimeout        = 30 * time.Second
	daemonReadinessRequestTimeout = 3 * time.Second
	daemonStopTimeout             = gracefulShutdownTimeout + 2*time.Second
	daemonForceStopTimeout        = 3 * time.Second
)

var errDaemonRecordNotFound = errors.New("daemon record not found")

// daemonRecord binds a PID to the operating system's immutable process-start
// identity. A PID by itself can be reused and must never authorize a signal.
// Shutdown names are unguessable per-start capabilities used by Windows named
// events, where console control events cannot cross arbitrary CLI consoles.
type daemonRecord struct {
	Version         int    `json:"version"`
	PID             int    `json:"pid"`
	ProcessIdentity string `json:"process_identity"`
	ShutdownName    string `json:"shutdown_name,omitempty"`
}

// getDaemonProcessIdentity is a test seam. Platform implementations return a
// boot-scoped process creation identity, not mutable command-line metadata.
var getDaemonProcessIdentity = daemonProcessIdentity

// ── runStart ────────────────────────────────────────────────────

func runStart(args []string) int {
	daemon := false
	serveArgs := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--daemon" || a == "-d" {
			daemon = true
			continue
		}
		serveArgs = append(serveArgs, a)
	}

	if !daemon {
		return RunServe(serveArgs)
	}

	opts, err := ParseServeFlags(serveArgs, os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	opts.ApplyEnv()
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading daemon configuration: %v\n", err)
		return 1
	}
	readyURL := daemonReadinessURL(cfg.Server.Host, cfg.Server.Port)

	pidDir := getPIDDir()
	if err := secureDaemonDir(pidDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot secure daemon directory: %v\n", err)
		return 1
	}
	releaseLock, err := acquireDaemonLock(filepath.Join(pidDir, "depsilo.lock"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot lock daemon state: %v\n", err)
		return 1
	}
	defer releaseLock()

	pidFile := filepath.Join(pidDir, "depsilo.pid")
	existing, readErr := readDaemonRecord(pidFile)
	switch {
	case readErr == nil:
		matches, matchErr := daemonRecordMatches(existing)
		if matchErr != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot validate existing daemon record: %v\n", matchErr)
			return 1
		}
		if matches {
			fmt.Printf("Depsilo already running (PID %d)\n", existing.PID)
			return 0
		}
		_ = os.Remove(pidFile)
	case !errors.Is(readErr, errDaemonRecordNotFound):
		fmt.Fprintf(os.Stderr, "Error: unsafe or invalid daemon record %s: %v; verify the recorded process and remove the file manually\n", pidFile, readErr)
		return 1
	}

	logPath := filepath.Join(pidDir, "depsilo.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot open daemon log: %v\n", err)
		return 1
	}
	if err := logFile.Chmod(0o600); err != nil {
		_ = logFile.Close()
		fmt.Fprintf(os.Stderr, "Error: cannot secure daemon log: %v\n", err)
		return 1
	}
	defer logFile.Close()

	childArgs := append([]string{"serve"}, serveArgs...)
	cmd := exec.Command(os.Args[0], childArgs...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = daemonSysProcAttr()
	shutdownName, err := configureDaemonShutdown(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error preparing daemon shutdown channel: %v\n", err)
		return 1
	}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
		return 1
	}
	childDone := make(chan error, 1)
	go func() { childDone <- cmd.Wait() }()

	identity, err := getDaemonProcessIdentity(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		<-childDone
		fmt.Fprintf(os.Stderr, "Error identifying daemon process: %v\n", err)
		return 1
	}
	record := daemonRecord{
		Version:         daemonRecordVersion,
		PID:             cmd.Process.Pid,
		ProcessIdentity: identity,
		ShutdownName:    shutdownName,
	}
	if err := writeDaemonRecord(pidFile, record); err != nil {
		stopDaemonChild(cmd.Process, childDone, record)
		fmt.Fprintf(os.Stderr, "Error: could not write daemon record: %v\n", err)
		return 1
	}

	fmt.Printf("Depsilo starting (PID %d); log: %s\n", cmd.Process.Pid, logPath)
	client := newDaemonReadinessClient()
	deadline := time.NewTimer(daemonReadinessTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case childErr := <-childDone:
			_ = os.Remove(pidFile)
			fmt.Fprintf(os.Stderr, "Error: daemon exited before readiness: %v (see %s)\n", childErr, logPath)
			return 1
		case <-deadline.C:
			stopDaemonChild(cmd.Process, childDone, record)
			_ = os.Remove(pidFile)
			fmt.Fprintf(os.Stderr, "Error: daemon did not become ready at %s within %s (see %s)\n", readyURL, daemonReadinessTimeout, logPath)
			return 1
		case <-ticker.C:
			response, requestErr := client.Get(readyURL)
			if requestErr != nil {
				continue
			}
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				fmt.Printf("Depsilo ready (PID %d) at %s\n", cmd.Process.Pid, readyURL)
				return 0
			}
		}
	}
}

func newDaemonReadinessClient() *http.Client {
	// The server gives its database and storage readiness checks a shared two-
	// second budget. The launcher must allow that contract to complete, with a
	// small transport margin, or a healthy remote S3 backend can look dead.
	return &http.Client{Timeout: daemonReadinessRequestTimeout}
}

func daemonReadinessURL(host string, port int) string {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port)) + "/ready"
}

func stopDaemonChild(process *os.Process, done <-chan error, record daemonRecord) {
	if process == nil {
		return
	}
	if err := requestDaemonShutdown(process, record); err != nil {
		_ = process.Kill()
		<-done
		return
	}
	timer := time.NewTimer(daemonStopTimeout)
	defer timer.Stop()
	select {
	case <-done:
		return
	case <-timer.C:
		_ = process.Kill()
		<-done
	}
}

// ── runStop ─────────────────────────────────────────────────────

func runStop(args []string) int {
	_ = args
	pidDir := getPIDDir()
	if err := secureDaemonDir(pidDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot secure daemon directory: %v\n", err)
		return 1
	}
	releaseLock, err := acquireDaemonLock(filepath.Join(pidDir, "depsilo.lock"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot lock daemon state: %v\n", err)
		return 1
	}
	defer releaseLock()

	pidFile := filepath.Join(pidDir, "depsilo.pid")
	record, err := readDaemonRecord(pidFile)
	if err != nil {
		if errors.Is(err, errDaemonRecordNotFound) {
			fmt.Fprintln(os.Stderr, "Depsilo is not running")
		} else {
			fmt.Fprintf(os.Stderr, "Error: unsafe or invalid daemon record %s: %v; refusing to signal any process\n", pidFile, err)
		}
		return 1
	}

	matches, err := daemonRecordMatches(record)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error validating Depsilo process identity: %v\n", err)
		return 1
	}
	if !matches {
		_ = os.Remove(pidFile)
		fmt.Fprintln(os.Stderr, "Depsilo is not running (removed stale daemon record)")
		return 1
	}

	process, err := os.FindProcess(record.PID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Depsilo is not running")
		_ = os.Remove(pidFile)
		return 1
	}
	if err := requestDaemonShutdown(process, record); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping Depsilo: %v\n", err)
		removeRecordIfProcessEnded(pidFile, record)
		return 1
	}

	fmt.Printf("Stopping Depsilo (PID %d)...", record.PID)
	deadline := time.Now().Add(daemonStopTimeout)
	for time.Now().Before(deadline) {
		matches, matchErr := daemonRecordMatches(record)
		if matchErr != nil {
			fmt.Println()
			fmt.Fprintf(os.Stderr, "Error validating Depsilo process while stopping: %v\n", matchErr)
			return 1
		}
		if !matches {
			fmt.Println(" stopped!")
			_ = os.Remove(pidFile)
			return 0
		}
		time.Sleep(200 * time.Millisecond)
		fmt.Print(".")
	}

	fmt.Println()
	fmt.Fprintln(os.Stderr, "Warning: process did not exit in time; forcing termination")
	matches, err = daemonRecordMatches(record)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error revalidating Depsilo process identity: %v\n", err)
		return 1
	}
	if !matches {
		_ = os.Remove(pidFile)
		return 0
	}
	if err := process.Kill(); err != nil {
		removeRecordIfProcessEnded(pidFile, record)
		fmt.Fprintf(os.Stderr, "Error force-stopping Depsilo: %v\n", err)
		return 1
	}

	forceDeadline := time.Now().Add(daemonForceStopTimeout)
	for time.Now().Before(forceDeadline) {
		matches, matchErr := daemonRecordMatches(record)
		if matchErr != nil {
			fmt.Fprintf(os.Stderr, "Error confirming forced shutdown: %v\n", matchErr)
			return 1
		}
		if !matches {
			_ = os.Remove(pidFile)
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Fprintln(os.Stderr, "Error: Depsilo still appears to be running after forced termination; daemon record retained")
	return 1
}

// ── daemon record helpers ───────────────────────────────────────

func getPIDDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "depsilo-"+daemonTempIdentity())
	}
	return filepath.Join(home, ".local", "share", "depsilo")
}

func secureDaemonDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func readDaemonRecord(path string) (daemonRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return daemonRecord{}, errDaemonRecordNotFound
	}
	if err != nil {
		return daemonRecord{}, err
	}
	var record daemonRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return daemonRecord{}, fmt.Errorf("legacy or malformed PID-only record is not a safe process identity: %w", err)
	}
	if record.Version != daemonRecordVersion || record.PID <= 0 || strings.TrimSpace(record.ProcessIdentity) == "" {
		return daemonRecord{}, errors.New("invalid daemon record fields")
	}
	return record, nil
}

func writeDaemonRecord(path string, record daemonRecord) error {
	if record.Version != daemonRecordVersion || record.PID <= 0 || strings.TrimSpace(record.ProcessIdentity) == "" {
		return errors.New("refusing to write invalid daemon record")
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".depsilo.pid-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	if directory, openErr := os.Open(dir); openErr == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func daemonRecordMatches(record daemonRecord) (bool, error) {
	if !processExists(record.PID) {
		return false, nil
	}
	identity, err := getDaemonProcessIdentity(record.PID)
	if err != nil {
		if !processExists(record.PID) {
			return false, nil
		}
		return false, err
	}
	return identity == record.ProcessIdentity, nil
}

func removeRecordIfProcessEnded(path string, record daemonRecord) {
	matches, err := daemonRecordMatches(record)
	if err == nil && !matches {
		_ = os.Remove(path)
	}
}

func daemonBaseEnvironment() []string {
	prefix := daemonShutdownEventEnv + "="
	environment := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, prefix) {
			environment = append(environment, entry)
		}
	}
	return environment
}

// readPID remains a compatibility helper for tests and diagnostics. Production
// stop/start paths intentionally require the full structured daemon record.
func readPID(path string) (int, error) {
	record, err := readDaemonRecord(path)
	if err == nil {
		return record.PID, nil
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		return 0, readErr
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}
