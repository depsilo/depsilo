package config

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestAuthenticationSourceSessionImpactUsesSecuritySettings(t *testing.T) {
	for _, test := range []struct {
		name    string
		patch   OIDCSettingsPatch
		pending bool
		reset   bool
	}{
		{"unchanged", OIDCSettingsPatch{DisplayName: oidcPointer("Existing")}, false, false},
		{"display name", OIDCSettingsPatch{DisplayName: oidcPointer("Company login")}, true, false},
		{"disabled", OIDCSettingsPatch{Enabled: oidcPointer(false)}, true, true},
		{"issuer", OIDCSettingsPatch{Issuer: oidcPointer("https://replacement.example")}, true, true},
		{"client", OIDCSettingsPatch{ClientID: oidcPointer("replacement")}, true, true},
		{"secret", OIDCSettingsPatch{ClientSecret: oidcPointer("replacement")}, true, true},
		{"callback", OIDCSettingsPatch{RedirectURL: oidcPointer("https://new-depsilo.example/api/v1/auth/oidc/callback")}, true, true},
		{"admin groups", OIDCSettingsPatch{AdminGroups: oidcPointer([]string{"new-operators"})}, true, true},
		{"readonly groups", OIDCSettingsPatch{ReadonlyGroups: oidcPointer([]string{"readers"})}, true, true},
		{"groups claim", OIDCSettingsPatch{GroupsClaim: oidcPointer("roles")}, true, true},
		{"session ttl", OIDCSettingsPatch{SessionTTL: oidcPointer("15m")}, true, true},
		{"loopback policy", OIDCSettingsPatch{AllowInsecureLoopback: oidcPointer(true)}, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := newOIDCStoreFixture(t, strings.Replace(oidcStoreDocument, "enabled = false", "enabled = true", 1))
			state, err := store.UpdateAuthenticationSource(context.Background(), "oidc", test.patch)
			if err != nil {
				t.Fatal(err)
			}
			if state.PendingRestart != test.pending || state.SessionsWillReset != test.reset {
				t.Fatalf("wrong activation impact: pending=%t reset=%t, want pending=%t reset=%t", state.PendingRestart, state.SessionsWillReset, test.pending, test.reset)
			}
		})
	}
}

func TestAuthenticationSourceSessionImpactRequiresRunningEnabledSource(t *testing.T) {
	ctx := context.Background()
	store, _ := newOIDCStoreFixture(t, oidcStoreDocument)
	state, err := store.UpdateAuthenticationSource(ctx, "oidc", OIDCSettingsPatch{Enabled: oidcPointer(true)})
	if err != nil {
		t.Fatal(err)
	}
	if !state.PendingRestart || state.SessionsWillReset {
		t.Fatal("enabling a dormant source claimed to revoke existing sessions")
	}
	state, err = store.CreateAuthenticationSource(ctx, sourceCreate("company", "https://company.example"))
	if err != nil {
		t.Fatal(err)
	}
	if !state.PendingRestart || state.SessionsWillReset {
		t.Fatal("adding a source claimed to revoke existing sessions")
	}
	for _, enabled := range []bool{false, true} {
		document := oidcStoreDocument
		if enabled {
			document = strings.Replace(document, "enabled = false", "enabled = true", 1)
		}
		store, _ := newOIDCStoreFixture(t, document)
		if err := store.DeleteAuthenticationSource(ctx, "oidc"); err != nil {
			t.Fatal(err)
		}
		states, err := store.AuthenticationSourceSettings(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(states.Sources) != 1 || !states.Sources[0].PendingRestart || states.Sources[0].SessionsWillReset != enabled {
			t.Fatal("source removal reported wrong running-session impact")
		}
	}
}

func TestAuthenticationSourceSessionImpactUsesCapturedEnvironment(t *testing.T) {
	document := strings.Replace(oidcStoreDocument, "enabled = false", "enabled = true", 1)
	_, path := newOIDCStoreFixture(t, document)
	cfg, err := decodeConfigDocument([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_AUTH_OIDC_CLIENT_SECRET", "environment-secret")
	cfg.Auth.OIDC.ClientSecret = "environment-secret"
	store := NewStore(path, cfg, zap.NewAtomicLevel())
	t.Setenv("DEPSILO_AUTH_OIDC_CLIENT_SECRET", "changed-after-startup")
	state, err := store.UpdateAuthenticationSource(context.Background(), "oidc", OIDCSettingsPatch{DisplayName: oidcPointer("Renamed")})
	if err != nil {
		t.Fatal(err)
	}
	if !state.PendingRestart || state.SessionsWillReset {
		t.Fatal("rename compared file/environment secrets instead of captured startup authority")
	}
}
