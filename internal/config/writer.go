package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"depsilo/internal/ecosystem"
)

// SetupRequest is the request body from the setup wizard.
type SetupRequest struct {
	// Database is populated by the setup handler from the effective startup
	// configuration. It is deliberately not accepted from the browser: the
	// wizard must never be able to retarget the transaction it is committing.
	Database struct {
		Driver string `json:"-"`
		DSN    string `json:"-"`
	} `json:"-" mapstructure:"-"`
	Server struct {
		Port int `json:"port"`
	} `json:"server"`
	Storage struct {
		Path string `json:"path"`
	} `json:"storage"`
	Admin struct {
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"admin"`
	Ecosystems map[string]EcosystemSetup `json:"ecosystems"`
}

// EcosystemSetup configures one ecosystem in the setup wizard.
type EcosystemSetup struct {
	Enabled   bool            `json:"enabled"`
	Upstreams []UpstreamSetup `json:"upstreams"`
}

// UpstreamSetup configures one upstream source.
type UpstreamSetup struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Priority int    `json:"priority"`
	Proxy    string `json:"proxy,omitempty"`
}

// WriteConfig generates a TOML config file from the setup wizard data.
func WriteConfig(path string, req SetupRequest) error {
	return writeConfig(path, req, osAtomicFileWriter{})
}

// WriteConfigWithStageHook is the internal fault-injection seam for setup
// recovery tests. It keeps the normal API on WriteConfig while allowing the
// cross-package setup test to stop after the temporary write/fsync, immediately
// before rename, immediately after rename, or after the parent-directory fsync.
// This helper is not a compatibility contract for external callers.
func WriteConfigWithStageHook(path string, req SetupRequest, hook WriteStageHook) error {
	return writeConfig(path, req, osAtomicFileWriter{onStage: hook})
}

func writeConfig(path string, req SetupRequest, writer atomicFileWriter) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := ensureConfigDirectory(dir); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	absoluteConfigDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve config directory: %w", err)
	}
	storagePath := req.Storage.Path
	if storagePath == "" {
		storagePath = "./data/cache"
	}
	if !filepath.IsAbs(storagePath) {
		storagePath = filepath.Join(absoluteConfigDir, storagePath)
	}
	storagePath = filepath.Clean(storagePath)
	stateDataDir := filepath.Join(absoluteConfigDir, "data")
	dbPath := filepath.Join(stateDataDir, "depsilo.db")
	if req.Database.DSN != "" {
		// Preserve the effective DSN selected by config.Load (including any
		// operator-provided URI/query options). This keeps the database that was
		// committed above identical to the one a clean restart will open. Do not
		// normalize it here: whitespace can be part of a SQLite filename or URI.
		dbPath = req.Database.DSN
	}
	compileCachePath := filepath.Join(stateDataDir, "compile-cache")

	jwtSecret, err := NewSecureToken()
	if err != nil {
		return fmt.Errorf("generate JWT secret: %w", err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("config_version = %d\n\n", CurrentConfigVersion))

	// Server section
	port := req.Server.Port
	if port == 0 {
		port = 23333
	}
	b.WriteString("[server]\n")
	b.WriteString("host = \"127.0.0.1\"\n")
	b.WriteString(fmt.Sprintf("port = %d\n", port))
	b.WriteString("\n")

	// Database section
	databaseDriver := strings.TrimSpace(req.Database.Driver)
	if databaseDriver == "" {
		databaseDriver = "sqlite"
	}
	b.WriteString("[database]\n")
	b.WriteString(fmt.Sprintf("driver = %q\n", databaseDriver))
	b.WriteString(fmt.Sprintf("dsn = %q\n", dbPath))
	b.WriteString("\n")

	// Storage section
	b.WriteString("[storage]\n")
	b.WriteString("type = \"local\"\n")
	b.WriteString(fmt.Sprintf("path = %q\n", storagePath))
	b.WriteString("\n")

	// Cache section
	b.WriteString("[cache]\n")
	b.WriteString("max_size_gb = 20\n")
	b.WriteString("ttl_index = \"5m\"\n")
	b.WriteString("ttl_blob = \"72h\"\n")
	b.WriteString("lru_threshold = 90\n")
	b.WriteString("\n")

	// Package-policy refreshes default to the availability-friendly
	// last-known-good behavior, but keep the choice explicit in every newly
	// generated configuration so operators can review and change it.
	b.WriteString("[policy]\n")
	b.WriteString("on_load_error = \"use_stale_then_allow\"\n")
	b.WriteString("\n")

	// Compiler artifacts use a separate namespace but share the same durable
	// data root. Keeping the disabled default explicit prevents a later opt-in
	// from silently placing objects relative to the process working directory.
	b.WriteString("[compile_cache]\n")
	b.WriteString("enabled = false\n")
	b.WriteString("\n")
	b.WriteString("[compile_cache.storage]\n")
	b.WriteString("type = \"local\"\n")
	b.WriteString(fmt.Sprintf("path = %q\n", compileCachePath))
	b.WriteString("\n")

	// Auth section
	b.WriteString("[auth]\n")
	b.WriteString("enabled = true\n")
	b.WriteString(fmt.Sprintf("jwt_secret = %q\n", jwtSecret))
	b.WriteString("token_ttl = \"168h\"\n")
	b.WriteString("\n")

	// Ecosystem sections. Iterate the catalog rather than the request map so
	// repeated setup submissions produce a stable, reviewable document.
	for _, definition := range ecosystem.SetupDefinitions() {
		if !definition.StandardUpstreams {
			continue
		}
		setup, ok := req.Ecosystems[definition.Name]
		if !ok {
			continue
		}
		if !setup.Enabled {
			continue
		}
		for _, u := range setup.Upstreams {
			b.WriteString(fmt.Sprintf("[[%s.upstreams]]\n", definition.Name))
			b.WriteString(fmt.Sprintf("name = %q\n", u.Name))
			b.WriteString(fmt.Sprintf("url = %q\n", u.URL))
			b.WriteString(fmt.Sprintf("priority = %d\n", u.Priority))
			if u.Proxy != "" {
				b.WriteString(fmt.Sprintf("proxy = %q\n", u.Proxy))
			} else {
				b.WriteString("# proxy = \"http://127.0.0.1:7890\"\n")
			}
			b.WriteString("\n")
		}
	}

	// Docker section (empty by default, user configures later)
	// Security section
	b.WriteString("[security]\n")
	b.WriteString("enabled = true\n")
	b.WriteString("osv_api_url = \"https://api.osv.dev\"\n")
	b.WriteString("scan_interval = \"24h\"\n")
	b.WriteString("check_ttl = \"24h\"\n")

	outcome, err := writer.Write(path, []byte(b.String()), 0600)
	if err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	if outcome.durabilityErr != nil {
		return fmt.Errorf("sync config directory: %w", outcome.durabilityErr)
	}

	return nil
}

func ensureConfigDirectory(dir string) error {
	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		return os.MkdirAll(dir, 0700)
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	case filepath.Base(filepath.Clean(dir)) == ".depsilo":
		// This is Depsilo's dedicated default state/config directory, so it is
		// safe to tighten an older installation that was created as 0755.
		return os.Chmod(dir, 0700)
	default:
		// ConfigPath may point at an arbitrary project or system directory. Do
		// not change that directory's operator-managed permissions; the config
		// file itself is still atomically replaced with mode 0600.
		return nil
	}
}
