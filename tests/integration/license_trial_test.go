//go:build integration

package integration

// license_trial_test.go — integration tests for the full trial lifecycle,
// paywall behaviour, and license-key set/clear flow.
//
// State ordering:
//   TestLicenseTrial_FullLifecycle runs the trial flow first (file sort
//   order ensures it precedes TestLicenseKey_* because 'l' < 'l' —
//   subtests within one function guarantee intra-test ordering).
//
// All admin endpoints require JWT auth. The helpers adminGet / adminPost /
// adminPut / adminDelete (from helper.go) log in once as admin/admin and
// cache the token for the rest of the test run.

import (
	"net/http"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// Test 1: Full trial lifecycle
// -----------------------------------------------------------------------------

// TestLicenseTrial_FullLifecycle exercises the trial state machine end-to-end
// using t.Run subtests that must run in the listed order.
func TestLicenseTrial_FullLifecycle(t *testing.T) {
	// State precondition: this must be the first test that touches
	// /admin/license/trial, because the DB is shared for the whole run.

	t.Run("status starts as free with trial available", func(t *testing.T) {
		resp := adminGet(t, depsiloURL+"/api/v1/admin/license/status")
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusOK)
		body := readBody(t, resp)
		if !strings.Contains(body, `"source":"none"`) {
			t.Errorf("expected source=none, got body: %s", body)
		}
		if !strings.Contains(body, `"trial_available":true`) {
			t.Errorf("expected trial_available=true, got body: %s", body)
		}
		if !strings.Contains(body, `"trial_used":false`) {
			t.Errorf("expected trial_used=false, got body: %s", body)
		}
		if !strings.Contains(body, `"is_pro":false`) {
			t.Errorf("expected is_pro=false on fresh install, got body: %s", body)
		}
	})

	t.Run("paywall returns 402 with trial_available=true before activation", func(t *testing.T) {
		// /admin/projects is behind entitlement.RequirePro (router.go line ~183).
		resp := adminGet(t, depsiloURL+"/api/v1/admin/projects")
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusPaymentRequired)
		body := readBody(t, resp)
		if !strings.Contains(body, `"code":"PRO_REQUIRED"`) {
			t.Errorf("expected code=PRO_REQUIRED, got body: %s", body)
		}
		if !strings.Contains(body, `"trial_available":true`) {
			t.Errorf("expected trial_available=true in paywall body, got: %s", body)
		}
	})

	t.Run("activating trial returns 200 with source=trial and is_pro=true", func(t *testing.T) {
		resp := adminPost(t, depsiloURL+"/api/v1/admin/license/trial/activate", nil)
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusOK)
		body := readBody(t, resp)
		if !strings.Contains(body, `"source":"trial"`) {
			t.Errorf("expected source=trial, got body: %s", body)
		}
		if !strings.Contains(body, `"is_pro":true`) {
			t.Errorf("expected is_pro=true after activation, got body: %s", body)
		}
		if !strings.Contains(body, `"trial_used":true`) {
			t.Errorf("expected trial_used=true after activation, got body: %s", body)
		}
	})

	t.Run("status endpoint confirms trial is active", func(t *testing.T) {
		resp := adminGet(t, depsiloURL+"/api/v1/admin/license/status")
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusOK)
		body := readBody(t, resp)
		if !strings.Contains(body, `"source":"trial"`) {
			t.Errorf("expected source=trial in status, got body: %s", body)
		}
		if !strings.Contains(body, `"is_pro":true`) {
			t.Errorf("expected is_pro=true in status, got body: %s", body)
		}
		if !strings.Contains(body, `"trial_available":false`) {
			t.Errorf("expected trial_available=false (trial consumed), got body: %s", body)
		}
	})

	t.Run("pro endpoint no longer returns 402 after trial activation", func(t *testing.T) {
		resp := adminGet(t, depsiloURL+"/api/v1/admin/projects")
		defer resp.Body.Close()
		// After activation IsPro == true, so RequirePro must pass.
		// Response can be 200 (empty list) or any non-402.
		if resp.StatusCode == http.StatusPaymentRequired {
			body := readBody(t, resp)
			t.Errorf("expected non-402 after trial activation, got 402. Body: %s", body)
		}
	})

	t.Run("re-activating trial returns 409 TRIAL_ALREADY_USED", func(t *testing.T) {
		resp := adminPost(t, depsiloURL+"/api/v1/admin/license/trial/activate", nil)
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusConflict)
		body := readBody(t, resp)
		if !strings.Contains(body, "TRIAL_ALREADY_USED") {
			t.Errorf("expected TRIAL_ALREADY_USED in body, got: %s", body)
		}
	})

	// Simulating trial expiry to test trial_used=true paywall is not feasible
	// in an integration test without time-travel. That scenario is covered by
	// unit tests in internal/entitlement/checker_test.go.
}

// -----------------------------------------------------------------------------
// Test 2: Key set/clear flow
// -----------------------------------------------------------------------------

// TestLicenseKey_SetThenClear verifies that PUT /license/key persists the key
// (masked in the response) and DELETE /license/key clears it.
//
// WARNING: PUT /license/key calls license.Manager.SetKey() synchronously, which
// contacts https://api.lemonsqueezy.com/v1/licenses/validate with a 10 s
// timeout and up to 2 retries (~25 s worst case). The test is skipped when
// -short is given, but runs by default in `make test-integration`.
func TestLicenseKey_SetThenClear(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: PUT /license/key triggers Lemon Squeezy network call (~25 s worst-case timeout)")
	}

	// Set a fake key. The remote validation will fail (invalid key), but
	// SetKey persists the key and returns 200 regardless — the frontend reads
	// status.is_pro to determine whether validation succeeded.
	body := strings.NewReader(`{"key":"depsilo-integration-test-key-xxxx"}`)
	resp := adminPut(t, depsiloURL+"/api/v1/admin/license/key", body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)
	rb := readBody(t, resp)

	// MaskKey("depsilo-integration-test-key-xxxx") finds first '-' at index 7,
	// returns "depsilo-***"  (license.go MaskKey implementation).
	if !strings.Contains(rb, `"license_key_masked":"depsilo-***"`) {
		t.Errorf("expected license_key_masked=depsilo-***, got: %s", rb)
	}

	// Clear the key.
	resp2 := adminDelete(t, depsiloURL+"/api/v1/admin/license/key")
	defer resp2.Body.Close()
	assertStatus(t, resp2, http.StatusOK)
	rb2 := readBody(t, resp2)
	// After clearing, license_key_masked must not contain the old prefix.
	if strings.Contains(rb2, `"license_key_masked":"depsilo-`) {
		t.Errorf("expected license_key_masked to be absent/empty after clear, got: %s", rb2)
	}
}

// TestLicenseKey_RejectEmpty verifies that an empty key string is rejected.
func TestLicenseKey_RejectEmpty(t *testing.T) {
	body := strings.NewReader(`{"key":""}`)
	resp := adminPut(t, depsiloURL+"/api/v1/admin/license/key", body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusBadRequest)
}

// TestLicenseKey_RejectMissing verifies that a body lacking the "key" field is rejected.
func TestLicenseKey_RejectMissing(t *testing.T) {
	body := strings.NewReader(`{}`)
	resp := adminPut(t, depsiloURL+"/api/v1/admin/license/key", body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusBadRequest)
}
