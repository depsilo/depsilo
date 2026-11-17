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
	"sync"
	"syscall"
	"testing"
	"time"

	"depsilo/internal/api"
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
		exitCode := Run(command, args)
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
	if !strings.Contains(string(logBody), fmt.Sprintf("Port %d is already in use.", port)) || strings.Contains(string(logBody), "stacktrace") {
		t.Fatalf("daemon log did not explain the listen failure in one plain line; log=%s", logBody)
	}
}

// The reason a daemon died at once used to reach only its startup log; the
// terminal showed "exit status 1" and a path.
func TestDaemonStartupFailurePrintsTheDaemonsLastMessage(t *testing.T) {
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
		"DEPSILO_BOOTSTRAP_TOKEN=",
	)
	pidFile := filepath.Join(home, ".local", "share", "depsilo", "depsilo.pid")
	logPath := filepath.Join(home, ".local", "share", "depsilo", "depsilo.log")
	defer terminateDaemonFromPIDFile(pidFile)
	// What an earlier, successful start left behind must not be repeated.
	const earlierToken = "Bootstrap token: left-by-an-earlier-start"
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("\n  "+earlierToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := exec.CommandContext(ctx, binary, "start", "--daemon", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	start.Env = env
	var stdout, stderr strings.Builder
	start.Stdout, start.Stderr = &stdout, &stderr
	runErr := start.Run()
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
		t.Fatalf("start --daemon on an occupied port: error=%v, want exit code 1; stdout=%s stderr=%s", runErr, stdout.String(), stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	if len(lines) != 2 ||
		!strings.HasPrefix(lines[0], fmt.Sprintf("Port %d is already in use. Another Depsilo may be running", port)) ||
		lines[1] != "Error: daemon exited before readiness: exit status 1 (see "+logPath+")" {
		t.Fatalf("standard error is not the daemon's last message followed by the exit line; stderr=%q", stderr.String())
	}
	logBody, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read daemon log: %v", err)
	}
	if !strings.Contains(string(logBody), lines[0]) {
		t.Fatalf("the printed message is not a line of the startup log; log=%s", logBody)
	}
	if strings.Contains(stdout.String()+stderr.String()+string(logBody), earlierToken) {
		t.Fatalf("the failed start repeated an earlier start's log; stdout=%s stderr=%s log=%s", stdout.String(), stderr.String(), logBody)
	}

	// A configuration the server rejects after the loader accepted it is the
	// daemon's last message too, not the start of a structured log entry.
	t.Run("configuration the server rejects", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		start := exec.CommandContext(ctx, binary, "start", "--daemon", "--host", "127.0.0.1", "--port", strconv.Itoa(availablePort(t)))
		start.Env = append(append([]string{}, env...), "DEPSILO_SERVER_TRUSTED_PROXIES=garbage")
		var stderr strings.Builder
		start.Stderr = &stderr
		runErr := start.Run()
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("start --daemon with an invalid trusted proxy: error=%v, want exit code 1; stderr=%s", runErr, stderr.String())
		}
		want := "Error: configure server.trusted_proxies: invalid IP address: garbage\n" +
			"Error: daemon exited before readiness: exit status 1 (see " + logPath + ")\n"
		if stderr.String() != want {
			t.Fatalf("standard error = %q, want %q", stderr.String(), want)
		}
	})
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

func TestServeExplainsOccupiedPortWithoutStackTrace(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy serve port: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	for _, test := range []struct {
		name       string
		serverURL  string
		wantStatus string
	}{
		{name: "status asks another instance", serverURL: "",
			wantStatus: fmt.Sprintf(`(try "DEPSILO_URL=http://127.0.0.1:%d depsilo status")`, port)},
		{name: "status asks this port", serverURL: fmt.Sprintf("http://localhost:%d", port),
			wantStatus: `(try "depsilo status")`},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "serve",
				"--host", "127.0.0.1", "--port", strconv.Itoa(port))
			cmd.Env = append(os.Environ(),
				cliHelperEnv+"=1",
				"HOME="+t.TempDir(),
				"DEPSILO_CONFIG=",
				"DEPSILO_PORT=",
				"DEPSILO_URL="+test.serverURL,
				"DEPSILO_AUTH_JWT_SECRET=0123456789abcdef0123456789abcdef",
				"DEPSILO_BOOTSTRAP_TOKEN=0123456789abcdef01234567",
			)
			output, runErr := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("serve on an occupied port: error=%v, want exit code 1; output=%s", runErr, output)
			}
			want := fmt.Sprintf("Port %d is already in use. Another Depsilo may be running %s, or choose another port with --port.", port, test.wantStatus)
			explained := 0
			for _, line := range strings.Split(string(output), "\n") {
				if line == want {
					explained++
				}
			}
			if explained != 1 {
				t.Fatalf("serve did not print the plain line %q exactly once; output=%s", want, output)
			}
			for _, noise := range []string{"stacktrace", "failed to start server", "serve.go"} {
				if strings.Contains(string(output), noise) {
					t.Fatalf("serve output still carries %q for an occupied port; output=%s", noise, output)
				}
			}
		})
	}
}

func TestStartupExplainsPermissionFailuresInOneLine(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX file permissions and a non-root user")
	}
	for _, test := range []struct {
		name string
		// prepare makes one path inaccessible under home and returns the
		// extra environment plus the start of the expected line.
		prepare func(t *testing.T, home string) (env []string, wantPrefix string)
		want    string
		reject  string
	}{
		{
			// The shape of a bind mount owned by another user: the state root
			// exists but this user cannot create anything inside it.
			name: "unwritable state directory",
			prepare: func(t *testing.T, home string) ([]string, string) {
				stateDir := filepath.Join(home, ".depsilo")
				if err := os.Mkdir(stateDir, 0o555); err != nil {
					t.Fatal(err)
				}
				return nil, "Permission denied: mkdir " + filepath.Join(stateDir, "data") + "."
			},
			want:   "The state directory must be writable by the user Depsilo runs as",
			reject: "access this path",
		},
		{
			// A config file the service user cannot read is not the state
			// directory, and making a directory writable does not fix it.
			name: "unreadable config file",
			prepare: func(t *testing.T, home string) ([]string, string) {
				configPath := filepath.Join(home, "depsilo.toml")
				if err := os.WriteFile(configPath, []byte("config_version = 1\n"), 0o000); err != nil {
					t.Fatal(err)
				}
				return []string{"DEPSILO_CONFIG=" + configPath}, "Permission denied: open " + configPath + "."
			},
			want:   "Depsilo must be able to access this path as the user it runs as",
			reject: "state directory",
		},
	} {
		for _, args := range [][]string{{"serve"}, {"start", "--daemon"}} {
			t.Run(test.name+"/"+strings.Join(args, " "), func(t *testing.T) {
				home := t.TempDir()
				env, wantPrefix := test.prepare(t, home)

				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)...)
				cmd.Dir = home
				for _, value := range os.Environ() {
					if !strings.HasPrefix(value, "DEPSILO_") {
						cmd.Env = append(cmd.Env, value)
					}
				}
				cmd.Env = append(cmd.Env, cliHelperEnv+"=1", "HOME="+home, "USERPROFILE="+home,
					"DEPSILO_SERVER_PORT="+strconv.Itoa(availablePort(t)))
				cmd.Env = append(cmd.Env, env...)
				output, runErr := cmd.CombinedOutput()
				var exitErr *exec.ExitError
				if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
					t.Fatalf("startup with a permission failure: error=%v, want exit code 1; output=%s", runErr, output)
				}
				text := string(output)
				for _, want := range []string{
					wantPrefix,
					test.want,
					"UID and GID 10001",
					"docs/deployment.md#bind-mounted-state",
				} {
					if !strings.Contains(text, want) {
						t.Fatalf("startup output omits %q; output=%s", want, text)
					}
				}
				for _, noise := range []string{test.reject, "stacktrace", "failed to start server", "serve.go"} {
					if strings.Contains(text, noise) {
						t.Fatalf("startup output carries %q; output=%s", noise, text)
					}
				}
			})
		}
	}
}

// With "--restart unless-stopped" a container repeats whatever a failed start
// prints. A mistake in the configuration must be one line the operator can
// act on, not a structured entry with a Go stack trace.
func TestStartupReportsInvalidConfigurationInOnePlainLine(t *testing.T) {
	runStartup := func(t *testing.T, home string, env []string, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)...)
		cmd.Dir = home
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "DEPSILO_") {
				cmd.Env = append(cmd.Env, value)
			}
		}
		cmd.Env = append(cmd.Env, cliHelperEnv+"=1", "HOME="+home, "USERPROFILE="+home,
			"DEPSILO_SERVER_PORT="+strconv.Itoa(availablePort(t)))
		cmd.Env = append(cmd.Env, env...)
		output, runErr := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("depsilo %v with an invalid configuration: error=%v, want exit code 1; output=%s", args, runErr, output)
		}
		for _, noise := range []string{"stacktrace", "failed to start server", "serve.go", `"level":"error"`} {
			if strings.Contains(string(output), noise) {
				t.Fatalf("depsilo %v output carries %q; output=%s", args, noise, output)
			}
		}
		return string(output)
	}

	const proxyPassword = "proxy-secret-4711"
	weakToken := strings.Repeat("a", 30)
	for _, test := range []struct {
		name   string
		env    string
		want   string
		secret string
	}{
		{
			name:   "predictable bootstrap token",
			env:    "DEPSILO_BOOTSTRAP_TOKEN=" + weakToken,
			want:   "Error: DEPSILO_BOOTSTRAP_TOKEN is too predictable: it has fewer than 8 distinct characters; set it to a random value of 32 bytes or more, or unset it so Depsilo generates a token\n",
			secret: weakToken,
		},
		{
			name: "upstream proxy that is not a URL",
			env:  "DEPSILO_EGRESS_UPSTREAM_PROXY=not a url",
			want: "Error: egress.upstream_proxy: must be an http:// or https:// proxy URL with a host\n",
		},
		{
			name:   "upstream proxy with credentials and a path",
			env:    "DEPSILO_EGRESS_UPSTREAM_PROXY=http://proxy-user:" + proxyPassword + "@proxy.example:3128/pac",
			want:   "Error: egress.upstream_proxy: must not contain a path, query, or fragment\n",
			secret: proxyPassword,
		},
	} {
		for _, args := range [][]string{{"serve"}, {"start", "--daemon"}} {
			t.Run(test.name+"/"+strings.Join(args, " "), func(t *testing.T) {
				output := runStartup(t, t.TempDir(), []string{test.env}, args...)
				if output != test.want {
					t.Fatalf("depsilo %v printed %q, want exactly %q", args, output, test.want)
				}
				if test.secret != "" && strings.Contains(output, test.secret) {
					t.Fatalf("depsilo %v repeated the secret value; output=%s", args, output)
				}
			})
		}
	}

	// The decoder reports its findings as a heading, an empty line and one
	// line per setting; joined, they must read as one sentence.
	t.Run("setting the decoder rejects/serve", func(t *testing.T) {
		output := runStartup(t, t.TempDir(), []string{"DEPSILO_CACHE_TTL_INDEX=forever"}, "serve")
		if strings.Count(output, "\n") != 1 || !strings.HasPrefix(output, "Error: decode config schema: ") ||
			!strings.Contains(output, ": 'cache.ttl_index' ") || strings.Contains(output, ":;") || strings.Contains(output, "; ;") {
			t.Fatalf("serve printed %q, want one line that names cache.ttl_index without empty parts", output)
		}
	})

	// What the loader accepts and the server then rejects. The server logs its
	// progress as structured lines before it fails; the failure itself must be
	// the one plain line.
	assertOnePlainLine := func(t *testing.T, output, want string) {
		t.Helper()
		plain := 0
		for _, line := range strings.Split(output, "\n") {
			switch {
			case line == want:
				plain++
			case line == "", strings.HasPrefix(line, "{"):
			default:
				t.Fatalf("unexpected plain line %q; output=%s", line, output)
			}
		}
		if plain != 1 {
			t.Fatalf("the start did not print the plain line %q exactly once; output=%s", want, output)
		}
	}
	// A configured instance whose database has no administrator yet.
	configuredHome := func(t *testing.T) string {
		t.Helper()
		home := t.TempDir()
		stateDir := filepath.Join(home, ".depsilo")
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		document := "config_version = 1\n[database]\ndsn = '" + filepath.Join(stateDir, "data", "depsilo.db") + "'\n" +
			"[storage]\npath = '" + filepath.Join(stateDir, "data", "cache") + "'\n"
		if err := os.WriteFile(filepath.Join(stateDir, "config.toml"), []byte(document), 0o600); err != nil {
			t.Fatal(err)
		}
		return home
	}

	for _, test := range []struct {
		name string
		env  string
		want string
	}{
		{
			name: "trusted proxy that is not an address",
			env:  "DEPSILO_SERVER_TRUSTED_PROXIES=garbage",
			want: "Error: configure server.trusted_proxies: invalid IP address: garbage",
		},
		{
			name: "port out of range",
			env:  "DEPSILO_SERVER_PORT=99999",
			want: "Error: listen on 127.0.0.1:99999: listen tcp: address 99999: invalid port",
		},
	} {
		t.Run(test.name+"/serve", func(t *testing.T) {
			assertOnePlainLine(t, runStartup(t, t.TempDir(), []string{test.env}, "serve"), test.want)
		})
	}

	// With a configuration file and no administrator the server reopens the
	// interactive setup, and only then reads the bootstrap token.
	t.Run("predictable bootstrap token for a configured instance/serve", func(t *testing.T) {
		output := runStartup(t, configuredHome(t), []string{"DEPSILO_BOOTSTRAP_TOKEN=" + weakToken}, "serve")
		assertOnePlainLine(t, output, "Error: reconcile initial administrator: reopen interactive setup: "+
			"DEPSILO_BOOTSTRAP_TOKEN is too predictable: it has fewer than 8 distinct characters; "+
			"set it to a random value of 32 bytes or more, or unset it so Depsilo generates a token")
		if strings.Contains(output, weakToken) {
			t.Fatalf("serve repeated the bootstrap token; output=%s", output)
		}
	})

	// The administrator from the environment is created only when a
	// configuration file exists, after the database is open.
	t.Run("administrator password that breaks the policy/serve", func(t *testing.T) {
		const weakPassword = "Sh0rt!pw"
		policyErr := api.ValidateInitialAdminCredentials("admin", weakPassword)
		if policyErr == nil {
			t.Fatal("the test password satisfies the credential policy")
		}
		output := runStartup(t, configuredHome(t), []string{"DEPSILO_ADMIN_PASSWORD=" + weakPassword}, "serve")
		assertOnePlainLine(t, output, "Error: ensure initial administrator: "+policyErr.Error()+
			" (set by DEPSILO_ADMIN_USERNAME and DEPSILO_ADMIN_PASSWORD)")
		if strings.Contains(output, weakPassword) {
			t.Fatalf("serve repeated the password; output=%s", output)
		}
	})
}

// A hand-written config.toml that leaves the database and storage paths out
// puts them into the working directory, which the official container image
// does not let the service user write. Volume ownership is not the cause, so
// the line must not send the operator there.
func TestStartupExplainsRelativeConfigPathsInAnUnwritableWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX file permissions and a non-root user")
	}
	const relativeHint = "Relative paths in config.toml resolve against the working directory, "
	const ownershipHint = "The state directory must be writable by the user Depsilo runs as; " +
		"the official container image runs as UID and GID 10001. See docs/deployment.md#bind-mounted-state"

	for _, test := range []struct {
		name string
		// prepare lays out root and returns the working directory, the
		// environment, and the line the start must print.
		prepare func(t *testing.T, root string) (workingDirectory string, env []string, want string)
	}{
		{
			name: "relative defaults outside the state directory",
			prepare: func(t *testing.T, root string) (string, []string, string) {
				configPath := filepath.Join(root, ".depsilo", "config.toml")
				writeStartupTestFile(t, configPath, "config_version = 1\n")
				workingDirectory := filepath.Join(root, "app")
				if err := os.Mkdir(workingDirectory, 0o555); err != nil {
					t.Fatal(err)
				}
				return workingDirectory, nil, "Permission denied: mkdir " + filepath.Join(workingDirectory, "data") + ". " +
					relativeHint + workingDirectory + "; set absolute paths in " + configPath +
					" for database.dsn, storage.path (a setting left out counts: its default is relative). See docs/deployment.md#default-paths"
			},
		},
		{
			name: "only the storage path is relative",
			prepare: func(t *testing.T, root string) (string, []string, string) {
				configPath := filepath.Join(root, ".depsilo", "config.toml")
				writeStartupTestFile(t, configPath, "config_version = 1\n[database]\ndsn = '"+
					filepath.Join(root, ".depsilo", "data", "depsilo.db")+"'\n[storage]\npath = 'cache'\n")
				workingDirectory := filepath.Join(root, "app")
				if err := os.Mkdir(workingDirectory, 0o555); err != nil {
					t.Fatal(err)
				}
				return workingDirectory, nil, "Permission denied: mkdir " + filepath.Join(workingDirectory, "cache") + ". " +
					relativeHint + workingDirectory + "; set absolute paths in " + configPath +
					" for storage.path (a setting left out counts: its default is relative). See docs/deployment.md#default-paths"
			},
		},
		{
			// The working directory holds config.toml: it is the state
			// directory, and it must be writable.
			name: "relative defaults in the state directory",
			prepare: func(t *testing.T, root string) (string, []string, string) {
				workingDirectory := filepath.Join(root, "app")
				writeStartupTestFile(t, filepath.Join(workingDirectory, "config.toml"), "config_version = 1\n")
				if err := os.Chmod(workingDirectory, 0o555); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(workingDirectory, 0o700) })
				return workingDirectory, []string{"DEPSILO_AUTH_JWT_SECRET=0123456789abcdef0123456789abcdef"},
					"Permission denied: mkdir " + filepath.Join(workingDirectory, "data") + ". " + ownershipHint
			},
		},
		{
			// Absolute paths that happen to lie below the working directory.
			name: "absolute paths inside the working directory",
			prepare: func(t *testing.T, root string) (string, []string, string) {
				workingDirectory := filepath.Join(root, "app")
				locked := filepath.Join(workingDirectory, "volume")
				if err := os.Mkdir(workingDirectory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(locked, 0o555); err != nil {
					t.Fatal(err)
				}
				writeStartupTestFile(t, filepath.Join(root, ".depsilo", "config.toml"), "config_version = 1\n[database]\ndsn = '"+
					filepath.Join(locked, "data", "depsilo.db")+"'\n[storage]\npath = '"+filepath.Join(locked, "data", "cache")+"'\n")
				return workingDirectory, nil, "Permission denied: mkdir " + filepath.Join(locked, "data") + ". " + ownershipHint
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			workingDirectory, env, want := test.prepare(t, root)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIHelperProcess$", "--", "serve")
			cmd.Dir = workingDirectory
			for _, value := range os.Environ() {
				if !strings.HasPrefix(value, "DEPSILO_") {
					cmd.Env = append(cmd.Env, value)
				}
			}
			cmd.Env = append(cmd.Env, cliHelperEnv+"=1", "HOME="+root, "USERPROFILE="+root,
				"DEPSILO_SERVER_PORT="+strconv.Itoa(availablePort(t)))
			cmd.Env = append(cmd.Env, env...)
			output, runErr := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 1 {
				t.Fatalf("serve: error=%v, want exit code 1; output=%s", runErr, output)
			}
			explained := 0
			for _, line := range strings.Split(string(output), "\n") {
				if line == want {
					explained++
				} else if strings.HasPrefix(line, "Permission denied") {
					t.Fatalf("serve explained the failure as %q, want %q", line, want)
				}
			}
			if explained != 1 || strings.Contains(string(output), "stacktrace") {
				t.Fatalf("serve did not print the line %q exactly once without a stack trace; output=%s", want, output)
			}
		})
	}
}

func writeStartupTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
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

// The binary's entry point must accept every command its own help advertises.
// `depsilo audit verify` shipped unreachable for four releases because the
// entry point kept a second command list that this test now guards against.
func TestBinaryDispatchesEveryCommandNamedInHelp(t *testing.T) {
	binary := buildDepsiloCLI(t)
	// Commands that talk to a server reach this stub instead of a real
	// instance; it answers 404 to everything and records what it was asked.
	var requestsMu sync.Mutex
	var requests []string
	service := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestsMu.Lock()
		requests = append(requests, request.Method+" "+request.URL.Path)
		requestsMu.Unlock()
		http.NotFound(response, request)
	}))
	defer service.Close()
	receivedRequests := func() []string {
		requestsMu.Lock()
		defer requestsMu.Unlock()
		return append([]string(nil), requests...)
	}
	home := t.TempDir()
	workDir := t.TempDir()
	env := append(os.Environ(),
		cliHelperEnv+"=",
		"HOME="+home,
		"USERPROFILE="+home,
		"DEPSILO_CONFIG=",
		"DEPSILO_URL="+service.URL,
		"DEPSILO_SERVER_PORT="+strconv.Itoa(setupServerPort(t, service)),
		"DEPSILO_TOKEN=",
		"DEPSILO_BOOTSTRAP_TOKEN=",
	)
	run := func(t *testing.T, args ...string) (string, string, int) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, binary, args...)
		command.Env = env
		command.Dir = workDir
		var stdout, stderr strings.Builder
		command.Stdout = &stdout
		command.Stderr = &stderr
		err := command.Run()
		if ctx.Err() != nil {
			t.Fatalf("depsilo %v did not return: %v", args, ctx.Err())
		}
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatalf("run depsilo %v: %v", args, err)
		}
		return stdout.String(), stderr.String(), command.ProcessState.ExitCode()
	}

	help, _, exitCode := run(t, "help")
	if exitCode != 0 {
		t.Fatalf("help exit code = %d, want 0", exitCode)
	}
	names := helpCommandNames(help)
	if len(names) == 0 {
		t.Fatalf("help output has no Commands block; output=%s", help)
	}
	// Asking for help must never do the command's work. Every command is
	// asked alone; the others are asked again with a documented subcommand or
	// with the arguments that would otherwise make the handler act.
	invocations := make([][]string, 0, len(names)+24)
	for _, name := range names {
		invocations = append(invocations, []string{name})
	}
	invocations = append(invocations,
		[]string{"server"},
		[]string{"setup", "token"},
		[]string{"audit", "verify"},
		[]string{"start", "--daemon"},
		[]string{"flush", "--json"},
		[]string{"warmup", "pypi", "requests"},
		[]string{"backup", "--out", "backup.tar.gz"},
		[]string{"restore", "backup.tar.gz"},
		[]string{"diagnose", "--out", "report.json"},
		[]string{"init-agent", "--format", "all"},
		[]string{"reset-password", "admin"},
		[]string{"activate", "--shell", "fish"},
	)
	for _, subcommand := range []string{"stage", "list", "prune", "usage", "inspect", "verify", "delete", "export", "import", "keygen"} {
		invocations = append(invocations, []string{"distribution", subcommand})
	}
	invocations = append(invocations, []string{"distribution", "keygen", "--out", "transfer.key"})
	for _, invocation := range invocations {
		for _, flag := range []string{"--help", "-h"} {
			args := append(append([]string(nil), invocation...), flag)
			t.Run(strings.Join(args, " "), func(t *testing.T) {
				stdout, stderr, exitCode := run(t, args...)
				if strings.Contains(stderr, "Unknown command:") {
					t.Fatalf("help advertises %q but the binary rejects it; stdout=%s stderr=%s", args[0], stdout, stderr)
				}
				if exitCode != 0 || stderr != "" {
					t.Fatalf("depsilo %v: exit=%d stderr=%q, want 0 and nothing on standard error; stdout=%s", args, exitCode, stderr, stdout)
				}
				name := args[0]
				if name == "server" {
					name = "serve"
				}
				if name == "help" {
					if stdout != help {
						t.Fatalf("depsilo %v did not print the top-level help; stdout=%s", args, stdout)
					}
				} else if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "depsilo "+name) {
					t.Fatalf("depsilo %v did not print the usage of %q on standard output; stdout=%s", args, name, stdout)
				}
				if got := receivedRequests(); len(got) != 0 {
					t.Fatalf("depsilo %v sent requests while printing help: %v", args, got)
				}
				for _, directory := range []string{home, workDir} {
					entries, err := os.ReadDir(directory)
					if err != nil {
						t.Fatalf("read %s: %v", directory, err)
					}
					if len(entries) != 0 {
						t.Fatalf("depsilo %v created %s in %s while printing help", args, entries[0].Name(), directory)
					}
				}
			})
		}
	}

	t.Run("unknown command prints the help and exits 1", func(t *testing.T) {
		stdout, stderr, exitCode := run(t, "no-such-command")
		if exitCode != 1 || stderr != "Unknown command: no-such-command\n\n" || stdout != help {
			t.Fatalf("unknown command: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
	})
	t.Run("no arguments and help aliases print the help", func(t *testing.T) {
		for _, args := range [][]string{nil, {"--help"}, {"-h"}} {
			stdout, stderr, exitCode := run(t, args...)
			if exitCode != 0 || stdout != help || stderr != "" {
				t.Fatalf("depsilo %v: exit=%d stdout=%q stderr=%q", args, exitCode, stdout, stderr)
			}
		}
	})
	t.Run("version aliases", func(t *testing.T) {
		version, _, exitCode := run(t, "version")
		if exitCode != 0 || !strings.HasPrefix(version, "Depsilo ") {
			t.Fatalf("version: exit=%d stdout=%q", exitCode, version)
		}
		for _, alias := range []string{"--version", "-v"} {
			if stdout, _, exitCode := run(t, alias); exitCode != 0 || stdout != version {
				t.Fatalf("depsilo %s: exit=%d stdout=%q, want %q", alias, exitCode, stdout, version)
			}
		}
	})
	t.Run("server is an alias of serve", func(t *testing.T) {
		serveOut, serveErr, serveExit := run(t, "serve", "--help")
		serverOut, serverErr, serverExit := run(t, "server", "--help")
		if !strings.Contains(serveOut, "depsilo serve [flags]") {
			t.Fatalf("serve --help did not print the serve usage; stdout=%s stderr=%s", serveOut, serveErr)
		}
		if serverOut != serveOut || serverErr != serveErr || serverExit != serveExit {
			t.Fatalf("server --help differs from serve --help; stdout=%s stderr=%s exit=%d", serverOut, serverErr, serverExit)
		}
	})
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
