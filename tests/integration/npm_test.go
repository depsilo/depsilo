//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func npmVersionTarballURL(t *testing.T, packageName, version string) string {
	t.Helper()
	response := httpGet(t, depsiloURL+"/npm/"+packageName)
	assertStatus(t, response, http.StatusOK)
	defer response.Body.Close()
	var document struct {
		Versions map[string]struct {
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		} `json:"versions"`
	}
	if err := json.NewDecoder(response.Body).Decode(&document); err != nil {
		t.Fatalf("decode npm metadata for %s: %v", packageName, err)
	}
	tarball := document.Versions[version].Dist.Tarball
	if tarball == "" {
		t.Fatalf("npm metadata for %s has no tarball for %s", packageName, version)
	}
	return tarball
}

func TestNpm_PackageMetadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/npm/testpkg")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	// dist.tarball should be rewritten to depsilo
	if strings.Contains(body, mockServer.URL()) {
		t.Error("upstream URL not rewritten")
	}
	if !strings.Contains(body, depsiloURL+"/npm/testpkg/-/__depsilo_tarball_v1/") ||
		!strings.Contains(body, "/testpkg-1.0.0.tgz") {
		t.Errorf("authenticated local tarball URL not found: %s", body)
	}
}

func TestNpm_AcceptRepresentationsAreIsolated(t *testing.T) {
	requestMetadata := func(accept string) *http.Response {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, depsiloURL+"/npm/accept-fixture", nil)
		if err != nil {
			t.Fatal(err)
		}
		if accept != "" {
			request.Header.Set("Accept", accept)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		assertStatus(t, response, http.StatusOK)
		return response
	}

	full := requestMetadata("")
	if body := readBody(t, full); !strings.Contains(body, `"variant":"full"`) {
		t.Fatalf("full npm metadata = %s", body)
	}
	installAccept := "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*"
	install := requestMetadata(installAccept)
	if body := readBody(t, install); !strings.Contains(body, `"variant":"install"`) {
		t.Fatalf("install npm metadata = %s", body)
	}
	if got := install.Header.Get("Vary"); got != "Accept" {
		t.Fatalf("Vary = %q, want Accept", got)
	}

	before := mockServer.RequestCount()
	readBody(t, requestMetadata(""))
	readBody(t, requestMetadata(installAccept))
	if got := mockServer.RequestCount(); got != before {
		t.Fatalf("cached npm representations contacted upstream: before=%d after=%d", before, got)
	}
}

func TestNpm_TarballDownload(t *testing.T) {
	resp := httpGet(t, npmVersionTarballURL(t, "testpkg", "1.0.0"))
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_NPM_TARBALL" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestNpm_LegacyDirectTarballRejected(t *testing.T) {
	const upstreamPath = "/testpkg/-/testpkg-1.0.0.tgz"
	before := mockRequestCountForPath(upstreamPath)
	resp := httpGet(t, depsiloURL+"/npm"+upstreamPath)
	assertStatus(t, resp, http.StatusNotFound)
	_ = readBody(t, resp)
	if got := mockRequestCountForPath(upstreamPath); got != before {
		t.Fatalf("unsigned npm tarball contacted upstream: before=%d after=%d", before, got)
	}
}

func TestNpm_CacheHit(t *testing.T) {
	tarballURL := npmVersionTarballURL(t, "testpkg", "1.0.0")
	httpGet(t, tarballURL)
	before := mockServer.RequestCount()
	resp := httpGet(t, tarballURL)
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
