//go:build integration

package integration

import (
	"net/http"
	"strings"
	"testing"
)

func TestHF_ModelMetadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/api/models/bert-base-uncased")
	defer resp.Body.Close()
	assertStatus(t, resp, 200)
	assertBodyContains(t, resp, `"modelId":"bert-base-uncased"`)
}

func TestHF_ModelTreeListing(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/api/models/bert-base-uncased/tree/main")
	defer resp.Body.Close()
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"path":"config.json"`) {
		t.Errorf("body does not contain config.json entry. Got: %s", body)
	}
	if !strings.Contains(body, `"path":"pytorch_model.bin"`) {
		t.Errorf("body does not contain pytorch_model.bin entry. Got: %s", body)
	}
}

func TestHF_DatasetMetadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/api/datasets/squad")
	defer resp.Body.Close()
	assertStatus(t, resp, 200)
	assertBodyContains(t, resp, `"id":"squad"`)
}

func TestHF_ResolveSmallFile(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/bert-base-uncased/resolve/main/config.json")
	defer resp.Body.Close()
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"hidden":false`) {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestHF_ResolveLFS_StreamedFromCDN(t *testing.T) {
	const commit = "abc1234abc1234abc1234abc1234abc1234abc12"
	movingBefore := countMockPath("/bert-base-uncased/resolve/main/pytorch_model.bin")
	canonicalBefore := countMockPath("/bert-base-uncased/resolve/" + commit + "/pytorch_model.bin")
	cdnBefore := countMockPath("/cdn-lfs/pytorch_model.bin")

	resp := httpGet(t, depsiloURL+"/huggingface/bert-base-uncased/resolve/main/pytorch_model.bin")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	resp.Body.Close()
	if body != "FAKE_WEIGHT" {
		t.Errorf("body = %q, want FAKE_WEIGHT (server-side redirect should have fetched the CDN bytes)", body)
	}
	if got := resp.Header.Get("X-Linked-Etag"); got != "deadbeefcafe" {
		t.Errorf("X-Linked-Etag = %q, want deadbeefcafe (header from upstream must pass through)", got)
	}

	cached := httpGet(t, depsiloURL+"/huggingface/bert-base-uncased/resolve/main/pytorch_model.bin")
	assertStatus(t, cached, 200)
	cachedBody := readBody(t, cached)
	cached.Body.Close()
	if cachedBody != "FAKE_WEIGHT" {
		t.Errorf("cached body = %q, want FAKE_WEIGHT", cachedBody)
	}
	if got := cached.Header.Get("X-Linked-Etag"); got != "deadbeefcafe" {
		t.Errorf("cached X-Linked-Etag = %q, want deadbeefcafe", got)
	}
	if got := countMockPath("/bert-base-uncased/resolve/"+commit+"/pytorch_model.bin") - canonicalBefore; got != 1 {
		t.Errorf("canonical origin requests = %d, want 1 across miss + hit", got)
	}
	if got := countMockPath("/bert-base-uncased/resolve/main/pytorch_model.bin") - movingBefore; got < 0 || got > 1 {
		t.Errorf("moving-ref metadata requests = %d, want at most one HEAD", got)
	}
	if got := countMockPath("/cdn-lfs/pytorch_model.bin") - cdnBefore; got != 1 {
		t.Errorf("CDN requests = %d, want 1 across miss + hit", got)
	}
}

func TestHF_HEAD_HeadersOnly(t *testing.T) {
	req, err := http.NewRequest(http.MethodHead,
		depsiloURL+"/huggingface/bert-base-uncased/resolve/main/pytorch_model.bin", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readBody(t, resp)
	if len(body) > 0 {
		t.Errorf("HEAD body length = %d, want 0", len(body))
	}
	if got := resp.Header.Get("X-Linked-Etag"); got != "deadbeefcafe" {
		t.Errorf("X-Linked-Etag = %q, want deadbeefcafe", got)
	}
}

func TestHF_UnknownPath_404(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/huggingface/no/such/route/at/all")
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("status = %d, want 404 for unrecognized path", resp.StatusCode)
	}
}

func countMockPath(path string) int {
	count := 0
	for _, request := range mockServer.Requests() {
		if request.Path == path {
			count++
		}
	}
	return count
}
