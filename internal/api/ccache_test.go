package api

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
	"depsilo/internal/compilecache"
	"depsilo/internal/db"
)

const ccacheTestKey = "0123456789abcdef0123456789abcdef01234567"

type ccacheFixture struct {
	router      *gin.Engine
	handler     *CCacheHandler
	writeToken  string
	readToken   string
	namespace   string
	flatPath    string
	subdirsPath string
}

func newCCacheFixture(t *testing.T, maxEntryBytes int64) ccacheFixture {
	return newCCacheFixtureWithTimeout(t, maxEntryBytes, time.Minute)
}

func newCCacheFixtureWithTimeout(t *testing.T, maxEntryBytes int64, uploadTimeout time.Duration) ccacheFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "ccache.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheEntry{}, &db.CompileCacheCredential{}, &db.CompileCacheDeletion{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := compilecache.NewService(storage, database, compilecache.Limits{
		MaxBytes: 1024, MaxEntries: 1000, MaxEntryBytes: maxEntryBytes,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 1000,
		MaxConcurrentUploads: 4, MaxInflightUploadBytes: maxEntryBytes,
		UploadTimeout: uploadTimeout, MaxConcurrentDownloads: 4, DownloadTimeout: time.Minute,
		HighWatermarkPercent: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	namespace := "team-a"
	writeToken := "depsilo_cc_write_test"
	readToken := "depsilo_cc_read_test"
	for name, item := range map[string]struct {
		raw        string
		permission string
	}{
		"writer": {writeToken, "readwrite"},
		"reader": {readToken, "readonly"},
	} {
		digest := sha256.Sum256([]byte(item.raw))
		if err := database.Create(&db.CompileCacheCredential{
			Name: name, Namespace: namespace, TokenHash: hex.EncodeToString(digest[:]), Permissions: item.permission,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	handler := NewCCacheHandler(true, service, compilecache.NewAuthorizer(database))
	router := gin.New()
	router.Any("/ccache/v1/:namespace/*key", handler.Handle)
	return ccacheFixture{
		router: router, handler: handler, writeToken: writeToken, readToken: readToken, namespace: namespace,
		flatPath:    "/ccache/v1/" + namespace + "/" + ccacheTestKey,
		subdirsPath: "/ccache/v1/" + namespace + "/" + ccacheTestKey[:2] + "/" + ccacheTestKey[2:],
	}
}

func TestCCacheCredentialStoreFailureReturnsServiceUnavailable(t *testing.T) {
	fixture := newCCacheFixture(t, 512)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "closed-auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheCredential{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.handler.authorizer = compilecache.NewAuthorizer(database)
	response := fixture.request(http.MethodGet, fixture.flatPath, fixture.writeToken, nil)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("credential store failure = %d, want %d; body=%s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func (fixture ccacheFixture) request(method, path, token string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, request)
	return recorder
}

func TestCCacheHTTPRemoteStorageRoundTrip(t *testing.T) {
	fixture := newCCacheFixture(t, 512)
	payload := []byte("ccache opaque result")

	if response := fixture.request(http.MethodGet, fixture.subdirsPath, fixture.writeToken, nil); response.Code != http.StatusNotFound {
		t.Fatalf("initial GET = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPut, fixture.subdirsPath, fixture.writeToken, bytes.NewReader(payload)); response.Code != http.StatusCreated {
		t.Fatalf("first PUT = %d, body = %s", response.Code, response.Body.String())
	}
	response := fixture.request(http.MethodHead, fixture.subdirsPath, fixture.writeToken, nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Length") != strconv.Itoa(len(payload)) || response.Body.Len() != 0 {
		t.Fatalf("HEAD = %d length=%q body=%q", response.Code, response.Header().Get("Content-Length"), response.Body.String())
	}
	// Flat and default subdirs layouts address the same canonical object.
	response = fixture.request(http.MethodGet, fixture.flatPath, fixture.readToken, nil)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), payload) {
		t.Fatalf("GET = %d, body = %q", response.Code, response.Body.Bytes())
	}
	replacement := []byte("replacement")
	if response := fixture.request(http.MethodPut, fixture.flatPath, fixture.writeToken, bytes.NewReader(replacement)); response.Code != http.StatusNoContent {
		t.Fatalf("overwrite PUT = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodDelete, fixture.subdirsPath, fixture.writeToken, nil); response.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, body = %s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodDelete, fixture.subdirsPath, fixture.writeToken, nil); response.Code != http.StatusNoContent {
		t.Fatalf("idempotent DELETE = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestCCacheHTTPRemoteStorageEnforcesCredentialsAndBounds(t *testing.T) {
	fixture := newCCacheFixture(t, 4)
	for name, response := range map[string]*httptest.ResponseRecorder{
		"missing token":      fixture.request(http.MethodGet, fixture.flatPath, "", nil),
		"namespace mismatch": fixture.request(http.MethodGet, "/ccache/v1/team-b/"+ccacheTestKey, fixture.writeToken, nil),
		"readonly write":     fixture.request(http.MethodPut, fixture.flatPath, fixture.readToken, bytes.NewReader([]byte("four"))),
		"oversized":          fixture.request(http.MethodPut, fixture.flatPath, fixture.writeToken, bytes.NewReader([]byte("12345"))),
	} {
		want := map[string]int{
			"missing token": http.StatusUnauthorized, "namespace mismatch": http.StatusForbidden,
			"readonly write": http.StatusForbidden, "oversized": http.StatusRequestEntityTooLarge,
		}[name]
		if response.Code != want {
			t.Errorf("%s status = %d, want %d; body=%s", name, response.Code, want, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodPut, fixture.flatPath, bytes.NewReader([]byte("four")))
	request.Header.Set("Authorization", "Bearer "+fixture.writeToken)
	request.ContentLength = -1
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusLengthRequired {
		t.Fatalf("chunked PUT status = %d, want %d", recorder.Code, http.StatusLengthRequired)
	}
}

func TestCCacheSlowUploadTimesOutAndReleasesSlot(t *testing.T) {
	fixture := newCCacheFixtureWithTimeout(t, 4, 100*time.Millisecond)
	server := httptest.NewServer(fixture.router)
	defer server.Close()
	rawRequest := "PUT " + fixture.flatPath + " HTTP/1.1\r\n" +
		"Host: cache.test\r\n" +
		"Authorization: Bearer " + fixture.writeToken + "\r\n" +
		"Content-Length: 4\r\n\r\nx"
	started := time.Now()
	response, responseBody := sendRawHTTP1(t, server.URL, http.MethodPut, rawRequest)
	if response.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("slow PUT = %d, want %d; body=%s", response.StatusCode, http.StatusRequestTimeout, responseBody)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("slow PUT took %s", elapsed)
	}
	if !response.Close {
		t.Fatal("timed out upload connection was kept alive")
	}

	if response := fixture.request(http.MethodPut, fixture.flatPath, fixture.writeToken, bytes.NewReader([]byte("four"))); response.Code != http.StatusCreated {
		t.Fatalf("PUT after timeout = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestCCacheRejectsUnauthorizedUploadWithoutDrainingBody(t *testing.T) {
	fixture := newCCacheFixtureWithTimeout(t, 4, time.Second)
	server := httptest.NewServer(fixture.router)
	defer server.Close()
	rawRequest := "PUT " + fixture.flatPath + " HTTP/1.1\r\n" +
		"Host: cache.test\r\nContent-Length: 1\r\n\r\n"
	started := time.Now()
	response, responseBody := sendRawHTTP1(t, server.URL, http.MethodPut, rawRequest)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized PUT = %d, body=%s", response.StatusCode, responseBody)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("unauthorized PUT waited for its unread body for %s", elapsed)
	}
	if !response.Close {
		t.Fatal("unauthorized upload connection was kept alive")
	}
}

func TestCCacheRejectsBodiesOnBodylessMethodsWithoutDraining(t *testing.T) {
	fixture := newCCacheFixtureWithTimeout(t, 4, time.Second)
	server := httptest.NewServer(fixture.router)
	defer server.Close()
	for _, test := range []struct {
		method string
		token  string
	}{
		{method: http.MethodGet, token: fixture.readToken},
		{method: http.MethodHead, token: fixture.readToken},
		{method: http.MethodDelete, token: fixture.writeToken},
	} {
		t.Run(test.method, func(t *testing.T) {
			// Make the bodyless method's normal path a success. Without the
			// explicit rejection under test, net/http would otherwise try to
			// drain the missing request byte after writing that success response.
			put := fixture.request(http.MethodPut, fixture.flatPath, fixture.writeToken, bytes.NewReader([]byte("four")))
			if put.Code != http.StatusCreated && put.Code != http.StatusNoContent {
				t.Fatalf("prepare cache entry = %d, body=%s", put.Code, put.Body.String())
			}
			rawRequest := test.method + " " + fixture.flatPath + " HTTP/1.1\r\n" +
				"Host: cache.test\r\n" +
				"Authorization: Bearer " + test.token + "\r\n" +
				"Content-Length: 1\r\n\r\n"
			started := time.Now()
			response, _ := sendRawHTTP1(t, server.URL, test.method, rawRequest)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("%s with body = %d, want %d", test.method, response.StatusCode, http.StatusBadRequest)
			}
			if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
				t.Fatalf("%s waited for its unread body for %s", test.method, elapsed)
			}
			if !response.Close {
				t.Fatalf("%s with an unread body kept the connection alive", test.method)
			}
		})
	}
}

func sendRawHTTP1(t *testing.T, serverURL, method, request string) (*http.Response, string) {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", parsed.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: method})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(body)
}

func TestCCacheDisabledRouteCannotBecomeSPAHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewCCacheHandler(false, nil, nil)
	router.Any("/ccache/v1/:namespace/*key", handler.Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ccache/v1/team-a/"+ccacheTestKey, nil))
	if response.Code != http.StatusNotFound || response.Body.Len() != 0 || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("disabled response = %d %q", response.Code, response.Body.String())
	}
}

func TestCCacheDisabledUploadDoesNotDrainUnreadBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewCCacheHandler(false, nil, nil)
	router.Any("/ccache/v1/:namespace/*key", handler.Handle)
	server := httptest.NewServer(router)
	defer server.Close()
	path := "/ccache/v1/team-a/" + ccacheTestKey
	rawRequest := "PUT " + path + " HTTP/1.1\r\nHost: cache.test\r\nContent-Length: 1\r\n\r\n"
	started := time.Now()
	response, responseBody := sendRawHTTP1(t, server.URL, http.MethodPut, rawRequest)
	if response.StatusCode != http.StatusNotFound || responseBody != "" {
		t.Fatalf("disabled PUT = %d body=%q", response.StatusCode, responseBody)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("disabled PUT waited for its unread body for %s", elapsed)
	}
	if !response.Close {
		t.Fatal("disabled upload connection was kept alive")
	}
}
