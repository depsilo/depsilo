package main

import (
	"fmt"
	"os"

	"depsilo/internal/cli"
)

func main() {
	// CLI mode: dispatch by subcommand. Server mode is one subcommand
	// among many — its flag parsing + lifecycle now live in
	// internal/cli/serve.go, matching the layout of every other
	// command. main() stays thin: parse, dispatch, exit.
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "serve", "server":
			os.Exit(cli.RunServe(os.Args[2:]))
		case "status", "doctor", "init-agent", "prompt", "activate", "start", "stop", "warmup", "flush", "backup", "restore", "version":
			os.Exit(cli.Run(cmd, os.Args[2:]))
		case "help", "--help", "-h":
			cli.PrintHelp()
			os.Exit(0)
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
			cli.PrintHelp()
			os.Exit(1)
		}
	}
	// No arguments — show help instead of silently starting a server.
	// Safer for AI agents exploring the binary; use `depsilo serve`
	// or `depsilo start --daemon` to actually run the service.
	cli.PrintHelp()
	os.Exit(0)
}
