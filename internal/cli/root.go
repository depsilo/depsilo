package cli

import (
	"fmt"
	"os"
)

const (
	defaultPort = "23333"
	defaultHost = "http://localhost:" + defaultPort
)

// Run dispatches to the appropriate command handler.
// args is os.Args[2:] — the subcommand's arguments.
// Returns exit code (0 for success, 1 for error).
func Run(cmd string, args []string) int {
	switch cmd {
	case "status":
		return runStatus(args)
	case "activate":
		return runActivate(args)
	case "start":
		return runStart(args)
	case "stop":
		return runStop(args)
	case "warmup":
		return runWarmup(args)
	case "flush":
		return runFlush(args)
	case "backup":
		return runBackup(args)
	case "restore":
		return runRestore(args)
	case "version":
		return runVersion(args)
	case "doctor":
		return runDoctor(args)
	case "init-agent":
		return runInitAgent(args)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		return 1
	}
}

func PrintHelp() {
	fmt.Println(`Depsilo — Lightweight dependency proxy cache gateway

Usage:
    depsilo [command]

Commands:
    serve                       Start HTTP server in foreground
    start [--daemon]            Start the server (daemon mode with --daemon)
    stop                        Stop the running daemon
    status [--json]             Show server health, cache stats, upstreams
    doctor [--json]             Run end-to-end health diagnosis with hints
    init-agent [--format ...]   Write CLAUDE.md / AGENTS.md / .cursorrules so
                                AI coding agents auto-detect Depsilo
    version [--json]            Print version
    activate [--shell|--eco]    Print shell environment configuration
    warmup <eco> <pkg> [pkg...] Pre-fetch packages into cache
    flush                       Clear expired cache entries
    backup [--out file.tar.gz]  Backup config + database to archive
    restore <backup.tar.gz>     Restore config + database from backup
    help                        Show this message

Global flags:
    --json, -j                  Output machine-readable JSON (status, version, warmup)

Examples:
    depsilo status
    depsilo status --json
    depsilo doctor
    depsilo activate
    eval "$(depsilo activate)"
    depsilo start --daemon
    depsilo warmup pypi requests numpy torch

HTTP endpoints for AI agents and automation:
    GET  /health                Liveness probe (JSON)
    GET  /api/v1/discover       Self-describing service + endpoint catalog (JSON)
    GET  /api/v1/agent-prompt   Copy-paste prompt for AI coding agents (text/plain)
    GET  /api/v1/stats          Public usage stats (JSON)
    GET  /metrics               Prometheus metrics`)
}

// getServerURL returns the Depsilo server URL, respecting DEPSILO_URL and DEPSILO_PORT env vars.
func getServerURL() string {
	if u := os.Getenv("DEPSILO_URL"); u != "" {
		return u
	}
	if p := os.Getenv("DEPSILO_PORT"); p != "" {
		return "http://localhost:" + p
	}
	return defaultHost
}

// stripJSONFlag scans args for --json / -j, returns whether it was present
// and the filtered args (with the flag removed). Subcommands call this at
// the start of their handler to honor the global --json flag uniformly.
func stripJSONFlag(args []string) (bool, []string) {
	out := make([]string, 0, len(args))
	jsonMode := false
	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonMode = true
			continue
		}
		out = append(out, a)
	}
	return jsonMode, out
}

// getToken returns the auth token from environment or config file.
func getToken() string {
	if t := os.Getenv("DEPSILO_TOKEN"); t != "" {
		return t
	}
	// Read from ~/.config/depsilo/token if exists
	data, err := os.ReadFile(os.ExpandEnv("$HOME/.config/depsilo/token"))
	if err == nil {
		return string(data)
	}
	return ""
}
