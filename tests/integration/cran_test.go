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

func TestCRAN_PackageMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/cran/src/contrib/testpkg_1.0.0.tar.gz", "FAKE_CRAN_PKG", false)
}
