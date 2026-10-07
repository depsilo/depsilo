package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateCascadeConfigAllowsStagedPeersWhileDisabled(t *testing.T) {
	cfg := &Config{Cascade: CascadeConfig{Peers: []CascadePeerConfig{{Name: "home", URL: "https://cache.example"}}}}
	if err := validateCascadeConfig(cfg); err != nil {
		t.Fatalf("a disabled cascade may keep staged peers for a later enable: %v", err)
	}
	if cfg.Cascade.MaxHops == 0 || cfg.Cascade.MaxTTL == 0 {
		t.Fatal("disabled cascade defaults were not normalized")
	}
}

func TestValidateCascadeConfigNormalizesAndValidates(t *testing.T) {
	cfg := &Config{
		Cascade: CascadeConfig{
			Enabled: true,
			Token:   "0123456789abcdef",
			Peers: []CascadePeerConfig{{
				Name: "home",
				URL:  "https://cache.example/depsilo/",
			}},
		},
	}
	cfg.NPM.Upstreams = []UpstreamConfig{{Name: "peer", URL: "https://registry.npmjs.org", Priority: 1, Via: "home"}}
	if err := validateCascadeConfig(cfg); err != nil {
		t.Fatalf("validateCascadeConfig: %v", err)
	}
	if cfg.Cascade.MaxHops == 0 {
		t.Fatal("max_hops default was not applied")
	}
	if cfg.Cascade.MaxTTL != 7*24*time.Hour {
		t.Fatalf("max_ttl default = %s", cfg.Cascade.MaxTTL)
	}
	if cfg.Cascade.Peers[0].Token != "" {
		t.Fatal("validation must not materialize an inherited peer token")
	}
}

func TestValidateCascadeConfigRejectsBadPeersAndReferences(t *testing.T) {
	base := func() *Config {
		return &Config{Cascade: CascadeConfig{
			Enabled: true,
			Token:   "0123456789abcdef",
			Peers:   []CascadePeerConfig{{Name: "home", URL: "https://cache.example"}},
		}}
	}
	cases := map[string]func(*Config){
		"short token":           func(cfg *Config) { cfg.Cascade.Token = "short" },
		"enabled without token": func(cfg *Config) { cfg.Cascade.Token = "" },
		"whitespace token": func(cfg *Config) {
			cfg.Cascade.Token = "0123456789abcdef "
		},
		"duplicate peer": func(cfg *Config) {
			cfg.Cascade.Peers = append(cfg.Cascade.Peers, CascadePeerConfig{Name: "home", URL: "https://other.example"})
		},
		"bad peer name": func(cfg *Config) { cfg.Cascade.Peers[0].Name = "Home Server" },
		"http without opt-in": func(cfg *Config) {
			cfg.Cascade.Peers[0].URL = "http://cache.internal:23333"
		},
		"credentialed peer URL": func(cfg *Config) {
			cfg.Cascade.Peers[0].URL = "https://user:pass@cache.example"
		},
		"unknown via": func(cfg *Config) {
			cfg.NPM.Upstreams = []UpstreamConfig{{Name: "peer", URL: "https://registry.npmjs.org", Priority: 1, Via: "missing"}}
		},
		"extra index via": func(cfg *Config) {
			cfg.ExtraIndexes = []ExtraIndexConfig{{
				Name:      "corp",
				Path:      "corp",
				Upstreams: []UpstreamConfig{{Name: "peer", URL: "https://pypi.example", Priority: 1, Via: "home"}},
			}}
		},
		"too many hops": func(cfg *Config) { cfg.Cascade.MaxHops = 99 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base()
			mutate(cfg)
			if err := validateCascadeConfig(cfg); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestValidateCascadeAllowsLoopbackHTTPWithoutOptIn(t *testing.T) {
	cfg := &Config{Cascade: CascadeConfig{
		Enabled: true,
		Token:   "0123456789abcdef",
		Peers:   []CascadePeerConfig{{Name: "local", URL: "http://127.0.0.1:23334"}},
	}}
	if err := validateCascadeConfig(cfg); err != nil {
		t.Fatalf("loopback http peer should be allowed: %v", err)
	}
}

func TestLoadCascadeConfigFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	document := `config_version = 1

[cascade]
enabled = true
token = "0123456789abcdef"
allow_insecure_http = true
max_hops = 3
max_ttl = "12h"

[[cascade.peers]]
name = "home"
url = "http://192.168.1.10:23333"

[[npm.upstreams]]
name = "peer"
url = "https://registry.npmjs.org"
priority = 1
via = "home"
`
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	setTestJWTSecret(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Cascade.Enabled || cfg.Cascade.Token != "0123456789abcdef" {
		t.Fatalf("cascade settings = %#v", cfg.Cascade)
	}
	if cfg.Cascade.MaxHops != 3 || cfg.Cascade.MaxTTL != 12*time.Hour {
		t.Fatalf("cascade bounds = %d hops, %s", cfg.Cascade.MaxHops, cfg.Cascade.MaxTTL)
	}
	if len(cfg.Cascade.Peers) != 1 || cfg.Cascade.Peers[0].Name != "home" {
		t.Fatalf("peers = %#v", cfg.Cascade.Peers)
	}
	if len(cfg.NPM.Upstreams) != 1 || cfg.NPM.Upstreams[0].Via != "home" {
		t.Fatalf("npm upstreams = %#v", cfg.NPM.Upstreams)
	}
	if !strings.Contains(cfg.NPM.Upstreams[0].URL, "registry.npmjs.org") {
		t.Fatalf("upstream URL = %q", cfg.NPM.Upstreams[0].URL)
	}
}
