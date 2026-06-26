package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"depsilo/internal/cli"
	"depsilo/internal/server"
)

func main() {
	// CLI mode: dispatch by subcommand.
	if len(os.Args) > 1 {
		cmd := os.Args[1]
		switch cmd {
		case "serve", "server":
			// Fall through to server mode
		case "status", "doctor", "init-agent", "prompt", "activate", "start", "stop", "warmup", "flush", "backup", "restore", "version":
			os.Exit(cli.Run(cmd, os.Args[2:]))
		case "help", "--help", "-h":
			cli.PrintHelp()
			os.Exit(0)
		default:
			// Unknown command
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
			cli.PrintHelp()
			os.Exit(1)
		}
	} else {
		// No arguments — show help instead of silently starting a server.
		// Use `depsilo serve` (or `depsilo start --daemon`) to start the server.
		// This change is safer for AI agents exploring the binary.
		cli.PrintHelp()
		os.Exit(0)
	}

	// Server mode (entered only via explicit "serve"/"server" subcommand)
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := server.StartServer(ctx)
	if err != nil {
		zap.L().Fatal("failed to start server", zap.Error(err))
	}

	<-ctx.Done()
	zap.L().Info("shutting down server...")
	if err := srv.Shutdown(context.Background()); err != nil {
		zap.L().Error("server shutdown error", zap.Error(err))
	}
}
