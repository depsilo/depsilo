//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocker_VersionCheck(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/v2/")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Errorf("response is not valid JSON: %v", err)
	}
}

func TestDocker_ManifestFetch(t *testing.T) {
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/manifests/latest")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body == "" {
		t.Error("empty manifest response")
	}
	if !strings.Contains(body, "schemaVersion") {
		t.Error("manifest response missing schemaVersion")
	}
	if mockServer.RequestCount() <= before {
		t.Error("expected upstream hit on first request")
	}
}

func TestDocker_ManifestCacheHit(t *testing.T) {
	// Ensure cached from previous test
	httpGet(t, depsiloURL+"/v2/library/testimg/manifests/latest")

	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/manifests/latest")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "schemaVersion") {
		t.Error("manifest response missing schemaVersion on cache hit")
	}
	if mockServer.RequestCount() != before {
		t.Error("expected no upstream request on cache hit")
	}
}

func TestDocker_DigestManifest(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/manifests/sha256:fakedigest")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "schemaVersion") {
		t.Error("digest manifest response missing schemaVersion")
	}
}

func TestDocker_BlobFetch(t *testing.T) {
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/blobs/sha256:fakelayer")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_DOCKER_BLOB_DATA" {
		t.Errorf("unexpected blob body: %s", body)
	}
	if mockServer.RequestCount() <= before {
		t.Error("expected upstream hit on first blob request")
	}
}

func TestDocker_BlobCacheHit(t *testing.T) {
	// Ensure cached
	httpGet(t, depsiloURL+"/v2/library/testimg/blobs/sha256:fakelayer")

	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/blobs/sha256:fakelayer")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_DOCKER_BLOB_DATA" {
		t.Errorf("unexpected blob body on cache hit: %s", body)
	}
	if mockServer.RequestCount() != before {
		t.Error("expected no upstream request on blob cache hit")
	}
}

func TestDocker_TagList(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/tags/list")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	var result struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		t.Fatalf("failed to parse tag list: %v", err)
	}
	if result.Name != "library/testimg" {
		t.Errorf("name = %q, want library/testimg", result.Name)
	}
	if len(result.Tags) != 2 {
		t.Errorf("tags count = %d, want 2", len(result.Tags))
	}
}
