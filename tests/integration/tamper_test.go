//go:build integration

package integration

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// DIRECTION T1 acceptance: an immutable artifact whose upstream bytes
// change under the same version raises a tamper_detected event AND the
// client keeps getting the first-seen bytes (storage not overwritten).
func TestTamper_UpstreamSwapDetectedAndFirstSeenKept(t *testing.T) {
	url := depsiloURL + "/npm/tamperpkg/-/tamperpkg-1.0.0.tgz"

	// First fetch: caches ORIGINAL-BYTES and records the baseline.
	first := httpGet(t, url)
	firstBody := readBody(t, first)
	if firstBody != "ORIGINAL-BYTES" {
		t.Fatalf("first fetch body = %q", firstBody)
	}

	// Simulate an upstream silently swapping the artifact bytes.
	mockServer.SetTamperBody("EVIL-SWAPPED-BYTES")

	// Let the 2s blob TTL lapse, then hit it repeatedly: the stale hit
	// serves cache immediately and triggers a background refresh, which
	// verifies-not-overwrites and raises the event.
	time.Sleep(3 * time.Second)
	for i := 0; i < 3; i++ {
		resp := httpGet(t, url)
		body := readBody(t, resp)
		if body != "ORIGINAL-BYTES" {
			t.Fatalf("client got swapped bytes %q — first-seen not protected", body)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// A tamper_detected event must have been recorded.
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp := adminGet(t, depsiloURL+"/api/v1/admin/quarantine/events?action=tamper_detected")
		var payload struct {
			Items []struct {
				Package string `json:"package"`
				Action  string `json:"action"`
			} `json:"items"`
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		_ = json.Unmarshal(raw, &payload)
		if len(payload.Items) > 0 && payload.Items[0].Action == "tamper_detected" {
			if !strings.Contains(payload.Items[0].Package, "tamperpkg") {
				t.Errorf("event package = %q", payload.Items[0].Package)
			}
			return // success
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tamper_detected event; last response: %s", raw)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
