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

func TestConda_PackageMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/conda/pkgs/main/linux-64/testpkg-1.0.0.tar.bz2", "FAKE_CONDA_PKG", false)
}
