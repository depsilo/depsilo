package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"go.uber.org/zap"

	"depsilo/internal/logging"
	"depsilo/internal/server"
)

// ServeOptions captures the command-line shape of `depsilo serve`.
// Flags map 1:1 to config fields under [server]. When a flag is left
// at its zero value, the corresponding config / env / default takes
// over. Explicit flags overwrite via os.Setenv so the loader's
// AutomaticEnv hook picks them up — keeps the override path in one
// place (the loader) instead of fanning out into every consumer.
type ServeOptions struct {
	Port       int    // 0 = use config / DEPSILO_SERVER_PORT / default 23333
	Host       string // empty = use config / DEPSILO_SERVER_HOST / default 0.0.0.0
	ConfigPath string // empty = search standard paths (./config.toml, ~/.depsilo/...)
	LogLevel   string // empty = use config / default info
}

// ParseServeFlags processes the args for `depsilo serve`. Returns
// the parsed options + nil error on success. On --help / -h returns
// (nil, flag.ErrHelp) — caller should NOT treat that as a failure;
// the help text was already printed to stdout via the FlagSet.
//
// The output writer is injectable so tests can capture without
// touching os.Stdout / Stderr. Production callers pass os.Stderr
// because flag's default error messages target Stderr.
func ParseServeFlags(args []string, out io.Writer) (*ServeOptions, error) {
	fs := flag.NewFlagSet("depsilo serve", flag.ContinueOnError)
	fs.SetOutput(out)

	opts := &ServeOptions{}

	fs.IntVar(&opts.Port, "port", 0, "Listen port (overrides config; default 23333)")
	fs.IntVar(&opts.Port, "p", 0, "Alias for --port")
	fs.StringVar(&opts.Host, "host", "", "Listen host (overrides config; default 0.0.0.0)")
	fs.StringVar(&opts.ConfigPath, "config", "", "Path to config.toml (overrides DEPSILO_CONFIG and the standard search paths)")
	fs.StringVar(&opts.ConfigPath, "c", "", "Alias for --config")
	fs.StringVar(&opts.LogLevel, "log-level", "", "Log level: debug | info | warn | error (overrides config; default info)")

	// Custom Usage so the user sees serve-scoped help instead of the
	// program-wide help when they pass --help to serve specifically.
	fs.Usage = func() {
		fmt.Fprintln(out, `Usage:
    depsilo serve [flags]

Start the HTTP server in the foreground. Without flags, configuration
comes from (in order): DEPSILO_CONFIG → ./config.toml → ~/.depsilo/
config.toml → built-in defaults.

Flags:`)
		fs.PrintDefaults()
		fmt.Fprintln(out, `
Precedence (highest wins):
    CLI flag → environment variable → config file → built-in default

Environment variables:
    DEPSILO_CONFIG              Alternative path to config.toml
    DEPSILO_SERVER_PORT         Override [server] port
    DEPSILO_SERVER_HOST         Override [server] host
    DEPSILO_SERVER_LOG_LEVEL    Override [server] log_level

Examples:
    depsilo serve                                Use default config + port 23333
    depsilo serve --port 18080                   Override port only
    depsilo serve --host 127.0.0.1 --port 8080   Bind localhost-only on 8080
    depsilo serve --config /etc/depsilo.toml     Explicit config file
    DEPSILO_SERVER_PORT=9000 depsilo serve       Same as --port 9000`)
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if opts.Port < 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("--port out of range: %d (want 1-65535)", opts.Port)
	}
	if opts.LogLevel != "" {
		switch strings.ToLower(opts.LogLevel) {
		case "debug", "info", "warn", "warning", "error":
			// ok
		default:
			return nil, fmt.Errorf("--log-level: unknown level %q (want debug|info|warn|error)", opts.LogLevel)
		}
	}
	return opts, nil
}

// ApplyServeOptions exports the parsed options into environment
// variables the config loader's AutomaticEnv hook reads. Done as a
// separate step so tests can assert ParseServeFlags is pure (no env
// side-effects) — only RunServe touches the process environment.
//
// We don't clear pre-existing env values when the flag is zero: that
// preserves the "env beats default" half of the precedence rule. A
// flag explicitly set to a non-zero value overwrites the env; an
// unset flag leaves whatever the user already set in the environment.
func (o *ServeOptions) ApplyEnv() {
	if o.Port > 0 {
		os.Setenv("DEPSILO_SERVER_PORT", strconv.Itoa(o.Port))
	}
	if o.Host != "" {
		os.Setenv("DEPSILO_SERVER_HOST", o.Host)
	}
	if o.ConfigPath != "" {
		os.Setenv("DEPSILO_CONFIG", o.ConfigPath)
	}
	if o.LogLevel != "" {
		os.Setenv("DEPSILO_SERVER_LOG_LEVEL", o.LogLevel)
	}
}

// RunServe is the entry point cmd/depsilo/main.go calls for the
// "serve" / "server" subcommand. Returns the process exit code.
func RunServe(args []string) int {
	opts, err := ParseServeFlags(args, os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	opts.ApplyEnv()

	logger, logLevel, err := logging.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		return 1
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := server.StartServer(ctx, logLevel)
	if err != nil {
		zap.L().Fatal("failed to start server", zap.Error(err))
		return 1
	}

	<-ctx.Done()
	zap.L().Info("shutting down server...")
	if err := srv.Shutdown(context.Background()); err != nil {
		zap.L().Error("server shutdown error", zap.Error(err))
		return 1
	}
	return 0
}
