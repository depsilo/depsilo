//go:build !desktop

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

func main() {
	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := StartServer(ctx)
	if err != nil {
		zap.L().Fatal("failed to start server", zap.Error(err))
	}

	<-ctx.Done()
	zap.L().Info("shutting down server...")
	if err := srv.Shutdown(context.Background()); err != nil {
		zap.L().Error("server shutdown error", zap.Error(err))
	}
}
