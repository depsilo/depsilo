//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestCRAN_Packages(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/cran/src/contrib/PACKAGES")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "Package: testpkg") {
		t.Error("PACKAGES missing testpkg")
	}
}

func TestCRAN_PackageDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/cran/src/contrib/testpkg_1.0.0.tar.gz")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_CRAN_PKG" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestCRAN_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/cran/src/contrib/testpkg_1.0.0.tar.gz")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/cran/src/contrib/testpkg_1.0.0.tar.gz")
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
