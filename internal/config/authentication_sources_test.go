package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestAuthenticationSourcesNoPhantomDefaultAndUnknownSingletonSection(t *testing.T) {
	cfg, err := decodeConfigDocument(nil)
	if err != nil {
		t.Fatal(err)
	}
	if sources := AuthenticationSources(cfg.Auth); len(sources) != 0 {
		t.Fatalf("defaults created a source: %v", sources)
	}
	if _, err := decodeConfigDocument([]byte("[auth.oidc]\nenabled=false\n")); err == nil {
		t.Fatal("unprefixed [auth.oidc] section accepted")
	}
	cfg, err = decodeConfigDocument([]byte("[auth.sources.oidc]\nprotocol=\"oidc\"\ndisplay_name=\"Casdoor\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if sources := AuthenticationSources(cfg.Auth); len(sources) != 1 || sources["oidc"].DisplayName != "Casdoor" {
		t.Fatal("source ID oidc is not an ordinary source")
	}
}

func TestAuthenticationSourcesDefaultsEnvironmentAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	document := "[auth.sources.company-sso]\nprotocol=\"oidc\"\n"
	if err := os.WriteFile(path, []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	setTestJWTSecret(t)
	for field, value := range map[string]string{"ENABLED": "true", "DISPLAY_NAME": "Company", "ISSUER": "https://id.example", "CLIENT_ID": "client", "CLIENT_SECRET": "environment-secret", "REDIRECT_URL": "https://depsilo.example/api/v1/auth/oidc/company-sso/callback", "ADMIN_GROUPS": "admins,ops"} {
		t.Setenv("DEPSILO_AUTH_SOURCES_COMPANY_SSO_"+field, value)
	}
	t.Setenv("DEPSILO_AUTH_SOURCES_GHOST_ENABLED", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	source := cfg.Auth.Sources["company-sso"]
	if len(AuthenticationSources(cfg.Auth)) != 1 || source.ClientSecret != "environment-secret" || len(source.AdminGroups) != 2 || source.GroupsClaim != "groups" || source.SessionTTL != 30*time.Minute {
		t.Fatal("source defaults/overrides not decoded")
	}
	raw, err := decodeConfigDocument([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	if raw.Auth.Sources["company-sso"].ClientSecret != "" || raw.Auth.Sources["company-sso"].Enabled {
		t.Fatal("file-only decode used environment")
	}
	store := NewStore(path, cfg, zap.NewAtomicLevel())
	state, err := store.AuthenticationSourceSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Sources[0].PendingRestart || state.Sources[0].Configured.ClientSecretSet || !state.Sources[0].Effective.ClientSecretSet {
		t.Fatal("wrong environment ownership state")
	}
	_, err = store.UpdateAuthenticationSource(context.Background(), "company-sso", OIDCSettingsPatch{DisplayName: oidcPointer("Renamed")})
	requireOIDCStoreCode(t, err, StoreInvalidSetting)
	_, err = store.UpdateAuthenticationSource(context.Background(), "company-sso", OIDCSettingsPatch{SessionTTL: oidcPointer("20m")})
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(path)
	if strings.Contains(string(updated), "environment-secret") || strings.Contains(string(updated), "client_secret") {
		t.Fatal("environment secret persisted")
	}
	err = store.DeleteAuthenticationSource(context.Background(), "company-sso")
	requireOIDCStoreCode(t, err, StoreConfigReadOnly)
	t.Setenv("DEPSILO_AUTH_SOURCES_COMPANY_SSO_REDIRECT_URL", "https://depsilo.example/api/v1/auth/oidc/callback")
	if _, err := Load(); err == nil {
		t.Fatal("unprefixed callback accepted for a source")
	}
}

func sourceCreate(id, issuer string) AuthenticationSourceCreate {
	return AuthenticationSourceCreate{ID: id, Protocol: "oidc", OIDCSettingsPatch: OIDCSettingsPatch{Enabled: oidcPointer(true), DisplayName: oidcPointer(id), Issuer: oidcPointer(issuer), ClientID: oidcPointer("depsilo"), ClientSecret: oidcPointer("private-source-secret"), RedirectURL: oidcPointer("https://depsilo.example/api/v1/auth/oidc/" + id + "/callback"), AdminGroups: oidcPointer([]string{"admins"})}}
}

func TestAuthenticationSourcesCRUDPreservesDocumentAndRestartLifecycle(t *testing.T) {
	ctx := context.Background()
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	before, _ := os.ReadFile(path)
	source, err := store.CreateAuthenticationSource(ctx, sourceCreate("company", "https://company.example"))
	if err != nil {
		t.Fatal(err)
	}
	if source.Effective != nil || source.Configured == nil || !source.PendingRestart {
		t.Fatal("new source changed running authority")
	}
	raw, _ := json.Marshal(source)
	if strings.Contains(string(raw), "private-source-secret") {
		t.Fatal("secret exposed")
	}
	document, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(document), string(before)) {
		t.Fatal("new source changed existing document")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("secret did not restrict permissions")
	}
	_, err = store.CreateAuthenticationSource(ctx, sourceCreate("other", "https://company.example"))
	requireOIDCStoreCode(t, err, StoreInvalidSetting)
	_, err = store.CreateAuthenticationSource(ctx, sourceCreate("company", "https://different.example"))
	requireOIDCStoreCode(t, err, StoreAuthenticationSourceConflict)
	_, err = store.UpdateAuthenticationSource(ctx, "company", OIDCSettingsPatch{DisplayName: oidcPointer("Company Login"), AdminGroups: oidcPointer([]string{"new-admins", "ops"})})
	if err != nil {
		t.Fatal(err)
	}
	document, _ = os.ReadFile(path)
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewStore(path, cfg, zap.NewAtomicLevel())
	if err := restarted.DeleteAuthenticationSource(ctx, "company"); err != nil {
		t.Fatal(err)
	}
	state, err := restarted.AuthenticationSourceSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Sources) != 2 || state.Sources[0].ID != "company" || state.Sources[0].Configured != nil || state.Sources[0].Effective == nil || !state.Sources[0].PendingRestart {
		t.Fatal("pending removal lost running source")
	}
	_, err = restarted.CreateAuthenticationSource(ctx, sourceCreate("company", "https://new.example"))
	requireOIDCStoreCode(t, err, StoreAuthenticationSourceConflict)
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatalf("source removal modified unrelated file:\n%s", after)
	}
	if err := restarted.DeleteAuthenticationSource(ctx, "existing"); err != nil {
		t.Fatal(err)
	}
	final, _ := os.ReadFile(path)
	if !strings.Contains(string(final), "jwt_secret = \"local-signing-key-stays-unchanged\"") || !strings.Contains(string(final), "[license]\nkey = \"preserve-license\"") {
		t.Fatal("source removal damaged unrelated settings")
	}
	cfg, err = decodeConfigDocument(final)
	if err != nil {
		t.Fatal(err)
	}
	if len(AuthenticationSources(cfg.Auth)) != 0 {
		t.Fatal("removed source came back through defaults")
	}
}

func TestAuthenticationSourcesRejectInvalidChangesAndReadOnlyShapes(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name   string
		mutate func(*AuthenticationSourceCreate)
	}{
		{"path", func(c *AuthenticationSourceCreate) { c.ID = "../bad" }},
		{"uppercase", func(c *AuthenticationSourceCreate) { c.ID = "Company" }},
		{"protocol", func(c *AuthenticationSourceCreate) { c.Protocol = "ldap" }},
		{"wrong callback", func(c *AuthenticationSourceCreate) {
			c.RedirectURL = oidcPointer("https://depsilo.example/api/v1/auth/oidc/other/callback")
		}},
		{"duplicate issuer", func(c *AuthenticationSourceCreate) { c.Issuer = oidcPointer("https://identity.example") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := newOIDCStoreFixture(t, oidcStoreDocument)
			create := sourceCreate("company", "https://company.example")
			test.mutate(&create)
			_, err := store.CreateAuthenticationSource(ctx, create)
			requireOIDCStoreCode(t, err, StoreInvalidSetting)
		})
	}
	for _, document := range []string{
		`auth.sources.company = { protocol="oidc", enabled=false }`,
		"[auth.sources]\ncompany.protocol=\"oidc\"\ncompany.enabled=false\n",
		"[auth]\nsources = { company = { protocol=\"oidc\",enabled=false } }\n",
	} {
		t.Run(document[:min(len(document), 30)], func(t *testing.T) {
			store, path := newOIDCStoreFixture(t, document)
			state, err := store.AuthenticationSourceSettings(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if state.ConfigWritable || state.Sources[0].ConfigWritable {
				t.Fatal("unsupported shape writable")
			}
			_, err = store.UpdateAuthenticationSource(ctx, "company", OIDCSettingsPatch{DisplayName: oidcPointer("Renamed")})
			requireOIDCStoreCode(t, err, StoreConfigReadOnly)
			err = store.DeleteAuthenticationSource(ctx, "company")
			requireOIDCStoreCode(t, err, StoreConfigReadOnly)
			raw, _ := os.ReadFile(path)
			if string(raw) != document {
				t.Fatal("read-only document changed")
			}
		})
	}
}

func TestAuthenticationSourcesCallbackOriginAndDisabledRecovery(t *testing.T) {
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	ctx := context.Background()
	_, err := store.CreateAuthenticationSource(ctx, sourceCreate("one", "https://one.example"))
	if err != nil {
		t.Fatal(err)
	}
	two := sourceCreate("two", "https://two.example")
	two.RedirectURL = oidcPointer("https://other-depsilo.example/api/v1/auth/oidc/two/callback")
	_, err = store.CreateAuthenticationSource(ctx, two)
	requireOIDCStoreCode(t, err, StoreInvalidSetting)
	// A dormant source can contain stale values and still be disabled or renamed.
	raw, _ := os.ReadFile(path)
	raw = append(raw, []byte("\n[auth.sources.dormant]\nprotocol=\"oidc\"\nenabled=false\nissuer=\"old invalid URL\"\nclient_secret=\" bad secret \"\n")...)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdateAuthenticationSource(ctx, "dormant", OIDCSettingsPatch{Enabled: oidcPointer(false), DisplayName: oidcPointer("Archived")})
	if err != nil {
		t.Fatalf("disabled recovery blocked: %v", err)
	}
	_, err = store.UpdateAuthenticationSource(ctx, "existing", OIDCSettingsPatch{Issuer: oidcPointer("https://one.example")})
	requireOIDCStoreCode(t, err, StoreInvalidSetting)
}

func TestAuthenticationSourcesAtomicAndConcurrentEdits(t *testing.T) {
	ctx := context.Background()
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	original, _ := os.ReadFile(path)
	store.writer = &failingOIDCWriter{}
	_, err := store.CreateAuthenticationSource(ctx, sourceCreate("company", "https://company.example"))
	requireOIDCStoreCode(t, err, StoreConfigWriteFailed)
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("failed write changed document")
	}
	store.writer = osAtomicFileWriter{}
	var group sync.WaitGroup
	errors := make(chan error, 3)
	group.Add(3)
	go func() {
		defer group.Done()
		_, err := store.CreateAuthenticationSource(ctx, sourceCreate("one", "https://one.example"))
		errors <- err
	}()
	go func() {
		defer group.Done()
		_, err := store.CreateAuthenticationSource(ctx, sourceCreate("two", "https://two.example"))
		errors <- err
	}()
	go func() {
		defer group.Done()
		_, err := store.UpdateAuthenticationSource(ctx, "existing", OIDCSettingsPatch{DisplayName: oidcPointer("Existing updated")})
		errors <- err
	}()
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := os.ReadFile(path)
	cfg, err := decodeConfigDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(AuthenticationSources(cfg.Auth)) != 3 || cfg.Auth.Sources["existing"].DisplayName != "Existing updated" {
		t.Fatal("concurrent editor lost update")
	}
}

func TestAuthenticationSourceOriginalIDsAreNotNormalized(t *testing.T) {
	for _, document := range []string{
		"[auth.sources.Company]\nprotocol=\"oidc\"\n",
		"auth.sources.Company={protocol=\"oidc\"}\n",
	} {
		if _, err := decodeConfigDocument([]byte(document)); err == nil {
			t.Fatal("uppercase ID silently normalized")
		}
	}
}

func TestAuthenticationSourcesNestedEmptyTablesAreReadOnly(t *testing.T) {
	for _, document := range []string{
		"[auth.sources.company]\nprotocol=\"oidc\"\nenabled=false\n[auth.sources.company.extra]\n",
	} {
		store, path := newOIDCStoreFixture(t, document)
		state, err := store.AuthenticationSourceSettings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if state.ConfigWritable || len(state.Sources) != 1 || state.Sources[0].ConfigWritable {
			t.Fatal("nested table misreported as writable")
		}
		err = store.DeleteAuthenticationSource(context.Background(), state.Sources[0].ID)
		requireOIDCStoreCode(t, err, StoreConfigReadOnly)
		raw, _ := os.ReadFile(path)
		if string(raw) != document {
			t.Fatal("nested table deletion modified file")
		}
	}
}

func TestAuthenticationSourceIDOIDCIsOrdinary(t *testing.T) {
	store, path := newOIDCStoreFixture(t, "")
	if _, err := store.CreateAuthenticationSource(context.Background(), sourceCreate("oidc", "https://company.example")); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	cfg, err := decodeConfigDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[auth.sources.oidc]") || cfg.Auth.Sources["oidc"].RedirectURL != "https://depsilo.example/api/v1/auth/oidc/oidc/callback" {
		t.Fatalf("source ID oidc was not stored as an ordinary source:\n%s", raw)
	}
}
