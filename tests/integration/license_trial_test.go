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
// adminPut / adminDelete (from helper.go) log in once as the test administrator and
// cache the token for the rest of the test run.

import (
	"encoding/json"
	"fmt"
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
	var projectID uint

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

	t.Run("free edition can create its first project and export its SBOM", func(t *testing.T) {
		resp := adminGet(t, depsiloURL+"/api/v1/admin/projects")
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusOK)
		body := readBody(t, resp)
		if !strings.Contains(body, `"project_limit":1`) || !strings.Contains(body, `"can_create":true`) || !strings.Contains(body, `"can_export_sbom":true`) {
			t.Fatalf("unexpected free project capabilities: %s", body)
		}
		created := adminPost(t, depsiloURL+"/api/v1/admin/projects", strings.NewReader(`{"name":"Free integration project"}`))
		defer created.Body.Close()
		assertStatus(t, created, http.StatusCreated)
		var project struct {
			ID uint `json:"id"`
		}
		if err := json.NewDecoder(created.Body).Decode(&project); err != nil || project.ID == 0 {
			t.Fatalf("decode created project: id=%d err=%v", project.ID, err)
		}
		projectID = project.ID
		exported := adminGet(t, fmt.Sprintf("%s/api/v1/admin/projects/%d/sbom", depsiloURL, projectID))
		defer exported.Body.Close()
		assertStatus(t, exported, http.StatusOK)
	})

	t.Run("an additional project requires Pro before activation", func(t *testing.T) {
		resp := adminPost(t, depsiloURL+"/api/v1/admin/projects", strings.NewReader(`{"name":"Second integration project"}`))
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusPaymentRequired)
		if body := readBody(t, resp); !strings.Contains(body, `"code":"PRO_REQUIRED"`) || !strings.Contains(body, `"trial_available":true`) {
			t.Errorf("expected Pro requirement with trial available: %s", body)
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

	t.Run("trial unlocks additional projects", func(t *testing.T) {
		created := adminPost(t, depsiloURL+"/api/v1/admin/projects", strings.NewReader(`{"name":"Second integration project"}`))
		defer created.Body.Close()
		assertStatus(t, created, http.StatusCreated)
		resp := adminGet(t, depsiloURL+"/api/v1/admin/projects")
		defer resp.Body.Close()
		assertStatus(t, resp, http.StatusOK)
		body := readBody(t, resp)
		if !strings.Contains(body, `"project_limit":null`) || !strings.Contains(body, `"can_create":true`) {
			t.Errorf("unexpected trial project capabilities: %s", body)
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
// Validation is local and offline: the current Enterprise-contract model accepts
// any non-empty key. The test remains omitted from explicit short-mode runs.
func TestLicenseKey_SetThenClear(t *testing.T) {
	if testing.Short() {
		t.Skip("license mutation flow omitted in short mode")
	}

	// Set a non-empty contract key. SetKey persists it, activates Pro locally,
	// and returns the masked value.
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
