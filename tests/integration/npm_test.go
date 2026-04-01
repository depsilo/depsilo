//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestNpm_PackageMetadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/npm/testpkg")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	// dist.tarball should be rewritten to depsilo
	if strings.Contains(body, mockServer.URL()) {
		t.Error("upstream URL not rewritten")
	}
	if !strings.Contains(body, depsiloURL+"/npm/testpkg/-/testpkg-1.0.0.tgz") {
		t.Error("local tarball URL not found")
	}
}

func TestNpm_TarballDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/npm/testpkg/-/testpkg-1.0.0.tgz")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_NPM_TARBALL" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestNpm_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/npm/testpkg/-/testpkg-1.0.0.tgz")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/npm/testpkg/-/testpkg-1.0.0.tgz")
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
