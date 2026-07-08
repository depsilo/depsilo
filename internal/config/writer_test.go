package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
