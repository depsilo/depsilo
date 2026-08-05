package server

import (
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

const testFrontendDocument = `<!doctype html><html><body><div id="root"></div></body></html>`

func TestFrontendFallbackDoesNotCaptureMachineRoutes(t *testing.T) {
	engine := newFrontendTestEngine("/private-index")

	for _, requestPath := range []string{
		"/apt/debian-security",
		"/apt/debian-security/nope",
		"/apt-security/dists/trixie-security/InRelease",
		"/api/v1/does-not-exist",
		"/pypi/",
		"/npm/",
		"/p/example/apt/debian",
		"/private-index/simple/package/",
		"/assets/does-not-exist.js",
		"/healthz",
		"/readyz",
		"/livez",
		"/metric",
		"/metricsz",
		"/API/v1/does-not-exist",
		// Common aliases for protocol endpoints are invalid and must not look
		// like successful Portal navigation.
		"/cargo/index/config.json",
		"/docker/v2/",
		"/pip/simple/package/",
		"/gem/specs.4.8.gz",
	} {
		t.Run(requestPath, func(t *testing.T) {
			response := serveFrontendRequest(
				engine,
				http.MethodGet,
				requestPath,
				map[string]string{"Accept": "text/html"},
			)

			assertPlainFrontendError(t, response, http.StatusNotFound)
		})
	}
}

func TestFrontendFallbackKeepsBrowserRoutes(t *testing.T) {
	engine := newFrontendTestEngine()

	for _, requestPath := range []string{
		"/",
		"/monitor",
		"/admin/upstreams",
		"/admin/upstreams/",
		"/does-not-exist",
	} {
		t.Run("GET "+requestPath, func(t *testing.T) {
			response := serveFrontendRequest(
				engine,
				http.MethodGet,
				requestPath,
				map[string]string{"Accept": "text/html,application/xhtml+xml"},
			)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
			}
			if contentType := mediaType(response.Header().Get("Content-Type")); contentType != "text/html" {
				t.Fatalf("content-type = %q, want text/html", contentType)
			}
			if !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
				t.Fatalf("body does not contain the Portal root: %q", response.Body.String())
			}
			if requestPath != "/" && !headerContainsToken(response.Header(), "Vary", "Accept") {
				t.Fatalf("Vary = %q, want it to contain Accept", response.Header().Values("Vary"))
			}
		})
	}

	response := serveFrontendRequest(
		engine,
		http.MethodHead,
		"/admin/upstreams",
		map[string]string{"Accept": "text/html"},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want %d", response.Code, http.StatusOK)
	}
	if contentType := mediaType(response.Header().Get("Content-Type")); contentType != "text/html" {
		t.Fatalf("HEAD content-type = %q, want text/html", contentType)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("HEAD body = %q, want empty", response.Body.String())
	}
	if !headerContainsToken(response.Header(), "Vary", "Accept") {
		t.Fatalf("HEAD Vary = %q, want it to contain Accept", response.Header().Values("Vary"))
	}

	multipleAcceptResponse := httptest.NewRecorder()
	multipleAcceptRequest := httptest.NewRequest(http.MethodGet, "/monitor", nil)
	multipleAcceptRequest.Header.Add("Accept", "application/json")
	multipleAcceptRequest.Header.Add("Accept", "text/html")
	engine.ServeHTTP(multipleAcceptResponse, multipleAcceptRequest)
	if multipleAcceptResponse.Code != http.StatusOK {
		t.Fatalf(
			"multiple Accept status = %d, want %d; body=%q",
			multipleAcceptResponse.Code,
			http.StatusOK,
			multipleAcceptResponse.Body.String(),
		)
	}
}

func TestFrontendRootRemainsCanonicalEntry(t *testing.T) {
	engine := newFrontendTestEngine()

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := serveFrontendRequest(engine, method, "/", nil)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
			}
			if contentType := mediaType(response.Header().Get("Content-Type")); contentType != "text/html" {
				t.Fatalf("content-type = %q, want text/html", contentType)
			}
			if method == http.MethodGet && !strings.Contains(response.Body.String(), `<div id="root"></div>`) {
				t.Fatalf("body does not contain the Portal root: %q", response.Body.String())
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatalf("HEAD body = %q, want empty", response.Body.String())
			}
		})
	}
}

func TestFrontendServesEmbeddedFilesOnlyForSafeMethods(t *testing.T) {
	engine := newFrontendTestEngine()

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			response := serveFrontendRequest(engine, method, "/favicon.svg", nil)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, http.StatusOK, response.Body.String())
			}
			if contentType := mediaType(response.Header().Get("Content-Type")); contentType != "image/svg+xml" {
				t.Fatalf("content-type = %q, want image/svg+xml", contentType)
			}
			if method == http.MethodGet && strings.Contains(response.Body.String(), `<div id="root"></div>`) {
				t.Fatal("embedded SVG was served the Portal document")
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatalf("HEAD body = %q, want empty", response.Body.String())
			}
		})
	}

	assetResponse := serveFrontendRequest(engine, http.MethodGet, "/assets/app-123.js", nil)
	if assetResponse.Code != http.StatusOK {
		t.Fatalf(
			"asset status = %d, want %d; body=%q",
			assetResponse.Code,
			http.StatusOK,
			assetResponse.Body.String(),
		)
	}
	if contentType := mediaType(assetResponse.Header().Get("Content-Type")); contentType != "text/javascript" {
		t.Fatalf("asset content-type = %q, want text/javascript", contentType)
	}
	if body := assetResponse.Body.String(); body != `console.log("test")` {
		t.Fatalf("asset body = %q, want the embedded JavaScript", body)
	}

	for _, requestPath := range []string{
		"/",
		"/favicon.svg",
	} {
		for _, method := range []string{
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		} {
			t.Run(method+" "+requestPath, func(t *testing.T) {
				response := serveFrontendRequest(engine, method, requestPath, nil)

				assertPlainFrontendError(t, response, http.StatusMethodNotAllowed)
				if allow := response.Header().Get("Allow"); allow != "GET, HEAD" {
					t.Fatalf("Allow = %q, want %q", allow, "GET, HEAD")
				}
			})
		}
	}
}

func TestFrontendFallbackRequiresBrowserNavigationRequest(t *testing.T) {
	engine := newFrontendTestEngine()

	tests := []struct {
		name    string
		method  string
		path    string
		headers map[string]string
	}{
		{name: "missing Accept", method: http.MethodGet, path: "/unregistered"},
		{
			name:    "JSON Accept",
			method:  http.MethodGet,
			path:    "/unregistered",
			headers: map[string]string{"Accept": "application/json"},
		},
		{
			name:    "wildcard Accept",
			method:  http.MethodGet,
			path:    "/unregistered",
			headers: map[string]string{"Accept": "*/*"},
		},
		{
			name:    "disabled HTML media range",
			method:  http.MethodGet,
			path:    "/unregistered",
			headers: map[string]string{"Accept": "text/html;q=0,application/json"},
		},
		{
			name:    "POST browser route",
			method:  http.MethodPost,
			path:    "/monitor",
			headers: map[string]string{"Accept": "text/html"},
		},
		{
			name:    "range fallback",
			method:  http.MethodGet,
			path:    "/unregistered",
			headers: map[string]string{"Accept": "text/html", "Range": "bytes=0-31"},
		},
		{
			name:    "unknown binary file",
			method:  http.MethodGet,
			path:    "/unregistered.bin",
			headers: map[string]string{"Accept": "text/html"},
		},
		{
			name:    "favicon typo",
			method:  http.MethodGet,
			path:    "/favicon.ico",
			headers: map[string]string{"Accept": "text/html"},
		},
		{
			name:    "robots file",
			method:  http.MethodGet,
			path:    "/robots.txt",
			headers: map[string]string{"Accept": "text/html"},
		},
		{
			name:    "web manifest",
			method:  http.MethodGet,
			path:    "/site.webmanifest",
			headers: map[string]string{"Accept": "text/html"},
		},
		{
			name:    "unknown JSON file",
			method:  http.MethodGet,
			path:    "/metadata.json",
			headers: map[string]string{"Accept": "text/html"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveFrontendRequest(engine, test.method, test.path, test.headers)

			assertPlainFrontendError(t, response, http.StatusNotFound)
		})
	}
}

func TestFrontendFallbackRejectsDotSegmentPaths(t *testing.T) {
	engine := newFrontendTestEngine()

	for _, requestPath := range []string{
		"/api/../monitor",
		"/apt/../monitor",
		"/pypi/../favicon.svg",
		"/api/%2e%2e/monitor",
		"/api/%2E%2E/monitor",
		"/api/./v1",
		"/api/%2e/v1",
		"/admin//upstreams",
		"/api//v1",
		"/api%5c..%5cmonitor",
	} {
		t.Run(requestPath, func(t *testing.T) {
			response := serveFrontendRequest(
				engine,
				http.MethodGet,
				requestPath,
				map[string]string{"Accept": "text/html"},
			)

			assertPlainFrontendError(t, response, http.StatusNotFound)
		})
	}
}

func newFrontendTestEngine(extraProxyPrefixes ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	registerFrontendFS(engine, fstest.MapFS{
		"index.html": {
			Data: []byte(testFrontendDocument),
			Mode: 0o444,
		},
		"favicon.svg": {
			Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
			Mode: 0o444,
		},
		"icons.svg": {
			Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
			Mode: 0o444,
		},
		"assets/app-123.js": {
			Data: []byte(`console.log("test")`),
			Mode: 0o444,
		},
	}, extraProxyPrefixes...)
	return engine
}

func serveFrontendRequest(
	engine http.Handler,
	method string,
	requestPath string,
	headers map[string]string,
) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, requestPath, nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	engine.ServeHTTP(response, request)
	return response
}

func assertPlainFrontendError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	wantStatus int,
) {
	t.Helper()

	if response.Code != wantStatus {
		t.Fatalf(
			"status = %d, want %d; content-type=%q body=%q",
			response.Code,
			wantStatus,
			response.Header().Get("Content-Type"),
			response.Body.String(),
		)
	}
	if contentType := mediaType(response.Header().Get("Content-Type")); contentType != "text/plain" {
		t.Fatalf("content-type = %q, want text/plain", contentType)
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cacheControl)
	}
	if nosniff := response.Header().Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", nosniff)
	}
	if strings.Contains(response.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("error response was served the Portal document: %q", response.Body.String())
	}
}

func mediaType(contentType string) string {
	parsed, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(contentType))
	}
	return strings.ToLower(parsed)
}

func headerContainsToken(header http.Header, name string, token string) bool {
	for _, value := range header.Values(name) {
		for _, candidate := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(candidate), token) {
				return true
			}
		}
	}
	return false
}
