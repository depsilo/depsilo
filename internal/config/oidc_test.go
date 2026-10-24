package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validOIDCConfig() OIDCConfig {
	return OIDCConfig{
		Enabled: true, DisplayName: "Casdoor", Issuer: "https://identity.example.test",
		ClientID: "depsilo", ClientSecret: "test-only-client-secret",
		RedirectURL: "https://depsilo.example.test/api/v1/auth/oidc/callback",
		AdminGroups: []string{"engineering/admins"}, ReadonlyGroups: []string{"engineering/readers"},
		GroupsClaim: "groups", SessionTTL: 30 * time.Minute,
	}
}

func TestOIDCValidationFailsClosed(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*OIDCConfig)
	}{
		{"missing issuer", func(c *OIDCConfig) { c.Issuer = "" }},
		{"issuer credentials", func(c *OIDCConfig) { c.Issuer = "https://private:secret@example.test" }},
		{"issuer query", func(c *OIDCConfig) { c.Issuer += "?organization=foo" }},
		{"issuer empty query", func(c *OIDCConfig) { c.Issuer += "?" }},
		{"issuer empty fragment", func(c *OIDCConfig) { c.Issuer += "#" }},
		{"issuer whitespace", func(c *OIDCConfig) { c.Issuer += " " }},
		{"issuer escaped path", func(c *OIDCConfig) { c.Issuer += "/%2fhidden" }},
		{"issuer dot segment", func(c *OIDCConfig) { c.Issuer += "/organization/../other" }},
		{"issuer invalid port", func(c *OIDCConfig) { c.Issuer += ":99999" }},
		{"remote insecure issuer", func(c *OIDCConfig) { c.Issuer = "http://identity.example.test"; c.AllowInsecureLoopback = true }},
		{"loopback without opt in", func(c *OIDCConfig) { c.Issuer = "http://127.0.0.1:18000" }},
		{"missing client id", func(c *OIDCConfig) { c.ClientID = "" }},
		{"missing client secret", func(c *OIDCConfig) { c.ClientSecret = "" }},
		{"secret whitespace", func(c *OIDCConfig) { c.ClientSecret += "\n" }},
		{"missing label", func(c *OIDCConfig) { c.DisplayName = "" }},
		{"missing claim", func(c *OIDCConfig) { c.GroupsClaim = "" }},
		{"callback wrong path", func(c *OIDCConfig) { c.RedirectURL = "https://depsilo.example.test/admin" }},
		{"callback query", func(c *OIDCConfig) { c.RedirectURL += "?next=/admin" }},
		{"remote insecure callback", func(c *OIDCConfig) {
			c.RedirectURL = "http://10.0.0.1/api/v1/auth/oidc/callback"
			c.AllowInsecureLoopback = true
		}},
		{"no admission groups", func(c *OIDCConfig) { c.AdminGroups = nil; c.ReadonlyGroups = nil }},
		{"overlapping roles", func(c *OIDCConfig) { c.ReadonlyGroups = c.AdminGroups }},
		{"duplicate group", func(c *OIDCConfig) { c.AdminGroups = []string{"admins", "admins"} }},
		{"empty group", func(c *OIDCConfig) { c.AdminGroups = []string{""} }},
		{"group whitespace", func(c *OIDCConfig) { c.AdminGroups = []string{" admins "} }},
		{"short session", func(c *OIDCConfig) { c.SessionTTL = 4 * time.Minute }},
		{"long session", func(c *OIDCConfig) { c.SessionTTL = time.Hour + time.Second }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validOIDCConfig()
			tt.change(&cfg)
			err := ValidateOIDCConfig(cfg)
			if err == nil {
				t.Fatal("invalid provider was accepted")
			}
			if cfg.ClientSecret != "" && strings.Contains(err.Error(), cfg.ClientSecret) {
				t.Fatal("validation error exposed a secret")
			}
		})
	}
	for _, issuer := range []string{"https://identity.example.test", "https://identity.example.test/organization/", "http://localhost:18000", "http://[::1]:18000"} {
		cfg := validOIDCConfig()
		cfg.Issuer = issuer
		cfg.AllowInsecureLoopback = true
		cfg.RedirectURL = "http://127.0.0.1:23333/api/v1/auth/oidc/callback"
		if err := ValidateOIDCConfig(cfg); err != nil {
			t.Fatalf("valid exact issuer rejected: %v", err)
		}
	}
	if err := ValidateOIDCConfig(OIDCConfig{}); err != nil {
		t.Fatalf("disabled OIDC should preserve existing local-only configurations: %v", err)
	}
}

func TestOIDCDefaultsAndEnvironmentPrecedence(t *testing.T) {
	defaults, err := decodeConfigDocument(nil)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Auth.OIDC.Enabled || defaults.Auth.OIDC.DisplayName != "OIDC" || defaults.Auth.OIDC.GroupsClaim != "groups" || defaults.Auth.OIDC.SessionTTL != 30*time.Minute {
		t.Fatal("OIDC defaults changed")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("config_version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	setTestJWTSecret(t)
	for key, value := range map[string]string{
		"ENABLED": "true", "DISPLAY_NAME": "Company login", "ISSUER": "https://identity.example.test",
		"CLIENT_ID": "depsilo", "CLIENT_SECRET": "private-env-only-secret",
		"REDIRECT_URL": "https://depsilo.example.test/api/v1/auth/oidc/callback",
		"ADMIN_GROUPS": "org/admins,org/operators", "READONLY_GROUPS": "org/readers",
		"GROUPS_CLAIM": "roles", "SESSION_TTL": "15m", "ALLOW_INSECURE_LOOPBACK": "false",
	} {
		t.Setenv("DEPSILO_AUTH_OIDC_"+key, value)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := loaded.Auth.OIDC
	if !cfg.Enabled || cfg.DisplayName != "Company login" || cfg.GroupsClaim != "roles" || cfg.SessionTTL != 15*time.Minute ||
		cfg.ClientSecret != "private-env-only-secret" || !reflect.DeepEqual(cfg.AdminGroups, []string{"org/admins", "org/operators"}) ||
		!reflect.DeepEqual(cfg.ReadonlyGroups, []string{"org/readers"}) {
		t.Fatal("OIDC environment configuration did not match")
	}
	t.Setenv("DEPSILO_AUTH_OIDC_SESSION_TTL", "2h")
	if _, err := Load(); err == nil {
		t.Fatal("invalid runtime session lifetime accepted")
	}
}

func TestOIDCSettingsUpdatePreservesProviderAndRedactsSecrets(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	provider := `
[auth.oidc]
enabled = true
display_name = "Company login"
issuer = "https://identity.example.test"
client_id = "depsilo"
# Keep this secret outside TOML via DEPSILO_AUTH_OIDC_CLIENT_SECRET.
redirect_url = "https://depsilo.example.test/api/v1/auth/oidc/callback"
admin_groups = ["admins"]
readonly_groups = ["readers"]
session_ttl = "20m"
`
	if err := os.WriteFile(path, []byte(storeConfigFixture+provider), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_AUTH_OIDC_CLIENT_SECRET", "private-env-only-secret")
	cfg, level := loadStoreFixture(t, path)
	store := NewStore(path, cfg, level)
	ttl := "48h"
	result, err := store.Update(context.Background(), SettingsPatch{Auth: &SettingsAuthPatch{TokenTTL: &ttl}})
	if err != nil {
		t.Fatal(err)
	}
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(document), provider) || strings.Contains(string(document), cfg.Auth.OIDC.ClientSecret) {
		t.Fatal("settings update changed provider section or persisted environment secret")
	}
	for _, value := range []any{cfg.Auth.OIDC, SettingsSnapshotFromConfig(cfg), result} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), cfg.Auth.OIDC.ClientSecret) {
			t.Fatal("JSON response exposed provider credentials")
		}
	}
	reloaded, err := Load()
	if err != nil || !reflect.DeepEqual(reloaded.Auth.OIDC, cfg.Auth.OIDC) {
		t.Fatal("settings update changed effective OIDC configuration")
	}
}
