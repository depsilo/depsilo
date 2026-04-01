//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestConda_RepoData(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/conda/pkgs/main/linux-64/repodata.json")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "testpkg") {
		t.Error("repodata missing testpkg")
	}
}

func TestConda_PackageDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/conda/pkgs/main/linux-64/testpkg-1.0.0.tar.bz2")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_CONDA_PKG" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestConda_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/conda/pkgs/main/linux-64/testpkg-1.0.0.tar.bz2")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/conda/pkgs/main/linux-64/testpkg-1.0.0.tar.bz2")
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
