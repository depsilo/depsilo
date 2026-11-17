package cli

import (
	"fmt"
	"os"
	"strings"
)

const (
	defaultPort = "23333"
	defaultHost = "http://localhost:" + defaultPort
)

// command is one top-level subcommand of the depsilo binary.
type command struct {
	name    string
	aliases []string
	run     func(args []string) int
}

// commands is the only list of top-level commands. Main and Run both dispatch
// through it, so a command added here is reachable from the binary without a
// second edit in cmd/depsilo. Every entry also belongs in the "Commands:"
// block of helpText; a test keeps the two in step.
var commands = []command{
	{name: "serve", aliases: []string{"server"}, run: RunServe},
	{name: "start", run: runStart},
	{name: "stop", run: runStop},
	{name: "setup", run: runSetup},
	{name: "status", run: runStatus},
	{name: "doctor", run: runDoctor},
	{name: "diagnose", run: runDiagnose},
	{name: "init-agent", run: runInitAgent},
	{name: "prompt", run: runPrompt},
	{name: "version", aliases: []string{"--version", "-v"}, run: runVersion},
	{name: "activate", run: runActivate},
	{name: "warmup", run: runWarmup},
	{name: "flush", run: runFlush},
	{name: "backup", run: runBackup},
	{name: "restore", run: runRestore},
	{name: "distribution", run: runDistribution},
	{name: "audit", run: runAudit},
	{name: "help", aliases: []string{"--help", "-h"}, run: runHelp},
}

// lookupCommand returns the handler for a command name or alias.
func lookupCommand(name string) (func(args []string) int, bool) {
	for _, c := range commands {
		if c.name == name {
			return c.run, true
		}
		for _, alias := range c.aliases {
			if alias == name {
				return c.run, true
			}
		}
	}
	return nil, false
}

// Main is the whole behavior of the depsilo binary. args is os.Args[1:].
// Returns the process exit code.
func Main(args []string) int {
	if len(args) == 0 {
		// No arguments — show help instead of silently starting a server.
		// Safer for AI agents exploring the binary; use `depsilo serve`
		// or `depsilo start --daemon` to actually run the service.
		PrintHelp()
		return 0
	}
	run, ok := lookupCommand(args[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", args[0])
		PrintHelp()
		return 1
	}
	return run(args[1:])
}

// Run dispatches to the appropriate command handler.
// args is os.Args[2:] — the subcommand's arguments.
// Returns exit code (0 for success, 1 for error).
func Run(cmd string, args []string) int {
	run, ok := lookupCommand(cmd)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		return 1
	}
	return run(args)
}

func runHelp(_ []string) int {
	PrintHelp()
	return 0
}

func PrintHelp() {
	fmt.Println(helpText)
}

const helpText = `Depsilo - self-hosted dependency cache and distribution service

Usage:
    depsilo <command> [flags]
    depsilo <command> --help        Show flags for a specific command
    depsilo --version               Print version

Commands:
    serve [flags]               Start HTTP server in foreground (see ` + "`depsilo serve --help`" + `)
    start [--daemon]            Start the server (daemon mode with --daemon)
    stop                        Stop the running daemon
    setup token [flags]         Print the running instance's bootstrap token
    status [--json]             Show server health, cache stats, upstreams
    doctor [--json]             Run end-to-end health diagnosis with hints
    diagnose [--out file]       Write a local, redacted diagnostic report
    init-agent [--format ...]   Write CLAUDE.md / AGENTS.md / .cursorrules so
                                AI coding agents auto-detect Depsilo
    prompt [--url ...]          Print the project-integration prompt for an
                                AI coding agent (Dockerfile/CI/build rewrite)
    version [--json]            Print version
    activate [--shell|--eco]    Print shell environment configuration
    warmup <eco> <pkg> [pkg...] Pre-fetch packages into cache
    flush                       Clear expired cache entries
    backup [--out file.tar.gz]  Online backup of config + SQLite state
    restore <backup.tar.gz>     Validated restore; target server must be stopped
    distribution <command>     Prepare and verify durable dependency-set drafts
    audit verify [--json]       Recompute the tamper-evident audit hash chain
    help                        Show this message

Common serve flags (use depsilo serve --help for full detail):
    --port, -p N                Listen port (overrides config; default 23333)
    --host H                    Listen host (overrides config; default 127.0.0.1)
    --config, -c PATH           Path to config.toml
    --log-level L               debug | info | warn | error

Other flags:
    --json, -j                  Machine-readable JSON output. Accepted by: status, doctor,
                                diagnose, init-agent, version, warmup, flush, backup,
                                restore, distribution, audit verify

Environment variables:
    DEPSILO_URL                 Used by status / doctor to find a remote server
    DEPSILO_TOKEN               Auth token (alternative to ~/.config/depsilo/token)
    DEPSILO_CONFIG              Path to config.toml (default search: ./config.toml,
                                /app/config.toml, ~/.depsilo/config.toml)
    DEPSILO_SERVER_PORT         Override [server] port (12-factor style)
    DEPSILO_SERVER_HOST         Override [server] host
    DEPSILO_SERVER_LOG_LEVEL    Override [server] log_level
    DEPSILO_LICENSE_KEY         Pro license key (alternative to [license] key)

Precedence (highest wins):
    CLI flag → environment variable → config file → built-in default

Examples:
    depsilo serve                                Start with the default port (23333)
    depsilo serve --port 18080                   Override only the port
    depsilo serve -p 8080 -c /etc/depsilo.toml   Custom port + config
    DEPSILO_SERVER_PORT=9000 depsilo serve       Same effect, via env
    depsilo status                               Quick health check
    depsilo status --json                        Same, machine-readable
    depsilo doctor                               Full end-to-end diagnosis
    eval "$(depsilo activate)"                   Configure local shell to use Depsilo
    depsilo warmup pypi requests numpy torch     Pre-warm cache

HTTP endpoints for AI agents and automation:
    GET  /health                Liveness probe (JSON)
    GET  /api/v1/discover       Self-describing service + endpoint catalog (JSON)
    GET  /api/v1/agent-prompt   Copy-paste prompt for AI coding agents (text/plain)
    GET  /api/v1/stats          Public usage stats (JSON)
    GET  /metrics               Prometheus metrics`

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
// Surrounding whitespace is dropped: a token file written by an editor or
// `echo` ends in a newline, which is not valid in an Authorization header.
func getToken() string {
	if t := strings.TrimSpace(os.Getenv("DEPSILO_TOKEN")); t != "" {
		return t
	}
	// Read from ~/.config/depsilo/token if exists
	data, err := os.ReadFile(os.ExpandEnv("$HOME/.config/depsilo/token"))
	if err == nil {
		return strings.TrimSpace(string(data))
	}
	return ""
}
