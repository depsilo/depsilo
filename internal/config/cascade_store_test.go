package config

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func stringPointer(value string) *string { return &value }

func newCascadeStoreFixture(t *testing.T, document string) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	effective, err := decodeConfigDocument([]byte(document))
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return newStore(path, effective, zap.NewAtomicLevel(), osAtomicFileWriter{}), path
}

const cascadeStoreDocument = `config_version = 1

[cache]
max_size_gb = 20

[cascade]
enabled = false

# operator comment stays put
[[cascade.peers]]
name = "home"
url = "http://127.0.0.1:23333"

[license]
key = ""
`

func TestCascadeConfigStateMasksTokens(t *testing.T) {
	document := strings.Replace(cascadeStoreDocument,
		"url = \"http://127.0.0.1:23333\"",
		"url = \"http://127.0.0.1:23333\"\ntoken = \"peer-secret-token-0123456789\"",
		1,
	)
	document = strings.Replace(document, "enabled = false", "enabled = true\ntoken = \"shared-secret-token-0123456789\"", 1)
	store, _ := newCascadeStoreFixture(t, document)

	state, err := store.CascadeConfig(context.Background())
	if err != nil {
		t.Fatalf("CascadeConfig: %v", err)
	}
	if !state.Enabled || !state.TokenSet || state.MaxHops != defaultCascadeMaxHops || state.MaxTTL != "168h" {
		t.Fatalf("state = %#v", state)
	}
	if len(state.Peers) != 1 || state.Peers[0].Name != "home" || !state.Peers[0].TokenSet {
		t.Fatalf("peers = %#v", state.Peers)
	}
	if !state.ConfigWritable || state.PendingRestart {
		t.Fatalf("writable=%v pending=%v", state.ConfigWritable, state.PendingRestart)
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") {
		t.Fatalf("state leaked a token: %s", encoded)
	}
}

func TestUpdateCascadeConfigWritesScalarsAndPeers(t *testing.T) {
	store, path := newCascadeStoreFixture(t, cascadeStoreDocument)
	enabled := true
	sharedToken := "shared-secret-token-0123456789"
	maxHops := 3
	maxTTL := "12h"
	allowInsecure := true
	peers := []CascadePeerInput{
		{Name: "home", URL: "http://127.0.0.1:23333"},
		{Name: "lab", URL: "https://cache.lab.example/", Token: stringPointer("peer-secret-token-0123456789"), ForwardCredentials: true},
	}
	result, err := store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{
		Enabled:           &enabled,
		SharedToken:       &sharedToken,
		MaxHops:           &maxHops,
		MaxTTL:            &maxTTL,
		AllowInsecureHTTP: &allowInsecure,
		Peers:             &peers,
	})
	if err != nil {
		t.Fatalf("UpdateCascadeConfig: %v", err)
	}
	for _, want := range []string{
		"cascade.enabled", "cascade.token", "cascade.max_hops", "cascade.max_ttl",
		"cascade.allow_insecure_http", "cascade.peers",
	} {
		if !containsString(result.Changed, want) {
			t.Fatalf("changed = %v, missing %s", result.Changed, want)
		}
	}
	if !result.State.PendingRestart || !result.State.TokenSet || len(result.State.Peers) != 2 {
		t.Fatalf("state = %#v", result.State)
	}
	if result.State.Peers[0].TokenSet || !result.State.Peers[1].TokenSet || !result.State.Peers[1].ForwardCredentials {
		t.Fatalf("peers = %#v", result.State.Peers)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	for _, want := range []string{
		`enabled = true`,
		`token = "shared-secret-token-0123456789"`,
		`max_hops = 3`,
		`max_ttl = "12h"`,
		`allow_insecure_http = true`,
		`name = "lab"`,
		`url = "https://cache.lab.example"`,
		`token = "peer-secret-token-0123456789"`,
		`forward_credentials = true`,
		`# operator comment stays put`,
		`[license]`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("written config missing %q:\n%s", want, text)
		}
	}
	if strings.Count(text, `[[cascade.peers]]`) != 2 {
		t.Fatalf("peer blocks = %d:\n%s", strings.Count(text, "[[cascade.peers]]"), text)
	}

	// The newly written file matches what a fresh read returns.
	state, err := store.CascadeConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !state.PendingRestart || state.MaxHops != 3 || state.MaxTTL != "12h" || !state.AllowInsecureHTTP {
		t.Fatalf("re-read state = %#v", state)
	}
}

func TestUpdateCascadeConfigClearsTokensAndPeers(t *testing.T) {
	document := strings.Replace(cascadeStoreDocument,
		"enabled = false",
		"enabled = true\ntoken = \"shared-secret-token-0123456789\"", 1)
	store, path := newCascadeStoreFixture(t, document)

	// Disable and clear both the shared token and the entire peer list.
	disabled := false
	empty := ""
	noPeers := []CascadePeerInput{}
	result, err := store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{
		Enabled:     &disabled,
		SharedToken: &empty,
		Peers:       &noPeers,
	})
	if err != nil {
		t.Fatalf("UpdateCascadeConfig: %v", err)
	}
	if result.State.TokenSet || len(result.State.Peers) != 0 || len(result.Changed) == 0 {
		t.Fatalf("state = %#v", result.State)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	if strings.Contains(text, "[[cascade.peers]]") || strings.Contains(text, "shared-secret-token") {
		t.Fatalf("tokens or peers survived removal:\n%s", text)
	}
	if !strings.Contains(text, "enabled = false") || !strings.Contains(text, "[license]") {
		t.Fatalf("unrelated content was lost:\n%s", text)
	}
}

func TestUpdateCascadeConfigPeerTokenInheritanceRoundTrip(t *testing.T) {
	store, path := newCascadeStoreFixture(t, cascadeStoreDocument)
	peers := []CascadePeerInput{
		{Name: "home", URL: "http://127.0.0.1:23333", Token: stringPointer("peer-secret-token-0123456789")},
	}
	if _, err := store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{Peers: &peers}); err != nil {
		t.Fatalf("set override: %v", err)
	}
	// Clearing the override must remove the token line and restore inheritance.
	cleared := []CascadePeerInput{{Name: "home", URL: "http://127.0.0.1:23333", Token: stringPointer("")}}
	result, err := store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{Peers: &cleared})
	if err != nil {
		t.Fatalf("clear override: %v", err)
	}
	if result.State.Peers[0].TokenSet {
		t.Fatalf("override still set: %#v", result.State.Peers)
	}
	written, _ := os.ReadFile(path)
	if strings.Contains(string(written), "peer-secret-token") {
		t.Fatalf("peer token line survived clearing:\n%s", written)
	}
}

func TestUpdateCascadeConfigRejectsInvalidPatchWithoutWriting(t *testing.T) {
	store, path := newCascadeStoreFixture(t, cascadeStoreDocument)
	before, _ := os.ReadFile(path)
	enabled := true
	empty := ""
	_, err := store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{
		Enabled:     &enabled,
		SharedToken: &empty,
	})
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != StoreInvalidSetting {
		t.Fatalf("err = %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("invalid patch rewrote the config file")
	}
}

func TestUpdateCascadeConfigRejectsReadOnlyFile(t *testing.T) {
	store, path := newCascadeStoreFixture(t, cascadeStoreDocument)
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	state, err := store.CascadeConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.ConfigWritable {
		t.Fatal("read-only config reported writable")
	}
	maxHops := 2
	_, err = store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{MaxHops: &maxHops})
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != StoreConfigReadOnly {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateCascadeConfigRejectsRemovingReferencedPeer(t *testing.T) {
	document := cascadeStoreDocument + `
[[npm.upstreams]]
name = "peer-npm"
url = "https://registry.npmjs.org"
priority = 1
via = "home"
`
	store, _ := newCascadeStoreFixture(t, document)
	noPeers := []CascadePeerInput{}
	_, err := store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{Peers: &noPeers})
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != StoreInvalidSetting {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(storeErr.Error(), "unknown cascade peer") {
		t.Fatalf("err = %v", storeErr)
	}
}

func TestUpdateCascadeConfigNoOpDoesNotWrite(t *testing.T) {
	store, path := newCascadeStoreFixture(t, cascadeStoreDocument)
	before, _ := os.ReadFile(path)
	same := false
	result, err := store.UpdateCascadeConfig(context.Background(), CascadeConfigPatch{Enabled: &same})
	if err != nil {
		t.Fatalf("no-op update: %v", err)
	}
	if len(result.Changed) != 0 {
		t.Fatalf("changed = %v", result.Changed)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("no-op update rewrote the file")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
