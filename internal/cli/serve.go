package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/api"
	"depsilo/internal/config"
	"depsilo/internal/logging"
	"depsilo/internal/server"
)

const gracefulShutdownTimeout = 10 * time.Second

// ServeOptions captures the command-line shape of `depsilo serve`.
// Flags map 1:1 to config fields under [server]. When a flag is left
// at its zero value, the corresponding config / env / default takes
// over. Explicit flags overwrite via os.Setenv so the loader's
// AutomaticEnv hook picks them up — keeps the override path in one
// place (the loader) instead of fanning out into every consumer.
type ServeOptions struct {
	Port       int    // 0 = use config / DEPSILO_SERVER_PORT / default 23333
	Host       string // empty = use config / DEPSILO_SERVER_HOST / default 127.0.0.1
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
	fs.StringVar(&opts.Host, "host", "", "Listen host (overrides config; default 127.0.0.1)")
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
		normalized := strings.ToLower(opts.LogLevel)
		switch normalized {
		case "debug", "info", "warn", "error":
			opts.LogLevel = normalized
		case "warning":
			opts.LogLevel = "warn"
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

// RunServe is the handler Main dispatches the "serve" / "server"
// subcommand to. Returns the process exit code.
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

	// Load the configuration once before the logger exists. A file or an
	// environment the loader rejects is the operator's to correct: it gets
	// one plain line, not a structured entry with a stack trace that a
	// restarting container repeats. The server loads the configuration again
	// and logs what this silent load does not.
	cfg, err := config.LoadForServer()
	if err != nil {
		fmt.Fprintln(os.Stderr, configurationFailureLine(err))
		return 1
	}

	logger, logLevel, err := logging.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to init logger: %v\n", err)
		return 1
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	ctx, cancel, err := daemonShutdownContext(context.Background())
	if err != nil {
		zap.L().Error("failed to initialize shutdown channel", zap.Error(err))
		return 1
	}
	defer cancel()

	srv, err := server.StartServer(ctx, logLevel)
	if err != nil {
		// The server does not say which of its failures are mistakes in the
		// configuration (a trusted proxy that is not an address, a port out
		// of range, a storage type it does not know), so every failed start
		// is reported the way a rejected configuration is: one plain line.
		// A structured entry would add only this function's own stack.
		fmt.Fprintln(os.Stderr, startFailureLine(err, cfg))
		return 1
	}

	<-ctx.Done()
	zap.L().Info("shutting down server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx, srv); err != nil {
		zap.L().Error("server shutdown error", zap.Error(err))
		return 1
	}
	return 0
}

// windowsAddressInUse is WSAEADDRINUSE. Windows reports it instead of the
// portable syscall.EADDRINUSE value, and no other platform uses the number.
const windowsAddressInUse = syscall.Errno(10048)

// statusProbesLocalPort reports whether "depsilo status", in this
// environment, asks a loopback address on the given port.
func statusProbesLocalPort(port int) bool {
	target, err := url.Parse(getServerURL())
	if err != nil || target.Port() != strconv.Itoa(port) {
		return false
	}
	switch target.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// configurationFailureLine is the one plain line for a configuration, file or
// environment, that the loader rejected. The message is the loader's own; it
// names the setting and never repeats a secret value.
func configurationFailureLine(err error) string {
	if line, ok := startupFailureLine(err, nil); ok {
		return line
	}
	return "Error: " + onOneLine(err.Error())
}

// startFailureLine is the one plain line for a server that failed to start
// with the configuration cfg: the line written for the failure when there is
// one, and the error's own message otherwise.
func startFailureLine(err error, cfg *config.Config) string {
	if line, ok := startupFailureLine(err, cfg); ok {
		return line
	}
	if line, ok := initialAdminFailureLine(err); ok {
		return line
	}
	return "Error: " + onOneLine(err.Error())
}

// onOneLine joins the lines of an error message with "; ". Empty lines are
// left out, and a line that ends in a colon introduces the next one, so it is
// followed by a space instead.
func onOneLine(message string) string {
	var line strings.Builder
	for _, part := range strings.Split(message, "\n") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if line.Len() > 0 {
			if strings.HasSuffix(line.String(), ":") {
				line.WriteString(" ")
			} else {
				line.WriteString("; ")
			}
		}
		line.WriteString(part)
	}
	return line.String()
}

// initialAdminFailureLine recognises a start that failed because the
// administrator described by DEPSILO_ADMIN_USERNAME and DEPSILO_ADMIN_PASSWORD
// breaks the credential rules, which is a mistake in the environment like any
// other. The server reports it as an untyped error after the database is
// open, so the same validation is repeated here and its message looked for.
func initialAdminFailureLine(err error) (line string, ok bool) {
	username, password := os.Getenv("DEPSILO_ADMIN_USERNAME"), os.Getenv("DEPSILO_ADMIN_PASSWORD")
	if username == "" && password == "" {
		return "", false
	}
	// The server's own defaults for the half that is not set.
	if username == "" {
		username = "admin"
	}
	if password == "" {
		generated, tokenErr := config.NewSecureToken()
		if tokenErr != nil {
			return "", false
		}
		password = generated
	}
	invalid := api.ValidateInitialAdminCredentials(username, password)
	message := onOneLine(err.Error())
	if invalid == nil || !strings.Contains(message, ": "+invalid.Error()) {
		return "", false
	}
	return "Error: " + message + " (set by DEPSILO_ADMIN_USERNAME and DEPSILO_ADMIN_PASSWORD)", true
}

// startupFailureLine recognises the startup failures an operator fixes
// without reading a Go error chain, and returns the one plain line that
// replaces the Go error chain. ok is false for every other error, which is
// reported with its own message. cfg is the configuration the failed start
// used, or nil when it could not be loaded.
func startupFailureLine(err error, cfg *config.Config) (line string, ok bool) {
	if errors.Is(err, syscall.EADDRINUSE) || errors.Is(err, windowsAddressInUse) {
		subject, statusCommand := "The listen address", "depsilo status"
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			if addr, isTCP := opErr.Addr.(*net.TCPAddr); isTCP && addr.Port > 0 {
				subject = "Port " + strconv.Itoa(addr.Port)
				host := ""
				if len(addr.IP) > 0 {
					host = addr.IP.String()
				}
				// A bare "depsilo status" asks DEPSILO_URL or port 23333,
				// which may be a different instance than the one in the way.
				if !statusProbesLocalPort(addr.Port) {
					statusCommand = "DEPSILO_URL=" + strings.TrimSuffix(daemonReadinessURL(host, addr.Port), "/ready") + " depsilo status"
				}
			}
		}
		return fmt.Sprintf("%s is already in use. Another Depsilo may be running (try \"%s\"), or choose another port with --port.",
			subject, statusCommand), true
	}

	var pathErr *fs.PathError
	if errors.As(err, &pathErr) && errors.Is(pathErr, fs.ErrPermission) {
		// Startup creates directories only inside its own state. Any other
		// operation may be a read of a file that is not state, such as the
		// config.toml named by --config, so that line asks for access to the
		// path instead of a writable state directory.
		if line, relative := relativePathFailureLine(pathErr, cfg); relative {
			return line, true
		}
		need := "Depsilo must be able to access this path as the user it runs as"
		if pathErr.Op == "mkdir" {
			need = "The state directory must be writable by the user Depsilo runs as"
		}
		return fmt.Sprintf("Permission denied: %s %s. %s; "+
			"the official container image runs as UID and GID 10001. See docs/deployment.md#bind-mounted-state",
			pathErr.Op, pathErr.Path, need), true
	}
	return "", false
}

// relativePathFailureLine recognises a permission failure that volume
// ownership does not explain: a config.toml whose paths are relative, or left
// at their relative defaults, puts the state into the working directory, which
// in the official container image is /app and not writable. It applies when
// the failed path lies inside the working directory, that directory is not
// the one holding config.toml, and a local path of the configuration is
// relative.
func relativePathFailureLine(pathErr *fs.PathError, cfg *config.Config) (line string, ok bool) {
	if cfg == nil || cfg.IsDefault || cfg.ConfigPath == "" {
		return "", false
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", false
	}
	configPath, err := filepath.Abs(cfg.ConfigPath)
	if err != nil {
		return "", false
	}
	// Startup reports canonical paths; compare like with like.
	canonicalWorkingDirectory := canonicalPath(workingDirectory)
	if canonicalWorkingDirectory == canonicalPath(filepath.Dir(configPath)) {
		return "", false
	}
	inside, err := filepath.Rel(canonicalWorkingDirectory, canonicalPath(pathErr.Path))
	if err != nil || inside == "." || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) || filepath.IsAbs(inside) {
		return "", false
	}

	var settings []string
	if cfg.Database.Driver == "sqlite" {
		file, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(cfg.Database.DSN), "file:"), "?")
		if file != "" && file != ":memory:" && !filepath.IsAbs(file) {
			settings = append(settings, "database.dsn")
		}
	}
	for _, storage := range []struct {
		setting string
		used    bool
		config  config.StorageConfig
	}{
		{"storage.path", true, cfg.Storage},
		{"compile_cache.storage.path", cfg.CompileCache.Enabled, cfg.CompileCache.Storage},
		{"distribution.storage.path", cfg.Distribution.Enabled, cfg.Distribution.Storage},
	} {
		if storage.used && storage.config.Type == "local" && storage.config.Path != "" && !filepath.IsAbs(storage.config.Path) {
			settings = append(settings, storage.setting)
		}
	}
	if len(settings) == 0 {
		return "", false
	}
	return fmt.Sprintf("Permission denied: %s %s. Relative paths in config.toml resolve against the working directory, %s; "+
		"set absolute paths in %s for %s (a setting left out counts: its default is relative). See docs/deployment.md#default-paths",
		pathErr.Op, pathErr.Path, workingDirectory, configPath, strings.Join(settings, ", ")), true
}

// canonicalPath resolves the symbolic links of the part of path that exists
// and keeps the rest, so a path that could not be created still compares
// equal to the place it would have been created in.
func canonicalPath(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for current := path; ; {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		rest = filepath.Join(filepath.Base(current), rest)
		current = parent
	}
}
