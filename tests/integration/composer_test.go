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

// On the second fetch both the dist blob (long TTL) and the p2 metadata it
// resolves through (fresh within its short TTL) must come from cache.
func TestComposer_DistMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/composer/dist/test/pkg/1.0.0.0/abc.zip", "FAKE_COMPOSER_DIST", false)
}

func TestComposer_DistUnknownVersion(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/composer/dist/test/pkg/9.9.9.9/nope.zip")
	assertStatus(t, resp, 404)
}
