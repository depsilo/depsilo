package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestWriteConfigAnchorsGeneratedStateToConfigDirectory(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "state", ".depsilo")
	path := filepath.Join(configDir, "config.toml")
	req := SetupRequest{}
	req.Server.Port = 23333
	req.Storage.Path = "./data/cache"

	if err := WriteConfig(path, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}

	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(document)); err != nil {
		t.Fatalf("read generated TOML: %v", err)
	}
	dataDir := filepath.Join(configDir, "data")
	wants := map[string]string{
		"server.host":                "127.0.0.1",
		"database.dsn":               filepath.Join(dataDir, "depsilo.db"),
		"storage.path":               filepath.Join(dataDir, "cache"),
		"compile_cache.storage.type": "local",
		"compile_cache.storage.path": filepath.Join(dataDir, "compile-cache"),
	}
	for key, want := range wants {
		if !v.InConfig(key) {
			t.Errorf("generated config does not explicitly contain %s", key)
			continue
		}
		if got := v.GetString(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
		if strings.HasSuffix(key, ".path") || key == "database.dsn" {
			if !filepath.IsAbs(v.GetString(key)) {
				t.Errorf("%s = %q, want an absolute path", key, v.GetString(key))
			}
		}
	}
	if !v.InConfig("compile_cache.enabled") {
		t.Fatal("generated config does not explicitly disable the compiler cache")
	}
	if v.GetBool("compile_cache.enabled") {
		t.Fatal("generated config enables the compiler cache")
	}
}

func TestWriteConfigPreservesEffectiveDatabaseDSNForSetupRestart(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, ".depsilo", "config.toml")
	effectiveDSN := filepath.Join(root, "external", "committed.db")
	req := SetupRequest{}
	req.Database.Driver = "sqlite"
	req.Database.DSN = effectiveDSN
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(root, "cache")

	if err := WriteConfig(configPath, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	document, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(document)); err != nil {
		t.Fatal(err)
	}
	if got := v.GetString("database.driver"); got != "sqlite" {
		t.Fatalf("database.driver = %q, want sqlite", got)
	}
	if got := v.GetString("database.dsn"); got != effectiveDSN {
		t.Fatalf("database.dsn = %q, want effective DSN %q", got, effectiveDSN)
	}
}

func TestWriteConfigPreservesWhitespaceInEffectiveDatabaseDSN(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.toml")
	effectiveDSN := filepath.Join(root, "database with spaces.db") + " "
	req := SetupRequest{}
	req.Database.DSN = effectiveDSN

	if err := WriteConfig(configPath, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	document, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(document)); err != nil {
		t.Fatal(err)
	}
	if got := v.GetString("database.dsn"); got != effectiveDSN {
		t.Fatalf("database.dsn = %q, want byte-for-byte effective DSN %q", got, effectiveDSN)
	}
}

func TestSetupRequestJSONCannotOverrideEffectiveDatabaseTarget(t *testing.T) {
	var req SetupRequest
	if err := json.Unmarshal([]byte(`{"database":{"driver":"postgres","dsn":"/tmp/attacker.db"}}`), &req); err != nil {
		t.Fatal(err)
	}
	if req.Database.Driver != "" || req.Database.DSN != "" {
		t.Fatalf("browser supplied database target was accepted: %#v", req.Database)
	}
	encoded, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("database")) || bytes.Contains(encoded, []byte("dsn")) {
		t.Fatalf("internal database target leaked into setup JSON: %s", encoded)
	}
}

func TestWriteConfigPreservesAbsoluteStoragePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".depsilo", "config.toml")
	storagePath := filepath.Join(root, "external", "artifacts")
	req := SetupRequest{}
	req.Server.Port = 23333
	req.Storage.Path = storagePath

	if err := WriteConfig(path, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	v := viper.New()
	v.SetConfigType("toml")
	if err := v.ReadConfig(bytes.NewReader(document)); err != nil {
		t.Fatalf("read generated TOML: %v", err)
	}
	if got := v.GetString("storage.path"); got != storagePath {
		t.Fatalf("storage.path = %q, want preserved absolute path %q", got, storagePath)
	}
	stateDataDir := filepath.Join(root, ".depsilo", "data")
	if got, want := v.GetString("database.dsn"), filepath.Join(stateDataDir, "depsilo.db"); got != want {
		t.Fatalf("database.dsn = %q, want %q", got, want)
	}
	if got, want := v.GetString("compile_cache.storage.path"), filepath.Join(stateDataDir, "compile-cache"); got != want {
		t.Fatalf("compile_cache.storage.path = %q, want %q", got, want)
	}
}

func TestWriteConfigCommentsDefaultProxyLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	req := SetupRequest{
		Ecosystems: map[string]EcosystemSetup{
			"npm": {
				Enabled: true,
				Upstreams: []UpstreamSetup{
					{
						Name:     "official",
						URL:      "https://registry.npmjs.org",
						Priority: 1,
					},
				},
			},
		},
	}
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(dir, "cache")

	if err := WriteConfig(path, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	configText := string(data)

	if strings.Contains(configText, "\nproxy = ") {
		t.Fatalf("generated config contains active proxy line:\n%s", configText)
	}
	if !strings.Contains(configText, "# proxy = \"http://127.0.0.1:7890\"") {
		t.Fatalf("generated config does not contain commented proxy value:\n%s", configText)
	}
}

func TestWriteConfigKeepsExplicitProxyActive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	req := SetupRequest{
		Ecosystems: map[string]EcosystemSetup{
			"npm": {
				Enabled: true,
				Upstreams: []UpstreamSetup{
					{
						Name:     "official",
						URL:      "https://registry.npmjs.org",
						Priority: 1,
						Proxy:    "http://proxy.internal:7890",
					},
				},
			},
		},
	}
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(dir, "cache")

	if err := WriteConfig(path, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	configText := string(data)

	if !strings.Contains(configText, "\nproxy = \"http://proxy.internal:7890\"") {
		t.Fatalf("generated config does not contain active explicit proxy:\n%s", configText)
	}
	if strings.Contains(configText, "# proxy = \"http://proxy.internal:7890\"") {
		t.Fatalf("generated config comments explicit proxy:\n%s", configText)
	}
}

func TestWriteConfigIncludesAlpineUpstreams(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")

	req := SetupRequest{
		Ecosystems: map[string]EcosystemSetup{
			"alpine": {
				Enabled: true,
				Upstreams: []UpstreamSetup{
					{
						Name:     "official",
						URL:      "https://dl-cdn.alpinelinux.org/alpine",
						Priority: 1,
					},
				},
			},
		},
	}
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(dir, "cache")

	if err := WriteConfig(path, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated config: %v", err)
	}
	configText := string(data)

	if !strings.Contains(configText, "[[alpine.upstreams]]") {
		t.Fatalf("generated config does not contain Alpine upstream section:\n%s", configText)
	}
	if !strings.Contains(configText, "url = \"https://dl-cdn.alpinelinux.org/alpine\"") {
		t.Fatalf("generated config does not contain Alpine upstream URL:\n%s", configText)
	}
}

func TestWriteConfigUsesCanonicalEcosystemOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	req := SetupRequest{
		Ecosystems: map[string]EcosystemSetup{
			"helm": {
				Enabled: true,
				Upstreams: []UpstreamSetup{{
					Name: "charts", URL: "https://charts.example", Priority: 1,
				}},
			},
			"pypi": {
				Enabled: true,
				Upstreams: []UpstreamSetup{{
					Name: "simple", URL: "https://pypi.example/simple", Priority: 1,
				}},
			},
			"npm": {
				Enabled: true,
				Upstreams: []UpstreamSetup{{
					Name: "registry", URL: "https://npm.example", Priority: 1,
				}},
			},
		},
	}
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(dir, "cache")

	if err := WriteConfig(path, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document := string(data)
	pypi := strings.Index(document, "[[pypi.upstreams]]")
	npm := strings.Index(document, "[[npm.upstreams]]")
	helm := strings.Index(document, "[[helm.upstreams]]")
	if pypi < 0 || npm < 0 || helm < 0 || !(pypi < npm && npm < helm) {
		t.Fatalf("ecosystem sections are not in canonical order: pypi=%d npm=%d helm=%d\n%s", pypi, npm, helm, document)
	}
}

func TestWriteConfigProtectsSecretsAndPermissions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "private-config")
	path := filepath.Join(dir, "config.toml")
	req := SetupRequest{
		Ecosystems: map[string]EcosystemSetup{
			"npm": {Enabled: true},
		},
	}
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(root, "cache")
	req.Admin.Username = "operator"
	req.Admin.Password = "Tr0ub4dor&Correct"

	if err := WriteConfig(path, req); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat config directory: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("config directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	document := string(data)
	if strings.Contains(document, "change-me-in-production") {
		t.Fatal("generated config contains the placeholder JWT secret")
	}
	if strings.Contains(document, req.Admin.Password) {
		t.Fatal("generated config contains the administrator password")
	}
	match := regexp.MustCompile(`jwt_secret = "([A-Za-z0-9_-]+)"`).FindStringSubmatch(document)
	if len(match) != 2 || len(match[1]) < 40 {
		t.Fatalf("generated JWT secret is missing or too short: %q", match)
	}
}

func TestWriteConfigTightensExistingDefaultDirectoryOnly(t *testing.T) {
	root := t.TempDir()
	defaultDir := filepath.Join(root, ".depsilo")
	ordinaryDir := filepath.Join(root, "project")
	for _, dir := range []string{defaultDir, ordinaryDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	req := SetupRequest{}
	req.Server.Port = 23333
	req.Storage.Path = filepath.Join(root, "cache")
	if err := WriteConfig(filepath.Join(defaultDir, "config.toml"), req); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(filepath.Join(ordinaryDir, "config.toml"), req); err != nil {
		t.Fatal(err)
	}
	defaultInfo, _ := os.Stat(defaultDir)
	if got := defaultInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("default config directory mode = %o, want 700", got)
	}
	ordinaryInfo, _ := os.Stat(ordinaryDir)
	if got := ordinaryInfo.Mode().Perm(); got != 0o755 {
		t.Fatalf("ordinary parent directory mode = %o, want preserved 755", got)
	}
}
