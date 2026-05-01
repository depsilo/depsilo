package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/server"
)

// ── runStart ────────────────────────────────────────────────────

func runStart(args []string) int {
	daemon := false
	for _, a := range args {
		if a == "--daemon" || a == "-d" {
			daemon = true
			break
		}
	}

	if !daemon {
		// Foreground: start server directly
		logger, err := zap.NewProduction()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
			return 1
		}
		zap.ReplaceGlobals(logger)
		defer logger.Sync()

		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()

		srv, err := server.StartServer(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}

		<-ctx.Done()
		fmt.Println("\nShutting down server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
		return 0
	}

	// Daemon mode
	pidDir := getPIDDir()
	if err := os.MkdirAll(pidDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot create PID directory: %v\n", err)
		return 1
	}
	pidFile := filepath.Join(pidDir, "depsilo.pid")

	// Check if already running
	if pid, err := readPID(pidFile); err == nil && processExists(pid) {
		fmt.Printf("Depsilo already running (PID %d)\n", pid)
		return 0
	}
	os.Remove(pidFile)

	// Start child process
	cmd := exec.Command(os.Args[0], "serve")
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
		return 1
	}

	// Write PID file
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write PID file: %v\n", err)
	}

	// Wait for server to be ready
	fmt.Printf("Depsilo starting (PID %d)...", cmd.Process.Pid)
	for i := 0; i < 30; i++ {
		conn, err := net.DialTimeout("tcp", "localhost:"+defaultPort, 500*time.Millisecond)
		if err == nil {
			conn.Close()
			fmt.Println(" ready!")
			return 0
		}
		time.Sleep(200 * time.Millisecond)
		fmt.Print(".")
	}
	fmt.Println()
	fmt.Fprintf(os.Stderr, "Warning: server started but not yet responding. Check logs.\n")
	return 0
}

// ── runStop ─────────────────────────────────────────────────────

func runStop(args []string) int {
	pidFile := filepath.Join(getPIDDir(), "depsilo.pid")
	pid, err := readPID(pidFile)
	if err != nil {
		// Also try .server.pid in CWD (legacy Makefile convention)
		cwdPidFile := ".server.pid"
		if pid2, err2 := readPID(cwdPidFile); err2 == nil && processExists(pid2) {
			pid = pid2
			pidFile = cwdPidFile
		} else {
			fmt.Fprintln(os.Stderr, "Depsilo is not running")
			return 1
		}
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Depsilo is not running")
		os.Remove(pidFile)
		return 1
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping Depsilo: %v\n", err)
		// Process might already be dead
		os.Remove(pidFile)
		return 1
	}

	// Wait for process to exit
	fmt.Printf("Stopping Depsilo (PID %d)...", pid)
	for i := 0; i < 15; i++ {
		if !processExists(pid) {
			fmt.Println(" stopped!")
			os.Remove(pidFile)
			return 0
		}
		time.Sleep(200 * time.Millisecond)
		fmt.Print(".")
	}
	fmt.Println()
	fmt.Fprintln(os.Stderr, "Warning: process did not exit in time, sending SIGKILL")
	process.Kill()
	os.Remove(pidFile)
	return 0
}

// ── PID file helpers ────────────────────────────────────────────

func getPIDDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "depsilo")
	}
	return filepath.Join(home, ".local", "share", "depsilo")
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func processExists(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds. Signal 0 checks existence.
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return false
	}
	return true
}
