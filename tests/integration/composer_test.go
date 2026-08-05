//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestComposer_PackagesJson(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/composer/packages.json")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	// metadata-url should be rewritten to depsilo
	if !strings.Contains(body, depsiloURL) {
		t.Error("metadata-url not rewritten")
	}
}

func TestComposer_PackageMetadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/composer/p2/test/pkg.json")
	assertStatus(t, resp, 200)
}

func TestComposer_MirrorsInjected(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/composer/packages.json")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "/composer/dist/%package%/%version%/%reference%.%type%") {
		t.Error("dist mirror template not injected into packages.json")
	}
}

func TestComposer_DistDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/composer/dist/test/pkg/1.0.0.0/abc.zip")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_COMPOSER_DIST" {
		t.Errorf("unexpected dist body: %q", body)
	}
}

func TestComposer_DistCacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/composer/dist/test/pkg/1.0.0.0/abc.zip")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/composer/dist/test/pkg/1.0.0.0/abc.zip")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_COMPOSER_DIST" {
		t.Errorf("unexpected dist body on cache hit: %q", body)
	}
	// Both the dist blob (long TTL) and the p2 metadata it resolves
	// through (fresh within its short TTL) must come from cache.
	if got := mockServer.RequestCount(); got != before {
		t.Errorf("expected dist cache hit, upstream saw %d new request(s)", got-before)
	}
}

func TestComposer_DistUnknownVersion(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/composer/dist/test/pkg/9.9.9.9/nope.zip")
	assertStatus(t, resp, 404)
}
