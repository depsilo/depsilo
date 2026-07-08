package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetupRequest is the request body from the setup wizard.
type SetupRequest struct {
	Server struct {
		Port int `json:"port"`
	} `json:"server"`
	Storage struct {
		Path string `json:"path"`
	} `json:"storage"`
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
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	var b strings.Builder

	// Server section
	port := req.Server.Port
	if port == 0 {
		port = 23333
	}
	b.WriteString("[server]\n")
	b.WriteString(fmt.Sprintf("host = \"0.0.0.0\"\n"))
	b.WriteString(fmt.Sprintf("port = %d\n", port))
	b.WriteString("\n")

	// Database section
	dbPath := filepath.Join(filepath.Dir(req.Storage.Path), "depsilo.db")
	b.WriteString("[database]\n")
	b.WriteString("driver = \"sqlite\"\n")
	b.WriteString(fmt.Sprintf("dsn = %q\n", dbPath))
	b.WriteString("\n")

	// Storage section
	storagePath := req.Storage.Path
	if storagePath == "" {
		storagePath = "./data/cache"
	}
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

	// Auth section
	b.WriteString("[auth]\n")
	b.WriteString("enabled = true\n")
	b.WriteString("jwt_secret = \"change-me-in-production\"\n")
	b.WriteString("token_ttl = \"168h\"\n")
	b.WriteString("\n")

	// Ecosystem sections
	ecosystemTomlKey := map[string]string{
		"pypi": "pypi", "apt": "apt", "npm": "npm", "go": "go",
		"cargo": "cargo", "maven": "maven", "rubygems": "rubygems",
		"composer": "composer", "nuget": "nuget", "conda": "conda",
		"cran": "cran", "alpine": "alpine", "helm": "helm",
	}

	for eco, setup := range req.Ecosystems {
		if !setup.Enabled {
			continue
		}
		tomlKey, ok := ecosystemTomlKey[eco]
		if !ok {
			continue
		}
		for _, u := range setup.Upstreams {
			b.WriteString(fmt.Sprintf("[[%s.upstreams]]\n", tomlKey))
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

	if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}
