//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCascadeConfigIsManagedThroughAdminAPI proves the write path end to end:
// the Admin API patches config.toml, never echoes secrets, and reports the
// restart requirement while the running process keeps its old values.
func TestCascadeConfigIsManagedThroughAdminAPI(t *testing.T) {
	const sharedSecret = "integration-cascade-token-0123456789"
	body := `{
		"max_hops": 3,
		"max_ttl": "12h",
		"peers": [
			{"name": "home", "url": "http://127.0.0.1:23333", "forward_credentials": false},
			{"name": "managed-example", "url": "https://cache.example", "token": "peer-managed-token-0123456789", "forward_credentials": true}
		]
	}`
	response := adminPut(t, depsiloURL+"/api/v1/admin/cascade/config", strings.NewReader(body))
	defer response.Body.Close()
	assertStatus(t, response, http.StatusOK)
	raw := readBody(t, response)
	if strings.Contains(raw, sharedSecret) || strings.Contains(raw, "peer-managed-token") {
		t.Fatalf("Admin API leaked a cascade token: %s", raw)
	}
	var result struct {
		State struct {
			MaxHops        int    `json:"max_hops"`
			MaxTTL         string `json:"max_ttl"`
			TokenSet       bool   `json:"token_set"`
			ConfigWritable bool   `json:"config_writable"`
			PendingRestart bool   `json:"pending_restart"`
			Peers          []struct {
				Name     string `json:"name"`
				TokenSet bool   `json:"token_set"`
			} `json:"peers"`
		} `json:"state"`
		RestartRequired bool `json:"restart_required"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, raw)
	}
	if result.State.MaxHops != 3 || result.State.MaxTTL != "12h" || !result.State.ConfigWritable {
		t.Fatalf("state = %+v", result.State)
	}
	if !result.State.PendingRestart || !result.RestartRequired {
		t.Fatalf("restart flags = pending:%v required:%v", result.State.PendingRestart, result.RestartRequired)
	}
	if !result.State.TokenSet || len(result.State.Peers) != 2 {
		t.Fatalf("masked secrets = %+v", result.State)
	}
	if result.State.Peers[0].TokenSet || !result.State.Peers[1].TokenSet {
		t.Fatalf("peer secret masking = %+v", result.State.Peers)
	}

	written, err := os.ReadFile(filepath.Join(testDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(written)
	for _, want := range []string{"max_hops = 3", `max_ttl = "12h"`, `name = "managed-example"`,
		`token = "peer-managed-token-0123456789"`, "forward_credentials = true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("config.toml missing %q:\n%s", want, text)
		}
	}

	// A second read returns the masked state from the file, not a stale copy.
	getResponse := adminGet(t, depsiloURL+"/api/v1/admin/cascade/config")
	defer getResponse.Body.Close()
	assertStatus(t, getResponse, http.StatusOK)
	getBody := readBody(t, getResponse)
	if strings.Contains(getBody, "peer-managed-token") || !strings.Contains(getBody, `"name":"managed-example"`) {
		t.Fatalf("GET body = %s", getBody)
	}
}
