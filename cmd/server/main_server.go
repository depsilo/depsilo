package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"depsilo/internal/logging"
	"depsilo/internal/server"
)

func main() {
	// Initialize logger
	logger, logLevel, err := logging.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := server.StartServer(ctx, logLevel)
	if err != nil {
		zap.L().Fatal("failed to start server", zap.Error(err))
	}

	<-ctx.Done()
	zap.L().Info("shutting down server...")
	if err := server.Shutdown(context.Background(), srv); err != nil {
		zap.L().Error("server shutdown error", zap.Error(err))
	}
}
