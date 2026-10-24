package config

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
	"go.uber.org/zap"
)

func TestLDAPInlineSettingsPatchKeepsValidTOML(t *testing.T) {
	document := []byte("[auth.sources.directory]\nprotocol = \"ldap\"\ndisplay_name = \"Old\"\nldap = { url = \"ldaps://directory.example\", bind_password = \"fixture-password\" }\n")
	updated, err := patchAuthenticationDocument(document, "directory", []oidcEntry{{"display_name", "New"}, {"ldap", map[string]string{"url": "ldaps://directory.example", "bind_password": "fixture-rotated"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := toml.Unmarshal(updated, &parsed); err != nil {
		t.Fatalf("invalid patch: %v; fixture document: %s", err, updated)
	}
}

func ldapSourceCreate() AuthenticationSourceCreate {
	return AuthenticationSourceCreate{ID: "directory", Protocol: "ldap", OIDCSettingsPatch: OIDCSettingsPatch{
		Enabled: oidcPointer(true), DisplayName: oidcPointer("Company directory"), AdminGroups: oidcPointer([]string{"cn=admins,ou=groups,dc=example,dc=com"}),
		LDAP: &LDAPSettingsPatch{URL: oidcPointer("ldaps://directory.example"), BindDN: oidcPointer("cn=reader,dc=example,dc=com"), BindPassword: oidcPointer("private-directory-password"), UserBaseDN: oidcPointer("ou=people,dc=example,dc=com")},
	}}
}

func TestLDAPSourceLifecyclePreservesOIDCAndPrivateSettings(t *testing.T) {
	ctx := context.Background()
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	before, _ := os.ReadFile(path)
	created, err := store.CreateAuthenticationSource(ctx, ldapSourceCreate())
	if err != nil {
		t.Fatal(err)
	}
	if created.Protocol != "ldap" || created.Effective != nil || !created.PendingRestart || created.Configured.LDAP == nil || !created.Configured.LDAP.BindPasswordSet || created.Configured.LDAP.SubjectAttribute != "entryUUID" {
		t.Fatalf("wrong LDAP snapshot: %+v", created)
	}
	raw, _ := json.Marshal(created)
	if strings.Contains(string(raw), "private-directory-password") || strings.Contains(string(raw), `"bind_password"`) {
		t.Fatal("LDAP password leaked in snapshot")
	}
	document, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(document), string(before)) {
		t.Fatal("LDAP source changed unrelated OIDC configuration")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("LDAP password file permissions too broad")
	}
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Sources["directory"].LDAP.BindPassword != "private-directory-password" {
		t.Fatal("LDAP bind password not persisted")
	}
	restarted := NewStore(path, cfg, zap.NewAtomicLevel())
	state, err := restarted.UpdateAuthenticationSource(ctx, "directory", OIDCSettingsPatch{DisplayName: oidcPointer("Renamed directory")})
	if err != nil || !state.PendingRestart || state.SessionsWillReset {
		t.Fatalf("rename affected session authority: %v %+v", err, state)
	}
	state, err = restarted.UpdateAuthenticationSource(ctx, "directory", OIDCSettingsPatch{LDAP: &LDAPSettingsPatch{BindPassword: oidcPointer("rotated-password")}})
	if err != nil || !state.SessionsWillReset {
		t.Fatalf("LDAP password rotation did not report session impact: %v", err)
	}
	preview, err := restarted.PreviewLDAPAuthenticationSourceDraft(ctx, AuthenticationSourceDraft{ID: "directory", Protocol: "ldap"})
	if err != nil || preview.LDAP.BindPassword != "rotated-password" {
		t.Fatalf("LDAP preview failed to retain saved password: %v", err)
	}
	if err := restarted.DeleteAuthenticationSource(ctx, "directory"); err != nil {
		t.Fatal(err)
	}
	state, err = restarted.RestoreAuthenticationSource(ctx, "directory")
	if err != nil || state.PendingRestart || !state.Configured.LDAP.BindPasswordSet {
		t.Fatalf("LDAP restore failed: %v %+v", err, state)
	}
	if err := restarted.DeleteAuthenticationSource(ctx, "directory"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatal("LDAP deletion changed existing document")
	}
}

func TestLDAPExplicitNestedTableSupportsEditsDeleteAndRestore(t *testing.T) {
	const directory = `
[auth.sources.directory]
protocol = "ldap"
enabled = true
display_name = "Directory"
admin_groups = ["cn=admins,dc=example"]
[auth.sources.directory.ldap]
url = "ldaps://directory.example" # retain URL comment
bind_dn = "cn=reader,dc=example"
bind_password = "nested-fixture-secret"
user_base_dn = "ou=people,dc=example"
user_filter = "(uid={username})"
subject_attribute = "entryUUID"
display_name_attribute = "displayName"
groups_attribute = "memberOf"
`
	const tail = "\n[cache]\nmax_size_gb = 20 # unrelated cache configuration\n"
	store, path := newOIDCStoreFixture(t, oidcStoreDocument+directory+tail)
	ctx := context.Background()
	states, err := store.AuthenticationSourceSettings(ctx)
	if err != nil || !states.ConfigWritable {
		t.Fatalf("standard nested LDAP source made all sources read-only: %v", err)
	}
	state, err := store.UpdateAuthenticationSource(ctx, "directory", OIDCSettingsPatch{DisplayName: oidcPointer("New name"), LDAP: &LDAPSettingsPatch{UserFilter: oidcPointer("(mail={username})")}})
	if err != nil || !state.SessionsWillReset || !state.Configured.LDAP.BindPasswordSet {
		t.Fatalf("nested partial update: %v", err)
	}
	raw, _ := os.ReadFile(path)
	for _, preserved := range []string{oidcStoreDocument, tail, "[auth.sources.directory.ldap]", "# retain URL comment"} {
		if !strings.Contains(string(raw), preserved) {
			t.Fatal("nested patch changed unrelated document or source shape")
		}
	}
	cfg, err := decodeConfigDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	ldap := cfg.Auth.Sources["directory"].LDAP
	if ldap.BindPassword != "nested-fixture-secret" || ldap.UserFilter != "(mail={username})" || ldap.UserBaseDN != "ou=people,dc=example" {
		t.Fatal("partial patch lost directory fields")
	}
	if _, err := store.UpdateAuthenticationSource(ctx, "oidc", OIDCSettingsPatch{DisplayName: oidcPointer("OIDC still editable")}); err != nil {
		t.Fatal("nested LDAP blocked another source's edit")
	}
	if err := store.DeleteAuthenticationSource(ctx, "directory"); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "nested-fixture-secret") || strings.Contains(string(raw), "auth.sources.directory") || !strings.Contains(string(raw), tail) {
		t.Fatal("nested source removal left private settings or damaged another table")
	}
	state, err = store.RestoreAuthenticationSource(ctx, "directory")
	if err != nil || state.PendingRestart || state.Configured.LDAP.UserFilter != "(uid={username})" || !state.Configured.LDAP.BindPasswordSet {
		t.Fatalf("nested startup baseline not restored: %v %+v", err, state)
	}
}

func TestLDAPSourceRejectsUnsafeOrMixedSettings(t *testing.T) {
	for _, test := range []struct {
		name, field string
		change      func(*AuthenticationSourceCreate)
	}{
		{"HTTP", "ldap.url", func(c *AuthenticationSourceCreate) { c.LDAP.URL = oidcPointer("http://directory.example") }},
		{"credentials in URL", "ldap.url", func(c *AuthenticationSourceCreate) { c.LDAP.URL = oidcPointer("ldaps://user:secret@directory.example") }},
		{"URL search path", "ldap.url", func(c *AuthenticationSourceCreate) { c.LDAP.URL = oidcPointer("ldap://directory.example/dc=example") }},
		{"empty bind password", "ldap.bind_password", func(c *AuthenticationSourceCreate) { c.LDAP.BindPassword = oidcPointer("") }},
		{"empty bind DN", "ldap.bind_dn", func(c *AuthenticationSourceCreate) { c.LDAP.BindDN = oidcPointer("") }},
		{"invalid base", "ldap.user_base_dn", func(c *AuthenticationSourceCreate) { c.LDAP.UserBaseDN = oidcPointer("not-a-DN") }},
		{"unstable subject", "ldap.subject_attribute", func(c *AuthenticationSourceCreate) { c.LDAP.SubjectAttribute = oidcPointer("uid") }},
		{"malformed filter", "ldap.user_filter", func(c *AuthenticationSourceCreate) { c.LDAP.UserFilter = oidcPointer("(uid={username}") }},
		{"unbound filter", "ldap.user_filter", func(c *AuthenticationSourceCreate) { c.LDAP.UserFilter = oidcPointer("(objectClass=*)") }},
		{"malformed CA", "ldap.ca_cert_pem", func(c *AuthenticationSourceCreate) { c.LDAP.CACertPEM = oidcPointer("private-not-a-certificate") }},
		{"OIDC setting", "issuer", func(c *AuthenticationSourceCreate) { c.Issuer = oidcPointer("https://oidc.example") }},
		{"group names", "admin_groups", func(c *AuthenticationSourceCreate) { c.AdminGroups = oidcPointer([]string{"admins"}) }},
		{"equivalent group overlap", "readonly_groups", func(c *AuthenticationSourceCreate) {
			c.ReadonlyGroups = oidcPointer([]string{"CN=ADMINS,OU=GROUPS,DC=EXAMPLE,DC=COM"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, path := newOIDCStoreFixture(t, oidcStoreDocument)
			before, _ := os.ReadFile(path)
			create := ldapSourceCreate()
			test.change(&create)
			_, err := store.CreateAuthenticationSource(context.Background(), create)
			if err == nil || AuthenticationFieldErrors(err)[test.field] == "" {
				t.Fatalf("missing field error: %v %v", err, AuthenticationFieldErrors(err))
			}
			if strings.Contains(err.Error(), "private-directory-password") || strings.Contains(err.Error(), "private-not-a-certificate") {
				t.Fatal("validation exposed credentials")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("invalid source persisted")
			}
		})
	}
	store, _ := newOIDCStoreFixture(t, oidcStoreDocument)
	_, err := store.UpdateAuthenticationSource(context.Background(), "oidc", OIDCSettingsPatch{LDAP: &LDAPSettingsPatch{URL: oidcPointer("ldaps://directory.example")}})
	if err == nil {
		t.Fatal("LDAP settings accepted by OIDC source")
	}
	_, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Name"), LDAP: &LDAPSettingsPatch{}})
	if err == nil {
		t.Fatal("legacy OIDC silently ignored LDAP patch")
	}
}

func TestLDAPEnvironmentOverridesRemainPrivateAndRestartBound(t *testing.T) {
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	create := ldapSourceCreate()
	create.Enabled = oidcPointer(false)
	create.LDAP.BindPassword = nil
	if _, err := store.CreateAuthenticationSource(context.Background(), create); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	t.Setenv("DEPSILO_AUTH_SOURCES_DIRECTORY_ENABLED", "true")
	t.Setenv("DEPSILO_AUTH_SOURCES_DIRECTORY_LDAP_BIND_PASSWORD", "environment-only-password")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Sources["directory"].LDAP.BindPassword != "environment-only-password" {
		t.Fatal("LDAP environment password not loaded")
	}
	store = NewStore(path, cfg, zap.NewAtomicLevel())
	t.Setenv("DEPSILO_AUTH_SOURCES_DIRECTORY_LDAP_BIND_PASSWORD", "changed-after-startup")
	state, err := store.UpdateAuthenticationSource(context.Background(), "directory", OIDCSettingsPatch{DisplayName: oidcPointer("Renamed")})
	if err != nil || state.SessionsWillReset || state.Configured.LDAP.BindPasswordSet || !state.Effective.LDAP.BindPasswordSet {
		t.Fatalf("environment authority changed: %v %+v", err, state)
	}
	preview, err := store.PreviewLDAPAuthenticationSourceDraft(context.Background(), AuthenticationSourceDraft{ID: "directory", Protocol: "ldap"})
	if err != nil || preview.LDAP.BindPassword != "environment-only-password" {
		t.Fatalf("preview did not use captured environment: %v", err)
	}
	_, err = store.UpdateAuthenticationSource(context.Background(), "directory", OIDCSettingsPatch{LDAP: &LDAPSettingsPatch{BindPassword: oidcPointer("replacement")}})
	if AuthenticationFieldErrors(err)["ldap.bind_password"] != "ENV_OVERRIDE" {
		t.Fatal("environment override edited")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "environment-only-password") || strings.Contains(string(raw), "changed-after-startup") {
		t.Fatal("environment credentials materialized into file")
	}
}

func TestLDAPAuthorityTracksDirectoryInsteadOfDisplayAndCredentials(t *testing.T) {
	base := LDAPConfig{URL: "ldaps://Directory.Example", UserBaseDN: "ou=People,dc=example,dc=com", SubjectAttribute: "entryUUID", BindDN: "cn=reader,dc=example,dc=com", BindPassword: "secret"}
	authority := LDAPAuthority("directory", base)
	copy := base
	copy.URL = "ldaps://directory.example:636"
	copy.BindDN = "cn=new-reader,dc=example,dc=com"
	copy.BindPassword = "rotated"
	if LDAPAuthority("directory", copy) != authority {
		t.Fatal("equivalent directory address or bind credentials changed identity authority")
	}
	for _, change := range []func(*LDAPConfig){func(c *LDAPConfig) { c.URL = "ldaps://other.example" }, func(c *LDAPConfig) { c.UserBaseDN = "ou=Other,dc=example,dc=com" }, func(c *LDAPConfig) { c.SubjectAttribute = "objectGUID" }} {
		copy := base
		change(&copy)
		if LDAPAuthority("directory", copy) == authority {
			t.Fatal("changed directory reused identity authority")
		}
	}
	if LDAPAuthority("other-source", base) == authority {
		t.Fatal("different source reused directory authority")
	}
}
