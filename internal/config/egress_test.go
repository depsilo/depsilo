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
		"[egress]\nprivate_ranges = ['198.18.0.0/15']\n":    "fake-IP answers need no declaration",
		"[egress]\nprivate_ranges = ['10.0.0.0/8']\n":       "egress.private_ranges[0]",
		"[egress]\nupstream_proxy = 'socks5://127.0.0.1'\n": "egress.upstream_proxy",
		"[egress]\nno_proxy = ['http://mirror.lan']\n":      "egress.no_proxy[0]",
	} {
		if _, err := loadExampleWithEgress(t, section); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Load(%q) error = %v, want %q", section, err, want)
		}
	}
}

func TestEgressConfigLoadsDeclarationsFromEnvironment(t *testing.T) {
	t.Setenv("DEPSILO_EGRESS_UPSTREAM_PROXY", "http://host.docker.internal:7890")
	t.Setenv("DEPSILO_EGRESS_NO_PROXY", "mirror.lan, 192.168.0.0/16")
	t.Setenv("DEPSILO_EGRESS_PRIVATE_RANGES", "100.64.0.0/10")
	cfg, err := loadExampleWithEgress(t, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Egress.UpstreamProxy != "http://host.docker.internal:7890" ||
		!reflect.DeepEqual(cfg.Egress.NoProxy, []string{"mirror.lan", "192.168.0.0/16"}) ||
		!reflect.DeepEqual(cfg.Egress.PrivateRanges, []string{"100.64.0.0/10"}) {
		t.Fatalf("egress = %+v", cfg.Egress)
	}
	policy, err := cfg.Egress.Policy()
	if err != nil {
		t.Fatalf("Policy: %v", err)
	}
	if got := policy.UpstreamProxyFor("https://pypi.org"); got != "http://host.docker.internal:7890" {
		t.Fatalf("proxy for pypi.org = %q", got)
	}
	if got := policy.UpstreamProxyFor("https://mirror.lan"); got != "" {
		t.Fatalf("proxy for a no_proxy host = %q, want direct egress", got)
	}
}

func TestEgressEnvironmentOverridesConfigFile(t *testing.T) {
	t.Setenv("DEPSILO_EGRESS_UPSTREAM_PROXY", "http://host.docker.internal:7890")
	t.Setenv("DEPSILO_EGRESS_NO_PROXY", "env.lan")
	t.Setenv("DEPSILO_EGRESS_PRIVATE_RANGES", "100.100.0.0/16")
	cfg, err := loadExampleWithEgress(t, "[egress]\nupstream_proxy = 'http://127.0.0.1:7890'\n"+
		"no_proxy = ['file.lan', '192.168.0.0/16']\nprivate_ranges = ['100.64.0.0/10']\n")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Egress.UpstreamProxy != "http://host.docker.internal:7890" ||
		!reflect.DeepEqual(cfg.Egress.NoProxy, []string{"env.lan"}) ||
		!reflect.DeepEqual(cfg.Egress.PrivateRanges, []string{"100.100.0.0/16"}) {
		t.Fatalf("egress = %+v, want the environment values", cfg.Egress)
	}
}

// allow_fake_ip is on unless the file or DEPSILO_EGRESS_ALLOW_FAKE_IP turns
// it off, and the variable works without a file value and overrides one
// either way. The policy carries the result to the Upstream and Docker
// registry clients.
func TestEgressAllowFakeIPDefaultsToTrueAndFollowsFileAndEnvironment(t *testing.T) {
	for _, test := range []struct {
		name, section string
		// env is the value of DEPSILO_EGRESS_ALLOW_FAKE_IP when setEnv is set.
		env    string
		setEnv bool
		want   bool
	}{
		{name: "default", want: true},
		{name: "default with other egress keys", section: "[egress]\nno_proxy = ['mirror.lan']\n", want: true},
		{name: "file value false", section: "[egress]\nallow_fake_ip = false\n"},
		{name: "file value true", section: "[egress]\nallow_fake_ip = true\n", want: true},
		{name: "environment false without a file value", env: "false", setEnv: true},
		{name: "environment false over a file value true", section: "[egress]\nallow_fake_ip = true\n", env: "false", setEnv: true},
		{name: "environment true over a file value false", section: "[egress]\nallow_fake_ip = false\n", env: "true", setEnv: true, want: true},
		// An empty variable counts as unset.
		{name: "empty environment value", section: "[egress]\nallow_fake_ip = false\n", setEnv: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.setEnv {
				t.Setenv("DEPSILO_EGRESS_ALLOW_FAKE_IP", test.env)
			}
			cfg, err := loadExampleWithEgress(t, test.section)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Egress.AllowFakeIP != test.want {
				t.Fatalf("allow_fake_ip = %v, want %v", cfg.Egress.AllowFakeIP, test.want)
			}
			policy, err := cfg.Egress.Policy()
			if err != nil {
				t.Fatalf("Policy: %v", err)
			}
			if policy.AllowsFakeIP() != test.want {
				t.Fatalf("policy allows fake-IP = %v, want %v", policy.AllowsFakeIP(), test.want)
			}
		})
	}
}

func TestEgressEnvironmentValuesAreValidatedAtStartup(t *testing.T) {
	for _, test := range []struct{ name, value, want string }{
		{"DEPSILO_EGRESS_UPSTREAM_PROXY", "ftp://bad", "egress.upstream_proxy"},
		{"DEPSILO_EGRESS_NO_PROXY", "mirror.lan,http://mirror.lan", "egress.no_proxy[1]"},
		{"DEPSILO_EGRESS_PRIVATE_RANGES", "10.0.0.0/8", "egress.private_ranges[0]"},
		{"DEPSILO_EGRESS_ALLOW_FAKE_IP", "sometimes", "allow_fake_ip"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(test.name, test.value)
			if _, err := loadExampleWithEgress(t, ""); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load with %s=%q error = %v, want %q", test.name, test.value, err, test.want)
			}
		})
	}
}
