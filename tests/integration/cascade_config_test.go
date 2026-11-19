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
// the running server patches its own config.toml, never echoes secrets, and
// puts the section into effect without a restart. Masking each field and the
// written document's shape are tested through the handler in
// internal/api/admin/cascade_config_test.go.
func TestCascadeConfigIsManagedThroughAdminAPI(t *testing.T) {
	body := `{
		"max_hops": 3,
		"max_ttl": "12h",
		"peers": [
			{"name": "home", "url": "http://127.0.0.1:9", "forward_credentials": false},
			{"name": "managed-example", "url": "https://cache.example", "token": "peer-managed-token-0123456789", "forward_credentials": true}
		]
	}`
	response := adminPut(t, depsiloURL+"/api/v1/admin/cascade/config", strings.NewReader(body))
	defer response.Body.Close()
	assertStatus(t, response, http.StatusOK)
	raw := readBody(t, response)
	if strings.Contains(raw, "peer-managed-token") {
		t.Fatalf("Admin API leaked a cascade token: %s", raw)
	}
	var result struct {
		State struct {
			MaxHops        int    `json:"max_hops"`
			MaxTTL         string `json:"max_ttl"`
			ConfigWritable bool   `json:"config_writable"`
			PendingRestart bool   `json:"pending_restart"`
		} `json:"state"`
		RestartRequired bool `json:"restart_required"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, raw)
	}
	if result.State.MaxHops != 3 || result.State.MaxTTL != "12h" || !result.State.ConfigWritable {
		t.Fatalf("state = %+v", result.State)
	}
	if result.State.PendingRestart || result.RestartRequired {
		t.Fatalf("restart flags = pending:%v required:%v", result.State.PendingRestart, result.RestartRequired)
	}

	written, err := os.ReadFile(filepath.Join(testDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if text := string(written); !strings.Contains(text, "max_hops = 3") || !strings.Contains(text, `token = "peer-managed-token-0123456789"`) {
		t.Fatalf("the server's config.toml was not patched:\n%s", text)
	}

	// The running process uses the saved values at once.
	infoResponse := adminGet(t, depsiloURL+"/api/v1/admin/cascade")
	defer infoResponse.Body.Close()
	assertStatus(t, infoResponse, http.StatusOK)
	infoBody := readBody(t, infoResponse)
	if strings.Contains(infoBody, "peer-managed-token") {
		t.Fatalf("running cascade topology leaked a token: %s", infoBody)
	}
	var info struct {
		MaxHops int `json:"max_hops"`
		Peers   []struct {
			Name string `json:"name"`
		} `json:"peers"`
	}
	if err := json.Unmarshal([]byte(infoBody), &info); err != nil {
		t.Fatal(err)
	}
	if info.MaxHops != 3 || len(info.Peers) != 2 || info.Peers[1].Name != "managed-example" {
		t.Fatalf("running cascade = %+v, want the saved values", info)
	}
}
