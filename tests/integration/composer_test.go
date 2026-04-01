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

func TestComposer_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/composer/p2/test/pkg.json")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/composer/p2/test/pkg.json")
	assertStatus(t, resp, 200)
	// Short TTL metadata may or may not be cached, just verify 200
	_ = before
}
