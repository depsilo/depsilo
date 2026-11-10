//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestPyPI_SimpleIndex(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/pypi/simple/testpkg/")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	// URL should be rewritten to depsilo, not upstream
	if strings.Contains(body, "pythonhosted.org") {
		t.Error("external URL not rewritten")
	}
	if !strings.Contains(body, depsiloURL+"/pypi/files/") {
		t.Error("local URL not found in rewritten HTML")
	}
	// Hash should be preserved
	if !strings.Contains(body, "#sha256=abcd1234") {
		t.Error("hash not preserved")
	}
}

func TestPyPI_PackageDownload(t *testing.T) {
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/pypi/files/packages/ab/cd/testpkg-1.0.0.tar.gz")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_PACKAGE_DATA_1234567890" {
		t.Errorf("unexpected body: %s", body)
	}
	// Verify upstream was hit (cache miss)
	if mockServer.RequestCount() <= before {
		t.Error("expected upstream hit on first request")
	}
}

func TestPyPI_CacheHit(t *testing.T) {
	// First request to ensure cached
	httpGet(t, depsiloURL+"/pypi/files/packages/ab/cd/testpkg-1.0.0.tar.gz")

	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/pypi/files/packages/ab/cd/testpkg-1.0.0.tar.gz")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_PACKAGE_DATA_1234567890" {
		t.Errorf("unexpected body on cache hit: %s", body)
	}
	// Verify upstream was NOT hit (cache hit)
	if mockServer.RequestCount() != before {
		t.Error("expected no upstream request on cache hit")
	}
}

// pip and uv read a wheel's PEP 658 core metadata before downloading it. The
// integration config enables the known-malicious dataset, which requires an
// identity for every PyPI artifact, so the metadata file must take its wheel's
// identity rather than be refused as unidentifiable.
func TestPyPI_CoreMetadataServedWhileBlocklistCoversPyPI(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/pypi/files/packages/ab/cd/testpkg-1.0.0-py3-none-any.whl.metadata")
	assertStatus(t, resp, 200)
	if body := readBody(t, resp); body != "FAKE_PACKAGE_DATA_1234567890" {
		t.Errorf("unexpected body: %s", body)
	}
}
