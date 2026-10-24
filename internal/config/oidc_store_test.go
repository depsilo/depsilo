package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

const oidcStoreDocument = `# preserve operator configuration
[server]
log_level = "info" # keep this comment
[auth]
jwt_secret = "local-signing-key-stays-unchanged"
[auth.oidc]
enabled = false
client_secret = "saved-provider-secret"
display_name = "Existing"
issuer = "https://identity.example"
client_id = "depsilo"
redirect_url = "https://depsilo.example/api/v1/auth/oidc/callback"
admin_groups = [
  "operators", # keep semantics even when changing the array
]
readonly_groups = []
[license]
key = "preserve-license"
`

func newOIDCStoreFixture(t *testing.T, document string) (*Store, string) {
	t.Helper()
	for _, field := range oidcFields {
		t.Setenv(oidcEnvName(field), "")
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(document), 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeConfigDocument([]byte(document))
	if err != nil {
		t.Fatal(err)
	}
	return NewStore(path, cfg, zap.NewAtomicLevel()), path
}

func oidcPointer[T any](value T) *T { return &value }
func requireOIDCStoreCode(t *testing.T, err error, code StoreErrorCode) {
	t.Helper()
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != code {
		t.Fatalf("wanted %s, got %v", code, err)
	}
}

func TestOIDCSettingsWritePreservesSecretsAndRestartAuthority(t *testing.T) {
	store, path := newOIDCStoreFixture(t, oidcStoreDocument)
	initial, err := store.OIDCSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if initial.PendingRestart || !initial.Configured.ClientSecretSet || initial.Effective.Enabled {
		t.Fatal("unexpected initial state")
	}
	state, err := store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{Enabled: oidcPointer(true), DisplayName: oidcPointer("Company Login"), AdminGroups: oidcPointer([]string{"admin-team", "ops"}), ReadonlyGroups: oidcPointer([]string{"readers"})})
	if err != nil {
		t.Fatal(err)
	}
	if !state.PendingRestart || !state.Configured.Enabled || state.Effective.Enabled || !state.Configured.ClientSecretSet {
		t.Fatal("save changed live authority or dropped secret")
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"saved-provider-secret", "local-signing-key-stays-unchanged"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("secret exposed in state")
		}
	}
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, unchanged := range []string{"# preserve operator configuration", "log_level = \"info\" # keep this comment", "jwt_secret = \"local-signing-key-stays-unchanged\"", "client_secret = \"saved-provider-secret\"", "[license]\nkey = \"preserve-license\""} {
		if !strings.Contains(string(document), unchanged) {
			t.Fatalf("unrelated configuration changed: %s", unchanged)
		}
	}
	next, err := decodeConfigDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Auth.OIDC.AdminGroups) != 2 || len(next.Auth.OIDC.ReadonlyGroups) != 1 {
		t.Fatal("groups not persisted")
	}
	reopened := NewStore(path, next, zap.NewAtomicLevel())
	state, err = reopened.OIDCSettings(context.Background())
	if err != nil || state.PendingRestart || !state.Effective.Enabled {
		t.Fatalf("restart state invalid: %v", err)
	}
	// Secret replacement must also require restart despite unchanged redacted snapshots.
	state, err = reopened.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{ClientSecret: oidcPointer("replacement-provider-secret")})
	if err != nil || !state.PendingRestart {
		t.Fatalf("secret rotation not detected: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("secret update should restrict file permissions")
	}
	state, err = reopened.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{Enabled: oidcPointer(false), ClientSecret: oidcPointer("")})
	if err != nil || state.Configured.ClientSecretSet {
		t.Fatalf("explicit clear failed: %v", err)
	}
}

func TestOIDCSettingsEnvironmentRemainsAuthoritativeAndPrivate(t *testing.T) {
	_, path := newOIDCStoreFixture(t, strings.ReplaceAll(strings.ReplaceAll(oidcStoreDocument, "enabled = false", "enabled = true"), "client_secret = \"saved-provider-secret\"\n", ""))
	document, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_AUTH_OIDC_CLIENT_SECRET", "environment-only-secret")
	cfg.Auth.OIDC.ClientSecret = "environment-only-secret"
	store := NewStore(path, cfg, zap.NewAtomicLevel())
	state, err := store.OIDCSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.PendingRestart || state.Configured.ClientSecretSet || !state.Effective.ClientSecretSet || state.Overrides["client_secret"] != "DEPSILO_AUTH_OIDC_CLIENT_SECRET" {
		t.Fatal("incorrect environment state")
	}
	_, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{ClientSecret: oidcPointer("overwrite")})
	requireOIDCStoreCode(t, err, StoreInvalidSetting)
	state, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Updated")})
	if err != nil || !state.PendingRestart {
		t.Fatalf("environment-backed save failed: %v", err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(updated), "client_secret") || strings.Contains(string(updated), "environment-only-secret") {
		t.Fatal("environment secret materialized")
	}
	// Later process environment changes are not treated as a runtime reload.
	t.Setenv("DEPSILO_AUTH_OIDC_CLIENT_SECRET", "different-process-value")
	state, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Existing")})
	if err != nil || state.PendingRestart {
		t.Fatalf("startup environment capture changed: %v", err)
	}
}

func TestOIDCSettingsRejectsInvalidOrUnwritableChanges(t *testing.T) {
	cases := []struct {
		name  string
		patch OIDCSettingsPatch
	}{
		{"empty", OIDCSettingsPatch{}},
		{"invalid disabled issuer", OIDCSettingsPatch{Issuer: oidcPointer("http://identity.example")}},
		{"invalid disabled callback", OIDCSettingsPatch{RedirectURL: oidcPointer("https://depsilo.example/other")}},
		{"missing enabled secret", OIDCSettingsPatch{Enabled: oidcPointer(true), ClientSecret: oidcPointer("")}},
		{"overlapping groups", OIDCSettingsPatch{ReadonlyGroups: oidcPointer([]string{"operators"})}},
		{"oversized secret", OIDCSettingsPatch{ClientSecret: oidcPointer(strings.Repeat("s", 4097))}},
		{"short ttl", OIDCSettingsPatch{SessionTTL: oidcPointer("1m")}},
		{"invalid ttl", OIDCSettingsPatch{SessionTTL: oidcPointer("private-value")}},
		{"control name", OIDCSettingsPatch{DisplayName: oidcPointer("bad\nname")}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store, path := newOIDCStoreFixture(t, oidcStoreDocument)
			_, err := store.UpdateOIDCSettings(context.Background(), test.patch)
			requireOIDCStoreCode(t, err, StoreInvalidSetting)
			if strings.Contains(err.Error(), "private-value") {
				t.Fatal("submitted invalid value leaked")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != oidcStoreDocument {
				t.Fatal("rejected update changed file")
			}
		})
	}
	t.Run("read only", func(t *testing.T) {
		store, path := newOIDCStoreFixture(t, oidcStoreDocument)
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}
		_, err := store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Update")})
		requireOIDCStoreCode(t, err, StoreConfigReadOnly)
	})
	t.Run("failed atomic write", func(t *testing.T) {
		store, path := newOIDCStoreFixture(t, oidcStoreDocument)
		store.writer = &failingOIDCWriter{}
		_, err := store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Update")})
		requireOIDCStoreCode(t, err, StoreConfigWriteFailed)
		after, _ := os.ReadFile(path)
		if string(after) != oidcStoreDocument {
			t.Fatal("failed write changed file")
		}
	})
}

type failingOIDCWriter struct{}

func (*failingOIDCWriter) Write(string, []byte, os.FileMode) (atomicWriteOutcome, error) {
	return atomicWriteOutcome{}, errors.New("write failed with private details")
}

func TestOIDCSettingsAddsSectionAndPreservesDocumentShapes(t *testing.T) {
	for _, document := range []string{"", "[auth]\ntoken_ttl = \"24h\"", "[server]\nlog_level = \"debug\"\r\n"} {
		store, path := newOIDCStoreFixture(t, document)
		_, err := store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Generic OIDC")})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(raw), document) {
			t.Fatal("existing document changed")
		}
		cfg, err := decodeConfigDocument(raw)
		if err != nil || cfg.Auth.OIDC.DisplayName != "Generic OIDC" {
			t.Fatalf("new section invalid: %v", err)
		}
	}
	// An array containing a closing bracket in a value or comment must still be
	// replaced as one value while preserving its following inline comment.
	store, path := newOIDCStoreFixture(t, strings.Replace(oidcStoreDocument, "  \"operators\", # keep semantics even when changing the array\n]", "  \"ops]team\", # ] ignored\n] # outer stays", 1))
	_, err := store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{AdminGroups: oidcPointer([]string{}), SessionTTL: oidcPointer((15 * time.Minute).String())})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "admin_groups = [] # outer stays") {
		t.Fatal("array range or outside comment lost")
	}
}

func TestOIDCSettingsDisablePreservesExistingLargeGroupMapping(t *testing.T) {
	groups := make([]string, 101)
	for i := range groups {
		groups[i] = fmt.Sprintf("group-%d", i)
	}
	document := strings.Replace(oidcStoreDocument, "enabled = false", "enabled = true", 1)
	document = strings.Replace(document, "admin_groups = [\n  \"operators\", # keep semantics even when changing the array\n]", "admin_groups = "+string(renderOIDCValue(groups)), 1)
	store, path := newOIDCStoreFixture(t, document)
	state, err := store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{Enabled: oidcPointer(false)})
	if err != nil {
		t.Fatal(err)
	}
	if state.Configured.Enabled || !state.Effective.Enabled || !state.PendingRestart || len(state.Configured.AdminGroups) != 101 {
		t.Fatal("disable did not preserve existing role mappings")
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != strings.Replace(document, "enabled = true", "enabled = false", 1) {
		t.Fatal("disable changed unrelated settings")
	}
}

func TestOIDCSettingsDisabledLegacyValuesDoNotBlockRecoveryEdits(t *testing.T) {
	document := strings.Replace(oidcStoreDocument, "https://identity.example", "http://legacy-identity.example", 1)
	store, path := newOIDCStoreFixture(t, document)
	state, err := store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Renamed")})
	if err != nil || state.Configured.DisplayName != "Renamed" {
		t.Fatalf("unrelated edit rejected: %v", err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "http://legacy-identity.example") {
		t.Fatal("legacy value was silently rewritten")
	}
	_, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{Enabled: oidcPointer(true)})
	requireOIDCStoreCode(t, err, StoreInvalidSetting)
	_, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{Issuer: oidcPointer("http://different-invalid.example")})
	requireOIDCStoreCode(t, err, StoreInvalidSetting)
	_, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{Issuer: oidcPointer("https://fixed.example"), Enabled: oidcPointer(true)})
	if err != nil {
		t.Fatalf("repair and enable rejected: %v", err)
	}
}

func TestOIDCSettingsUnsupportedDocumentShapeIsReadOnly(t *testing.T) {
	for _, document := range []string{
		"auth.oidc.display_name = \"Existing\"\n",
		"[auth]\noidc = { display_name = \"Existing\" }\n",
		"auth = { oidc = { display_name = \"Existing\" } }\n",
	} {
		t.Run(document, func(t *testing.T) {
			store, path := newOIDCStoreFixture(t, document)
			state, err := store.OIDCSettings(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if state.ConfigWritable || state.Configured.DisplayName != "Existing" {
				t.Fatal("unsupported configuration shape misrepresented")
			}
			_, err = store.UpdateOIDCSettings(context.Background(), OIDCSettingsPatch{DisplayName: oidcPointer("Changed")})
			requireOIDCStoreCode(t, err, StoreConfigReadOnly)
			raw, _ := os.ReadFile(path)
			if string(raw) != document {
				t.Fatal("unsupported document changed")
			}
		})
	}
}
