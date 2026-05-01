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
	case "version":
		return runVersion(args)
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
    serve              Start HTTP server in foreground (default)
    status             Show server health, cache stats, upstreams
    activate           Print shell environment configuration
    start [--daemon]   Start the server (daemon mode with --daemon)
    stop               Stop the running daemon
    warmup <eco> <pkg> [pkg...]  Pre-fetch packages into cache
    flush              Clear expired cache entries
    version            Print version

Examples:
    depsilo status
    depsilo activate
    depsilo start --daemon
    eval "$(depsilo activate)"
    depsilo warmup pypi requests numpy torch`)
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
