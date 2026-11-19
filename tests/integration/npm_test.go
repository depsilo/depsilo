//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestNpm_LegacyDirectTarballRejected(t *testing.T) {
	// Use a path no other test downloads. Reusing testpkg would race the
	// parent's background tamper re-verification of its 2s-TTL blob entry,
	// which legitimately fetches the same upstream path.
	const upstreamPath = "/legacy-reject-fixture/-/legacy-reject-fixture-1.0.0.tgz"
	before := mockRequestCountForPath(upstreamPath)
	resp := httpGet(t, depsiloURL+"/npm"+upstreamPath)
	assertStatus(t, resp, http.StatusNotFound)
	// A lockfile that points at the public registry lands here. The 404 says
	// why and how to get a URL this Depsilo serves.
	var problem struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(readBody(t, resp)), &problem); err != nil {
		t.Fatalf("unsigned npm tarball body is not a JSON error: %v", err)
	}
	// npm prints only the "error" field, so it must carry the explanation too.
	if problem.Code != "NPM_UNSIGNED_TARBALL" || problem.Error != problem.Message ||
		!strings.Contains(problem.Message, "not issued by this Depsilo") ||
		!strings.Contains(problem.Message, "npm install --package-lock-only --omit-lockfile-registry-resolved") {
		t.Fatalf("unsigned npm tarball error = %+v", problem)
	}
	if got := mockRequestCountForPath(upstreamPath); got != before {
		t.Fatalf("unsigned npm tarball contacted upstream: before=%d after=%d", before, got)
	}
}

func TestNpm_RejectedTarballReachesAdminLogs(t *testing.T) {
	// The access-log batch is written every few seconds. Waiting for it between
	// sequential tests would let the 2s blob TTL expire under the cache-hit
	// tests, so this one runs after them.
	t.Parallel()

	const upstreamPath = "/legacy-log-fixture/-/legacy-log-fixture-1.0.0.tgz"
	resp := httpGet(t, depsiloURL+"/npm"+upstreamPath)
	assertStatus(t, resp, http.StatusNotFound)
	_ = readBody(t, resp)

	type accessLogs struct {
		Items []struct {
			CacheKey   string `json:"cache_key"`
			StatusCode int    `json:"status_code"`
			Upstream   string `json:"upstream"`
		} `json:"items"`
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		response := adminGet(t, depsiloURL+"/api/v1/admin/logs?adapter_type=npm&search=legacy-log-fixture")
		assertStatus(t, response, http.StatusOK)
		var logs accessLogs
		if err := json.Unmarshal([]byte(readBody(t, response)), &logs); err != nil {
			t.Fatalf("decode access logs: %v", err)
		}
		for _, item := range logs.Items {
			if item.StatusCode == http.StatusNotFound && item.Upstream == "" &&
				strings.HasSuffix(item.CacheKey, upstreamPath) {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("rejected npm tarball did not reach Admin access logs: %+v", logs.Items)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
