//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The mock OSV endpoint marks npm "malicious-pkg" (every version) as
// malware; the test config's [supply_chain.blocklist] points at it.
// This is the DIRECTION Task 2 acceptance test: a known-malicious
// version is blocked end-to-end with 451 MALICIOUS_BLOCKED.
//
// The startup sync runs on its own goroutine, so the first assertion
// polls until the dataset lands (or times out loudly).

func fetchBlocked(t *testing.T) (*http.Response, string) {
	t.Helper()
	url := npmVersionTarballURL(t, "malicious-pkg", "1.0.0")
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET %s: %v", url, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusUnavailableForLegalReasons {
			return resp, string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("blocklist sync never took effect: last status %d body %s", resp.StatusCode, string(body))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestBlocklist_MaliciousVersionBlockedEndToEnd(t *testing.T) {
	resp, body := fetchBlocked(t)

	var payload struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		Ecosystem string `json:"ecosystem"`
		Package   string `json:"package"`
		Version   string `json:"version"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("451 body is not JSON: %v (%s)", err, body)
	}
	if payload.Code != "MALICIOUS_BLOCKED" {
		t.Errorf("code = %q, want MALICIOUS_BLOCKED (body %s)", payload.Code, body)
	}
	if !strings.Contains(payload.Message, "MAL-2026-0001") {
		t.Errorf("message should cite the advisory id: %s", payload.Message)
	}
	if payload.Package != "malicious-pkg" || payload.Version != "1.0.0" {
		t.Errorf("unexpected payload: %+v", payload)
	}
	_ = resp
}

func TestBlocklist_DisabledAgeGateDoesNotBypassMalware(t *testing.T) {
	// The test config disables the minimum-release-age gate — if the malware
	// gate depended on that switch, this request would be served. Any different
	// version must also block (the advisory covers all versions).
	url := npmVersionTarballURL(t, "malicious-pkg", "9.9.9")
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnavailableForLegalReasons {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 451 (body %s)", resp.StatusCode, body)
	}
}

func TestBlocklist_CleanPackageStillServes(t *testing.T) {
	// The regular npm test package must be unaffected by the blocklist.
	resp, err := http.Get(npmVersionTarballURL(t, "testpkg", "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clean package status = %d, want 200", resp.StatusCode)
	}
}

func TestBlocklist_AdminStatusReportsSync(t *testing.T) {
	resp := adminGet(t, depsiloURL+"/api/v1/admin/blocklist/status")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status endpoint = %d", resp.StatusCode)
	}
	var st struct {
		Enabled       bool             `json:"enabled"`
		Mode          string           `json:"mode"`
		EntryCount    int64            `json:"entry_count"`
		LastSuccessAt *time.Time       `json:"last_success_at"`
		PerEcosystem  map[string]int64 `json:"per_ecosystem"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || st.LastSuccessAt == nil {
		t.Errorf("unexpected sync state: %+v", st)
	}
	if st.Mode != "block" {
		t.Errorf("mode = %q, want %q", st.Mode, "block")
	}
	// The mock archive carries one npm and one Go advisory; importers
	// keep only their own ecosystem's sections.
	if st.PerEcosystem["npm"] != 1 || st.PerEcosystem["go"] != 1 || st.EntryCount != 2 {
		t.Errorf("entry counts: %+v", st)
	}
}

func TestBlocklist_OverrideLifecycle(t *testing.T) {
	// Ensure the dataset is live first.
	fetchBlocked(t)

	// Create a 24h override for the exact version…
	body := strings.NewReader(`{"ecosystem":"npm","package":"malicious-pkg","version":"1.0.0","reason":"integration test override"}`)
	resp := adminPost(t, depsiloURL+"/api/v1/admin/blocklist/overrides", body)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create override = %d (%s)", resp.StatusCode, raw)
	}
	var ov struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(raw, &ov); err != nil {
		t.Fatal(err)
	}

	// …the overridden version now serves (thresholds are zero, so the
	// age quarantine won't block it either)…
	servedResp, err := http.Get(npmVersionTarballURL(t, "malicious-pkg", "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	servedResp.Body.Close()
	if servedResp.StatusCode != http.StatusOK {
		t.Fatalf("override not honored: status = %d, want 200", servedResp.StatusCode)
	}

	// …other versions stay blocked…
	other, err := http.Get(npmVersionTarballURL(t, "malicious-pkg", "2.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	other.Body.Close()
	if other.StatusCode != http.StatusUnavailableForLegalReasons {
		t.Errorf("other version = %d, want 451", other.StatusCode)
	}

	// …and revoking restores the block. adminDelete has no body
	// support, so build the request by hand with the cached token.
	req, _ := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("%s/api/v1/admin/blocklist/overrides/%d", depsiloURL, ov.ID),
		strings.NewReader(`{"reason":"test cleanup"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+loginAdmin(t))
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("revoke = %d", delResp.StatusCode)
	}
	reblocked, err := http.Get(npmVersionTarballURL(t, "malicious-pkg", "1.0.0"))
	if err != nil {
		t.Fatal(err)
	}
	reblocked.Body.Close()
	if reblocked.StatusCode != http.StatusUnavailableForLegalReasons {
		t.Errorf("after revoke = %d, want 451", reblocked.StatusCode)
	}
}

func TestBlocklist_GoModuleBlocked(t *testing.T) {
	// Go has threshold 0 (no age quarantine by design) — the malware
	// gate must fire anyway, and the GOPROXY "!e" escaping must decode
	// before matching ("!evil" would otherwise never hit the row).
	fetchBlocked(t) // dataset live
	resp, err := http.Get(depsiloURL + "/go/github.com/evil/module/@v/v1.0.0.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnavailableForLegalReasons {
		t.Fatalf("status = %d, want 451 (body %s)", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "MALICIOUS_BLOCKED") {
		t.Errorf("body should carry MALICIOUS_BLOCKED: %s", body)
	}
}

func TestBlocklist_GoCleanModuleServes(t *testing.T) {
	resp, err := http.Get(depsiloURL + "/go/github.com/test/mod/@v/v1.0.0.zip")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clean module = %d, want 200", resp.StatusCode)
	}
}

func TestBlocklist_OverrideRejectsUnknownEcosystem(t *testing.T) {
	// OSV spellings must be rejected (or canonicalized) — a 201 for
	// "PyPI" that never matches would be a silently dead exemption.
	body := strings.NewReader(`{"ecosystem":"crates.io","package":"x","reason":"typo test"}`)
	resp := adminPost(t, depsiloURL+"/api/v1/admin/blocklist/overrides", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("unknown ecosystem = %d, want 400 (%s)", resp.StatusCode, raw)
	}
}
