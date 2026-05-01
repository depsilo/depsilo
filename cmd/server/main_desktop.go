//go:build desktop

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"go.uber.org/zap"

	"depsilo/internal/server"
	web "depsilo/web"
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

	// Check for --headless flag
	for _, arg := range os.Args[1:] {
		if arg == "--headless" {
			runHeadless()
			return
		}
	}

	// Desktop mode: start server + Wails window
	ctx, cancel := context.WithCancel(context.Background())

	srv, err := server.StartServer(ctx)
	if err != nil {
		zap.L().Fatal("failed to start server", zap.Error(err))
	}

	zap.L().Info("starting desktop mode")

	err = wails.Run(&options.App{
		Title:            "Depsilo",
		Width:            1280,
		Height:           800,
		MinWidth:         800,
		MinHeight:        600,
		DisableResize:    false,
		Fullscreen:       false,
		StartHidden:      false,
		BackgroundColour: &options.RGBA{R: 255, G: 255, B: 255, A: 1},
		AssetServer: &assetserver.Options{
			Assets: web.DistFS,
		},
		OnShutdown: func(_ context.Context) {
			zap.L().Info("desktop window closed, shutting down server...")
			srv.Shutdown(context.Background())
			cancel()
		},
	})
	if err != nil {
		zap.L().Fatal("wails failed", zap.Error(err))
	}
}

// runHeadless starts the server without the Wails desktop window.
func runHeadless() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := server.StartServer(ctx)
	if err != nil {
		zap.L().Fatal("failed to start server", zap.Error(err))
	}

	zap.L().Info("running in headless mode (no GUI)")

	<-ctx.Done()
	zap.L().Info("shutting down server...")
	if err := srv.Shutdown(context.Background()); err != nil {
		zap.L().Error("server shutdown error", zap.Error(err))
	}
}
