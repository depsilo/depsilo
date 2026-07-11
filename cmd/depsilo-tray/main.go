// Command depsilo-tray runs Depsilo as a menu-bar (tray) application.
// It starts the HTTP server in-process and renders a tray icon that
// shows live service status, plus shortcuts to open the admin / portal
// UIs and quit the service.
//
// Build:
//
//	go build -o bin/depsilo-tray ./cmd/depsilo-tray
//
// macOS app bundle: see `make app-macos`.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"fyne.io/systray"
	"go.uber.org/zap"

	"depsilo/internal/logging"
	"depsilo/internal/server"
	"depsilo/internal/tray"
)

func main() {
	logger, logLevel, err := logging.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		os.Exit(1)
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	// Cancellable context drives the in-process HTTP server lifecycle.
	ctx, cancel := context.WithCancel(context.Background())

	srv, err := server.StartServer(ctx, logLevel)
	if err != nil {
		zap.L().Fatal("failed to start server", zap.Error(err))
	}

	shutdown := func() {
		zap.L().Info("tray: shutting down server")
		cancel()
		// Give the server up to 5s to drain in-flight requests.
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := server.Shutdown(stopCtx, srv); err != nil {
			zap.L().Warn("server shutdown returned error", zap.Error(err))
		}
	}

	baseURL := serverURL()

	// systray.Run captures the calling goroutine (main thread on macOS) and
	// returns only when systray.Quit is called from the menu handler.
	systray.Run(
		func() {
			tray.Setup(tray.Options{
				BaseURL:  baseURL,
				Shutdown: shutdown,
			})
		},
		func() {
			zap.L().Info("tray: exiting")
		},
	)
}

// serverURL respects DEPSILO_URL / DEPSILO_PORT so the tray can also
// point at a remote daemon — useful when Depsilo runs in a homelab
// container and you want the tray on your laptop.
func serverURL() string {
	if u := os.Getenv("DEPSILO_URL"); u != "" {
		return u
	}
	if p := os.Getenv("DEPSILO_PORT"); p != "" {
		return "http://localhost:" + p
	}
	return "http://localhost:23333"
}
