package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const cliHelperEnv = "GO_WANT_DEPSILO_CLI_HELPER"

func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv(cliHelperEnv) != "1" {
		return
	}

	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(2)
	}
	command := os.Args[separator+1]
	args := os.Args[separator+2:]
	if command == "serve" {
		exitCode := RunServe(args)
		if marker := os.Getenv("DEPSILO_CLI_RETURN_MARKER"); marker != "" {
			_ = os.WriteFile(marker, []byte(strconv.Itoa(exitCode)), 0o600)
		}
		os.Exit(exitCode)
	}
	if command == "help" {
		PrintHelp()
		os.Exit(0)
	}
	os.Exit(Run(command, args))
}

func TestStartForegroundUsesServeFlags(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "start", "--help")
	cmd.Env = append(os.Environ(),
		cliHelperEnv+"=1",
		"HOME="+t.TempDir(),
		"DEPSILO_SERVER_PORT=0",
		"DEPSILO_AUTH_JWT_SECRET=0123456789abcdef0123456789abcdef",
		"DEPSILO_BOOTSTRAP_TOKEN=0123456789abcdef01234567",
	)
	output, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("start --help started the server instead of returning serve help; output=%s", output)
	}
	if err != nil {
		t.Fatalf("start --help: %v; output=%s", err, output)
	}
	if !strings.Contains(string(output), "Usage:\n    depsilo serve [flags]") {
		t.Fatalf("start --help did not expose serve flags; output=%s", output)
	}
}

func TestServeSignalShutdownHasDeadline(t *testing.T) {
	port := availablePort(t)
	logFile, err := os.CreateTemp(t.TempDir(), "serve-*.log")
	if err != nil {
		t.Fatalf("create serve log: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "serve",
		"--host", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Env = append(os.Environ(),
		cliHelperEnv+"=1",
		"HOME="+t.TempDir(),
		"DEPSILO_CONFIG=",
		"DEPSILO_AUTH_JWT_SECRET=0123456789abcdef0123456789abcdef",
		"DEPSILO_BOOTSTRAP_TOKEN=0123456789abcdef01234567",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve helper: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			<-done
		}
		_ = logFile.Close()
	}()

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	waitForCLIReady(t, baseURL+"/ready", done)

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatalf("open blocking request: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "POST /api/v1/auth/login HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\nContent-Length: 1048576\r\n\r\n{"); err != nil {
		t.Fatalf("write blocking request: %v", err)
	}
	// Let the handler enter JSON body decoding before shutdown begins.
	time.Sleep(100 * time.Millisecond)

	started := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal serve helper: %v", err)
	}
	select {
	case <-done:
		if elapsed := time.Since(started); elapsed > 12*time.Second {
			t.Fatalf("serve took %s to honor its shutdown deadline, want at most 12s", elapsed)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("serve remained blocked past the shutdown deadline")
	}
}

func TestDaemonUsesServePortAndWritesPrivateStartupLog(t *testing.T) {
	binary := buildDepsiloCLI(t)
	home := t.TempDir()
	port := availablePort(t)
	env := append(os.Environ(),
		cliHelperEnv+"=",
		"HOME="+home,
		"USERPROFILE="+home,
		"DEPSILO_CONFIG=",
		"DEPSILO_SERVER_PORT=",
		"DEPSILO_BOOTSTRAP_TOKEN=",
	)
	pidFile := filepath.Join(home, ".local", "share", "depsilo", "depsilo.pid")
	logPath := filepath.Join(home, ".local", "share", "depsilo", "depsilo.log")
	defer terminateDaemonFromPIDFile(pidFile)

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	start := exec.CommandContext(ctx, binary, "start", "--daemon", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	start.Env = env
	output, err := start.CombinedOutput()
	if err != nil {
		t.Fatalf("start daemon: %v; output=%s", err, output)
	}
	if ctx.Err() != nil {
		t.Fatalf("start daemon timed out: %v; output=%s", ctx.Err(), output)
	}
	if !strings.Contains(string(output), logPath) {
		t.Fatalf("start output omitted startup log path %q; output=%s", logPath, output)
	}

	response, err := (&http.Client{Timeout: time.Second}).Get(fmt.Sprintf("http://127.0.0.1:%d/ready", port))
	if err != nil {
		t.Fatalf("daemon did not listen on requested port %d: %v; output=%s", port, err, output)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("daemon readiness status = %d, want 200", response.StatusCode)
	}
	record, err := readDaemonRecord(pidFile)
	if err != nil {
		t.Fatalf("read structured daemon record: %v", err)
	}
	if record.Version != daemonRecordVersion || record.PID <= 0 || record.ProcessIdentity == "" {
		t.Fatalf("daemon record omitted process identity: %#v", record)
	}
	matches, err := daemonRecordMatches(record)
	if err != nil || !matches {
		t.Fatalf("daemon record does not identify the running child: matches=%v error=%v", matches, err)
	}
	if runtime.GOOS == "windows" && record.ShutdownName == "" {
		t.Fatal("Windows daemon record omitted its named shutdown capability")
	}
	pidInfo, err := os.Stat(pidFile)
	if err != nil {
		t.Fatalf("stat daemon record: %v", err)
	}
	if permissions := pidInfo.Mode().Perm(); runtime.GOOS != "windows" && permissions != 0o600 {
		t.Fatalf("daemon record permissions = %#o, want 0600", permissions)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat daemon startup log: %v", err)
	}
	if permissions := info.Mode().Perm(); runtime.GOOS != "windows" && permissions != 0o600 {
		t.Fatalf("daemon log permissions = %#o, want 0600", permissions)
	}
	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read daemon startup log: %v", err)
	}
	if !regexp.MustCompile(`Bootstrap token: [A-Za-z0-9_-]{24,}`).Match(logBody) {
		t.Fatalf("daemon startup log omitted generated bootstrap token; log=%s", logBody)
	}

	stop := exec.Command(binary, "stop")
	stop.Env = env
	stopOutput, err := stop.CombinedOutput()
	if err != nil {
		t.Fatalf("stop daemon: %v; output=%s", err, stopOutput)
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("daemon PID file still exists after stop: %v", err)
	}
}

func TestDaemonReadinessClientAllowsSlowSuccessfulCheck(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		time.Sleep(750 * time.Millisecond)
		response.WriteHeader(http.StatusOK)
	}))
	defer service.Close()

	response, err := newDaemonReadinessClient().Get(service.URL)
	if err != nil {
		t.Fatalf("readiness client rejected a valid slow check: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("readiness status = %d, want 200", response.StatusCode)
	}
}

func TestConcurrentDaemonStartsShareOneProcess(t *testing.T) {
	binary := buildDepsiloCLI(t)
	home := t.TempDir()
	port := availablePort(t)
	env := append(os.Environ(),
		cliHelperEnv+"=",
		"HOME="+home,
		"USERPROFILE="+home,
		"DEPSILO_CONFIG=",
		"DEPSILO_SERVER_PORT=",
		"DEPSILO_BOOTSTRAP_TOKEN=0123456789abcdef01234567",
	)
	pidFile := filepath.Join(home, ".local", "share", "depsilo", "depsilo.pid")
	defer terminateDaemonFromPIDFile(pidFile)

	type startResult struct {
		output []byte
		err    error
	}
	results := make(chan startResult, 2)
	for range 2 {
		go func() {
			command := exec.Command(binary, "start", "--daemon", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
			command.Env = env
			output, err := command.CombinedOutput()
			results <- startResult{output: output, err: err}
		}()
	}
	combinedOutput := ""
	for range 2 {
		result := <-results
		combinedOutput += string(result.output)
		if result.err != nil {
			t.Fatalf("concurrent start failed: %v; output=%s", result.err, result.output)
		}
	}
	if strings.Count(combinedOutput, "Depsilo starting") != 1 ||
		strings.Count(combinedOutput, "Depsilo already running") != 1 {
		t.Fatalf("concurrent starts did not elect exactly one owner; output=%s", combinedOutput)
	}
	record, err := readDaemonRecord(pidFile)
	if err != nil {
		t.Fatalf("read elected daemon record: %v", err)
	}
	if matches, matchErr := daemonRecordMatches(record); matchErr != nil || !matches {
		t.Fatalf("elected daemon is not alive: matches=%v error=%v", matches, matchErr)
	}
}

func TestDaemonStartupFailureReturnsNonZeroAndRemovesPID(t *testing.T) {
	binary := buildDepsiloCLI(t)
	home := t.TempDir()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy daemon port: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	env := append(os.Environ(),
		cliHelperEnv+"=",
		"HOME="+home,
		"USERPROFILE="+home,
		"DEPSILO_CONFIG=",
		"DEPSILO_SERVER_PORT=",
		"DEPSILO_BOOTSTRAP_TOKEN=0123456789abcdef01234567",
	)
	pidFile := filepath.Join(home, ".local", "share", "depsilo", "depsilo.pid")
	logPath := filepath.Join(home, ".local", "share", "depsilo", "depsilo.log")
	defer terminateDaemonFromPIDFile(pidFile)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := exec.CommandContext(ctx, binary, "start", "--daemon", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	start.Env = env
	output, runErr := start.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("daemon parent did not observe child startup failure: %v; output=%s", ctx.Err(), output)
	}
	if runErr == nil {
		t.Fatalf("daemon startup failure exited successfully; output=%s", output)
	}
	if _, err := os.Stat(pidFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed daemon left PID file behind: %v", err)
	}
	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read failed daemon log: %v", err)
	}
	if !strings.Contains(string(logBody), "failed to start server") || !strings.Contains(string(logBody), "address already in use") {
		t.Fatalf("daemon log omitted listen failure; log=%s", logBody)
	}
}

func TestServeStartupFailureReturnsExitCode(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy serve port: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	marker := filepath.Join(t.TempDir(), "returned")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "serve",
		"--host", "127.0.0.1", "--port", strconv.Itoa(port))
	cmd.Env = append(os.Environ(),
		cliHelperEnv+"=1",
		"HOME="+t.TempDir(),
		"DEPSILO_CONFIG=",
		"DEPSILO_CLI_RETURN_MARKER="+marker,
		"DEPSILO_AUTH_JWT_SECRET=0123456789abcdef0123456789abcdef",
		"DEPSILO_BOOTSTRAP_TOKEN=0123456789abcdef01234567",
	)
	output, runErr := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("serve did not exit after startup failure: %v; output=%s", ctx.Err(), output)
	}
	if runErr == nil {
		t.Fatalf("serve startup failure exited successfully; output=%s", output)
	}
	returned, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("RunServe terminated the process instead of returning an exit code: %v; output=%s", err, output)
	}
	if string(returned) != "1" {
		t.Fatalf("RunServe returned %q, want 1; output=%s", returned, output)
	}
}

func TestStopAllowsServeShutdownDeadline(t *testing.T) {
	binary := buildDepsiloCLI(t)
	home := t.TempDir()
	port := availablePort(t)
	env := append(os.Environ(),
		cliHelperEnv+"=",
		"HOME="+home,
		"USERPROFILE="+home,
		"DEPSILO_CONFIG=",
		"DEPSILO_SERVER_PORT=",
		"DEPSILO_BOOTSTRAP_TOKEN=0123456789abcdef01234567",
	)
	pidFile := filepath.Join(home, ".local", "share", "depsilo", "depsilo.pid")
	defer terminateDaemonFromPIDFile(pidFile)

	start := exec.Command(binary, "start", "--daemon", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	start.Env = env
	if output, err := start.CombinedOutput(); err != nil {
		t.Fatalf("start daemon: %v; output=%s", err, output)
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err != nil {
		t.Fatalf("open blocking request: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "POST /api/v1/auth/login HTTP/1.1\r\nHost: 127.0.0.1\r\nContent-Type: application/json\r\nContent-Length: 1048576\r\n\r\n{"); err != nil {
		t.Fatalf("write blocking request: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stop := exec.CommandContext(ctx, binary, "stop")
	stop.Env = env
	started := time.Now()
	output, err := stop.CombinedOutput()
	elapsed := time.Since(started)
	if ctx.Err() != nil {
		t.Fatalf("stop exceeded its bounded wait: %v; output=%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("stop daemon: %v; output=%s", err, output)
	}
	if elapsed < 9*time.Second {
		t.Fatalf("stop waited only %s and killed serve before its 10s graceful deadline; output=%s", elapsed, output)
	}
}

func availablePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func buildDepsiloCLI(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	binaryName := "depsilo-test"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	binary := filepath.Join(t.TempDir(), binaryName)
	build := exec.Command("go", "build", "-o", binary, "./cmd/depsilo")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build depsilo CLI: %v; output=%s", err, output)
	}
	return binary
}

func terminateDaemonFromPIDFile(pidFile string) {
	record, err := readDaemonRecord(pidFile)
	if err != nil {
		return
	}
	process, err := os.FindProcess(record.PID)
	if err != nil {
		return
	}
	_ = requestDaemonShutdown(process, record)
	for range 20 {
		if !processExists(record.PID) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = process.Kill()
}

func waitForCLIReady(t *testing.T, url string, processDone <-chan error) {
	t.Helper()
	client := &http.Client{Timeout: 200 * time.Millisecond}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-processDone:
			t.Fatalf("serve exited before readiness: %v", err)
		case <-deadline.C:
			t.Fatalf("serve did not become ready at %s", url)
		case <-ticker.C:
			response, err := client.Get(url)
			if err == nil {
				_ = response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return
				}
			}
		}
	}
}
