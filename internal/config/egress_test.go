package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadExampleWithEgress(t *testing.T, section string) (*Config, error) {
	t.Helper()
	example, err := os.ReadFile(filepath.Join("..", "..", "config.example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, append(example, []byte("\n"+section)...), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	t.Setenv("DEPSILO_AUTH_JWT_SECRET", "test-only-0123456789abcdef0123456789abcdef")
	return Load()
}

func TestEgressConfigLoadsDeclarations(t *testing.T) {
	cfg, err := loadExampleWithEgress(t, "[egress]\nupstream_proxy = 'http://127.0.0.1:7890'\n"+
		"no_proxy = ['mirror.lan', '192.168.0.0/16']\nprivate_ranges = ['100.64.0.0/10']\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Egress.UpstreamProxy != "http://127.0.0.1:7890" ||
		!reflect.DeepEqual(cfg.Egress.NoProxy, []string{"mirror.lan", "192.168.0.0/16"}) ||
		!reflect.DeepEqual(cfg.Egress.PrivateRanges, []string{"100.64.0.0/10"}) {
		t.Fatalf("egress = %+v", cfg.Egress)
	}
	policy, err := cfg.Egress.Policy()
	if err != nil || policy.UpstreamProxyFor("https://pypi.org") != "http://127.0.0.1:7890" {
		t.Fatalf("policy = %+v, %v", policy, err)
	}
}

func TestEgressConfigRejectsUnsafeDeclarationsAtStartup(t *testing.T) {
	for section, want := range map[string]string{
		"[egress]\nprivate_ranges = ['198.18.0.0/15']\n":    "egress.upstream_proxy instead",
		"[egress]\nprivate_ranges = ['10.0.0.0/8']\n":       "egress.private_ranges[0]",
		"[egress]\nupstream_proxy = 'socks5://127.0.0.1'\n": "egress.upstream_proxy",
		"[egress]\nno_proxy = ['http://mirror.lan']\n":      "egress.no_proxy[0]",
	} {
		if _, err := loadExampleWithEgress(t, section); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load(%q) error = %v, want %q", section, err, want)
		}
	}
}
