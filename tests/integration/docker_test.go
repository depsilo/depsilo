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

func TestDocker_ManifestMissThenHit(t *testing.T) {
	for attempt := 1; attempt <= 2; attempt++ {
		before := mockServer.RequestCount()
		resp := httpGet(t, depsiloURL+"/v2/library/testimg/manifests/latest")
		assertStatus(t, resp, 200)
		if body := readBody(t, resp); !strings.Contains(body, "schemaVersion") {
			t.Errorf("request %d: manifest response missing schemaVersion: %q", attempt, body)
		}
		upstreamRequests := mockServer.RequestCount() - before
		if attempt == 1 && upstreamRequests == 0 {
			t.Error("expected upstream hit on first request")
		}
		if attempt == 2 && upstreamRequests != 0 {
			t.Error("expected no upstream request on cache hit")
		}
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

func TestDocker_BlobMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/v2/library/testimg/blobs/sha256:fakelayer", "FAKE_DOCKER_BLOB_DATA", true)
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

func TestDocker_TagListQueryIsForwardedAndCachedPerPage(t *testing.T) {
	const first = "n=1&last=integration-alpha"
	const second = "n=1&last=integration-beta"
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/v2/library/testimg/tags/list?"+first)
	assertStatus(t, resp, 200)
	readBody(t, resp)
	resp = httpGet(t, depsiloURL+"/v2/library/testimg/tags/list?"+second)
	assertStatus(t, resp, 200)
	readBody(t, resp)

	seen := map[string]bool{}
	for _, request := range mockServer.Requests()[before:] {
		if request.Path == "/v2/library/testimg/tags/list" {
			seen[request.RawQuery] = true
		}
	}
	if !seen[first] || !seen[second] {
		t.Fatalf("Docker tag-list queries = %#v, want both pages", seen)
	}

	before = mockServer.RequestCount()
	resp = httpGet(t, depsiloURL+"/v2/library/testimg/tags/list?"+first)
	assertStatus(t, resp, 200)
	readBody(t, resp)
	if got := mockServer.RequestCount(); got != before {
		t.Fatalf("cached Docker tag page contacted upstream: before=%d after=%d", before, got)
	}
}
