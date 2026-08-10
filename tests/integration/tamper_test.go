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

	// Poll through the 2s blob TTL boundary. The first stale hit triggers a
	// background refresh; every response must keep serving the first-seen bytes
	// until that refresh records the tamper event.
	deadline := time.Now().Add(7 * time.Second)
	for {
		resp := httpGet(t, url)
		body := readBody(t, resp)
		if body != "ORIGINAL-BYTES" {
			t.Fatalf("client got swapped bytes %q — first-seen not protected", body)
		}

		eventResp := adminGet(t, depsiloURL+"/api/v1/admin/quarantine/events?action=tamper_detected")
		var payload struct {
			Items []struct {
				Package string `json:"package"`
				Action  string `json:"action"`
			} `json:"items"`
		}
		raw, _ := io.ReadAll(eventResp.Body)
		eventResp.Body.Close()
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
		time.Sleep(100 * time.Millisecond)
	}
}
