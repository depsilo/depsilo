package config

import (
	"context"
	"os"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestAuthenticationDraftPreviewPreservesSavedSecretWithoutMutation(t *testing.T) {
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	before, _ := os.ReadFile(path)
	cfg, err := store.PreviewAuthenticationSourceDraft(context.Background(), AuthenticationSourceDraft{ID: "oidc", Protocol: "oidc", Settings: OIDCSettingsPatch{DisplayName: oidcPointer("Preview name")}})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.ClientSecret != "saved-provider-secret" || cfg.DisplayName != "Preview name" {
		t.Fatal("preview did not merge saved private configuration")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("preview wrote document")
	}
	state, err := store.AuthenticationSourceSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Sources[0].PendingRestart || state.Sources[0].Configured.DisplayName != "Existing" || state.Sources[0].Effective.Enabled {
		t.Fatal("preview changed saved or effective state")
	}
	create := sourceCreate("company", "https://company.example")
	cfg, err = store.PreviewAuthenticationSourceDraft(context.Background(), AuthenticationSourceDraft{ID: create.ID, Protocol: create.Protocol, Create: true, Settings: create.OIDCSettingsPatch})
	if err != nil || cfg.ClientSecret != "private-source-secret" {
		t.Fatalf("new draft preview: %v", err)
	}
	state, err = store.AuthenticationSourceSettings(context.Background())
	if err != nil || len(state.Sources) != 1 {
		t.Fatal("new preview created source")
	}
}

func TestAuthenticationDraftPreviewUsesCapturedEnvironment(t *testing.T) {
	store, path := newOIDCStoreFixture(t, strings.Replace(oidcStoreDocument, "client_secret = \"saved-provider-secret\"\n", "", 1))
	raw, _ := os.ReadFile(path)
	cfg, err := decodeConfigDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_AUTH_OIDC_CLIENT_SECRET", "environment-secret")
	cfg.Auth.OIDC.ClientSecret = "environment-secret"
	store = NewStore(path, cfg, zap.NewAtomicLevel())
	t.Setenv("DEPSILO_AUTH_OIDC_CLIENT_SECRET", "later-environment-secret")
	preview, err := store.PreviewAuthenticationSourceDraft(context.Background(), AuthenticationSourceDraft{ID: "oidc", Protocol: "oidc"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ClientSecret != "environment-secret" {
		t.Fatal("preview changed running environment authority")
	}
	_, err = store.PreviewAuthenticationSourceDraft(context.Background(), AuthenticationSourceDraft{ID: "oidc", Protocol: "oidc", Settings: OIDCSettingsPatch{ClientSecret: oidcPointer("replacement")}})
	if AuthenticationFieldErrors(err)["client_secret"] != "ENV_OVERRIDE" {
		t.Fatal("missing environment field error")
	}
	after, _ := os.ReadFile(path)
	if string(raw) != string(after) || strings.Contains(string(after), "environment-secret") {
		t.Fatal("preview persisted environment")
	}
}

func TestAuthenticationFieldErrorsAreStableAndValueFree(t *testing.T) {
	for _, test := range []struct {
		name, field, code string
		patch             OIDCSettingsPatch
	}{
		{"required secret", "client_secret", "REQUIRED", OIDCSettingsPatch{ClientSecret: oidcPointer("")}},
		{"bad secret", "client_secret", "INVALID_VALUE", OIDCSettingsPatch{ClientSecret: oidcPointer("private\nsecret")}},
		{"bad issuer", "issuer", "INVALID_URL", OIDCSettingsPatch{Issuer: oidcPointer("https://private-secret@identity.example")}},
		{"callback path", "redirect_url", "CALLBACK_PATH", OIDCSettingsPatch{RedirectURL: oidcPointer("https://depsilo.example/wrong")}},
		{"bad ttl", "session_ttl", "INVALID_VALUE", OIDCSettingsPatch{SessionTTL: oidcPointer("private-secret")}},
		{"missing groups", "admin_groups", "GROUPS_REQUIRED", OIDCSettingsPatch{AdminGroups: oidcPointer([]string{}), ReadonlyGroups: oidcPointer([]string{})}},
		{"overlap groups", "admin_groups", "GROUPS_OVERLAP", OIDCSettingsPatch{ReadonlyGroups: oidcPointer([]string{"operators"})}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := newOIDCStoreFixture(t, oidcStoreDocument)
			_, err := store.PreviewAuthenticationSourceDraft(context.Background(), AuthenticationSourceDraft{ID: "oidc", Protocol: "oidc", Settings: test.patch})
			if err == nil || AuthenticationFieldErrors(err)[test.field] != test.code {
				t.Fatalf("missing structured field error: %v %v", err, AuthenticationFieldErrors(err))
			}
			if strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "private\nsecret") {
				t.Fatal("error includes supplied secret")
			}
		})
	}
}

func TestRestoreAuthenticationSourceUsesOriginalFileSnapshot(t *testing.T) {
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	ctx := context.Background()
	if _, err := store.UpdateAuthenticationSource(ctx, "oidc", OIDCSettingsPatch{DisplayName: oidcPointer("Pending change")}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAuthenticationSource(ctx, "oidc"); err != nil {
		t.Fatal(err)
	}
	state, err := store.AuthenticationSourceSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Sources) != 1 || !state.Sources[0].CanRestore {
		t.Fatal("pending removal cannot restore captured baseline")
	}
	restored, err := store.RestoreAuthenticationSource(ctx, "oidc")
	if err != nil {
		t.Fatal(err)
	}
	if restored.Configured == nil || restored.Configured.DisplayName != "Existing" || restored.PendingRestart || restored.CanRestore {
		t.Fatal("restored source differs from original running configuration")
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "saved-provider-secret") || !strings.Contains(string(raw), "preserve-license") {
		t.Fatal("restore lost source or unrelated secret")
	}
}

func TestRestoreAuthenticationSourceDoesNotMaterializeEnvironment(t *testing.T) {
	create := sourceCreate("company", "https://company.example")
	store, path := newOIDCStoreFixture(t, "")
	if _, err := store.CreateAuthenticationSource(context.Background(), create); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	raw = []byte(strings.Replace(string(raw), "client_secret = \"private-source-secret\"\n", "", 1))
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeConfigDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	source := cfg.Auth.Sources["company"]
	source.ClientSecret = "environment-only-private-secret"
	cfg.Auth.Sources["company"] = source
	t.Setenv("DEPSILO_AUTH_SOURCES_COMPANY_CLIENT_SECRET", source.ClientSecret)
	store = NewStore(path, cfg, zap.NewAtomicLevel())
	// A deployment edit may remove a source outside Admin; restoring must still
	// use its saved file values instead of copying the effective env credential.
	if err := os.WriteFile(path, []byte("# retained configuration\n"), 0600); err != nil {
		t.Fatal(err)
	}
	restored, err := store.RestoreAuthenticationSource(context.Background(), "company")
	if err != nil {
		t.Fatal(err)
	}
	if restored.PendingRestart || restored.Configured.ClientSecretSet || !restored.Effective.ClientSecretSet {
		t.Fatal("restore changed environment ownership")
	}
	after, _ := os.ReadFile(path)
	if strings.Contains(string(after), "client_secret") || strings.Contains(string(after), "environment-only-private-secret") {
		t.Fatal("restore materialized environment field")
	}
}

func TestRestoreRequiresReliableOriginalBaseline(t *testing.T) {
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	cfg, err := decodeConfigDocument([]byte(oidcStoreDocument))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Auth.OIDC.ClientSecret = "runtime-secret-not-from-file"
	store = NewStore(path, cfg, zap.NewAtomicLevel())
	if err := store.DeleteAuthenticationSource(context.Background(), "oidc"); err != nil {
		t.Fatal(err)
	}
	state, err := store.AuthenticationSourceSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Sources[0].CanRestore {
		t.Fatal("unsafe effective-to-file restoration offered")
	}
	_, err = store.RestoreAuthenticationSource(context.Background(), "oidc")
	requireOIDCStoreCode(t, err, StoreAuthenticationSourceConflict)
}
