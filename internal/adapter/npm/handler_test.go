package npm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

func TestMetadataUpstreamFailureIsRecordedAsAuditError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	audit := &adaptertest.AuditCapture{}
	scope := adapter.NewRequestScope(nil, audit, nil, nil)

	handler := New(manager, adaptertest.UnavailableSelector{}, config.CacheConfig{TTLIndex: time.Hour}, database, testNPMTarballSigningKey)
	router := gin.New()
	handler.Register(router.Group("/npm"))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/npm/is-number", nil)
	scope.Wrap(router).ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if len(audit.Entries) != 1 {
		t.Fatalf("audit entries = %#v", audit.Entries)
	}
	entry := audit.Entries[0]
	if entry.Ecosystem != "npm" || entry.PackageName != "is-number" || entry.Action != "metadata" ||
		entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
		t.Fatalf("audit entry = %#v", entry)
	}
}

func TestMetadataCacheDoesNotCrossLegacyPackageNameCase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var requests atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+strings.TrimPrefix(request.URL.Path, "/")+`","versions":{}}`)
	}))
	t.Cleanup(upstreamServer.Close)

	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{TTLIndex: time.Hour}, database, testNPMTarballSigningKey)
	router := gin.New()
	handler.Register(router.Group("/npm"))

	requestPackage := func(name string) string {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/npm/"+name, nil)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body=%s", name, response.Code, response.Body.String())
		}
		return response.Body.String()
	}

	if body := requestPackage("Express"); !strings.Contains(body, `"name":"Express"`) {
		t.Fatalf("Express response = %s", body)
	}
	if body := requestPackage("express"); !strings.Contains(body, `"name":"express"`) {
		t.Fatalf("express response reused case-distinct cache: %s", body)
	}
	if body := requestPackage("Express"); !strings.Contains(body, `"name":"Express"`) {
		t.Fatalf("cached Express response = %s", body)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("upstream requests = %d, want one per case-distinct identity", got)
	}
}

func TestMetadataAcceptRepresentationsHaveIndependentCaches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		canonicalInstallAccept = "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*"
		caseOWSInstallAccept   = "APPLICATION/VND.NPM.INSTALL-V1+JSON ; Q=1.0 , application/json ; q=0.8 , */*"
		zeroQualityAccept      = "application/vnd.npm.install-v1+json; q=0, application/json; q=1"
		lowQualityAccept       = "application/vnd.npm.install-v1+json; q=.1, application/json; q=1"
		invalidQualityAccept   = "application/vnd.npm.install-v1+json; q=invalid, application/json"
		quotedQualityAccept    = "application/vnd.npm.install-v1+json; q=\"1\", application/json; q=0"
		wildcardAccept         = "*/*"
	)
	var requests atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		accept := request.Header.Get("Accept")
		packageName := strings.TrimPrefix(request.URL.Path, "/")
		if accept == canonicalInstallAccept || accept == caseOWSInstallAccept {
			w.Header().Set("Content-Type", "application/vnd.npm.install-v1+json")
			_, _ = io.WriteString(w, `{"name":"`+packageName+`","variant":"install","versions":{}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+packageName+`","variant":"full","versions":{}}`)
	}))
	t.Cleanup(upstreamServer.Close)

	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{TTLIndex: time.Hour}, database, testNPMTarballSigningKey)
	router := gin.New()
	handler.Register(router.Group("/npm"))

	requestMetadata := func(packageName, accept string) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/npm/"+packageName, nil)
		if accept != "" {
			request.Header.Set("Accept", accept)
		}
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("Accept %q status = %d, body=%s", accept, response.Code, response.Body.String())
		}
		return response
	}
	assertVariant := func(packageName, accept, want string) {
		t.Helper()
		response := requestMetadata(packageName, accept)
		if !strings.Contains(response.Body.String(), `"variant":"`+want+`"`) {
			t.Fatalf("package %q Accept %q body = %s, want %s representation", packageName, accept, response.Body.String(), want)
		}
	}

	// A canonical install-v1 response must not poison JSON-preferred variants.
	const installFirst = "accept-install-first"
	install := requestMetadata(installFirst, canonicalInstallAccept)
	if !strings.Contains(install.Body.String(), `"variant":"install"`) {
		t.Fatalf("install metadata body = %s", install.Body.String())
	}
	if got := install.Header().Get("Vary"); got != "Accept" {
		t.Fatalf("Vary = %q, want Accept", got)
	}
	assertVariant(installFirst, zeroQualityAccept, "full")
	assertVariant(installFirst, lowQualityAccept, "full")
	beforeRepeats := requests.Load()
	assertVariant(installFirst, canonicalInstallAccept, "install")
	assertVariant(installFirst, zeroQualityAccept, "full")
	assertVariant(installFirst, lowQualityAccept, "full")
	if got := requests.Load(); got != beforeRepeats {
		t.Fatalf("stable cached variants contacted upstream: before=%d after=%d", beforeRepeats, got)
	}

	// The reverse request order must not let a JSON response occupy the legacy
	// install-v1 key either.
	const fullFirst = "accept-full-first"
	assertVariant(fullFirst, lowQualityAccept, "full")
	assertVariant(fullFirst, canonicalInstallAccept, "install")
	beforeRepeats = requests.Load()
	assertVariant(fullFirst, lowQualityAccept, "full")
	assertVariant(fullFirst, canonicalInstallAccept, "install")
	if got := requests.Load(); got != beforeRepeats {
		t.Fatalf("reverse-order cached variants contacted upstream: before=%d after=%d", beforeRepeats, got)
	}

	// A safely parsed, case-insensitive install preference has the same
	// upstream representation and may share the v0.9-compatible cache entry.
	const equivalentInstall = "accept-equivalent-install"
	assertVariant(equivalentInstall, caseOWSInstallAccept, "install")
	beforeRepeats = requests.Load()
	assertVariant(equivalentInstall, canonicalInstallAccept, "install")
	if got := requests.Load(); got != beforeRepeats {
		t.Fatalf("equivalent install-v1 Accept contacted upstream: before=%d after=%d", beforeRepeats, got)
	}

	// Unsafe or non-explicit negotiation must never reuse the install key.
	const conservativeFallbacks = "accept-conservative-fallbacks"
	assertVariant(conservativeFallbacks, canonicalInstallAccept, "install")
	assertVariant(conservativeFallbacks, invalidQualityAccept, "full")
	assertVariant(conservativeFallbacks, quotedQualityAccept, "full")
	assertVariant(conservativeFallbacks, wildcardAccept, "full")
	beforeRepeats = requests.Load()
	assertVariant(conservativeFallbacks, invalidQualityAccept, "full")
	assertVariant(conservativeFallbacks, quotedQualityAccept, "full")
	assertVariant(conservativeFallbacks, wildcardAccept, "full")
	if got := requests.Load(); got != beforeRepeats {
		t.Fatalf("fallback cached variants contacted upstream: before=%d after=%d", beforeRepeats, got)
	}
}

func TestInstallV1AcceptRetainsV090MetadataCacheKey(t *testing.T) {
	baseKey := MetadataCacheKey("upgrade-fixture")
	tests := []struct {
		name       string
		accept     string
		wantLegacy bool
	}{
		{name: "canonical install preference", accept: "application/vnd.npm.install-v1+json; q=1.0, application/json; q=0.8, */*", wantLegacy: true},
		{name: "case insensitive with OWS", accept: "APPLICATION/VND.NPM.INSTALL-V1+JSON ; Q=1.0 , application/json ; q=0.8 , */*", wantLegacy: true},
		{name: "install explicitly unacceptable", accept: "application/vnd.npm.install-v1+json; q=0, application/json; q=1"},
		{name: "json preferred", accept: "application/vnd.npm.install-v1+json; q=.1, application/json; q=1"},
		{name: "invalid quality", accept: "application/vnd.npm.install-v1+json; q=invalid, application/json"},
		{name: "quoted quality", accept: "application/vnd.npm.install-v1+json; q=\"1\", application/json; q=0"},
		{name: "wildcard only", accept: "*/*"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := metadataCacheKeyForAccept(baseKey, tt.accept)
			if tt.wantLegacy && got != baseKey {
				t.Fatalf("cache key = %q, want v0.9-compatible %q", got, baseKey)
			}
			if !tt.wantLegacy && got == baseKey {
				t.Fatalf("Accept %q reused install-v1 legacy key %q", tt.accept, baseKey)
			}
		})
	}
}
