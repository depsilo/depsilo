package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/compilecache"
	"depsilo/internal/db"
)

const (
	sccacheTestKey   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	sccacheCheckBody = "Hello, World!"
)

type sccacheFixture struct {
	router     *gin.Engine
	database   *gorm.DB
	handler    *SCCacheHandler
	writeToken string
	readToken  string
	namespace  string
	objectPath string
	checkPath  string
}

type failingProbeStorage struct{ cache.Storage }

func (storage *failingProbeStorage) Put(context.Context, string, io.Reader, int64, string) error {
	return errors.New("storage is read-only")
}

func newSCCacheFixture(t *testing.T, maxEntryBytes int64) sccacheFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "sccache.db"))
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
		UploadTimeout: time.Minute, MaxConcurrentDownloads: 4, DownloadTimeout: time.Minute,
		HighWatermarkPercent: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	namespace := "team-a"
	writeToken := "depsilo_sccache_write_test"
	readToken := "depsilo_sccache_read_test"
	for name, credential := range map[string]struct {
		raw        string
		permission string
	}{
		"sccache-writer": {raw: writeToken, permission: "readwrite"},
		"sccache-reader": {raw: readToken, permission: "readonly"},
	} {
		digest := sha256.Sum256([]byte(credential.raw))
		if err := database.Create(&db.CompileCacheCredential{
			Name: name, Namespace: namespace, TokenHash: hex.EncodeToString(digest[:]), Permissions: credential.permission,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	handler := NewSCCacheHandler(true, service, compilecache.NewAuthorizer(database))
	router := gin.New()
	router.Any("/sccache/v1/:namespace/*path", handler.Handle)
	router.Handle(methodPropfind, "/sccache/v1/:namespace/*path", handler.Handle)
	router.Handle(methodMkcol, "/sccache/v1/:namespace/*path", handler.Handle)
	basePath := "/sccache/v1/" + namespace + "/"
	return sccacheFixture{
		router: router, database: database, handler: handler,
		writeToken: writeToken, readToken: readToken, namespace: namespace,
		objectPath: basePath + strings.Join([]string{sccacheTestKey[:1], sccacheTestKey[1:2], sccacheTestKey[2:3], sccacheTestKey}, "/"),
		checkPath:  basePath + ".sccache_check",
	}
}

func (fixture sccacheFixture) request(
	method, path, token string,
	body io.Reader,
	configure ...func(*http.Request),
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, body)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for _, apply := range configure {
		apply(request)
	}
	recorder := httptest.NewRecorder()
	fixture.router.ServeHTTP(recorder, request)
	return recorder
}

func TestSCCacheWebDAVArtifactRoundTrip(t *testing.T) {
	fixture := newSCCacheFixture(t, 512)
	payload := []byte("opaque sccache result")

	if response := fixture.request(http.MethodGet, fixture.objectPath, fixture.writeToken, nil); response.Code != http.StatusNotFound {
		t.Fatalf("initial GET = %d, body=%s", response.Code, response.Body.String())
	}
	if response := fixture.request(http.MethodPut, fixture.objectPath, fixture.writeToken, bytes.NewReader(payload)); response.Code != http.StatusCreated {
		t.Fatalf("first PUT = %d, body=%s", response.Code, response.Body.String())
	}
	response := fixture.request(http.MethodGet, fixture.objectPath, fixture.readToken, nil)
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), payload) {
		t.Fatalf("GET hit = %d, body=%q", response.Code, response.Body.Bytes())
	}

	replacement := []byte("replacement")
	if response := fixture.request(http.MethodPut, fixture.objectPath, fixture.writeToken, bytes.NewReader(replacement)); response.Code != http.StatusNoContent {
		t.Fatalf("overwrite PUT = %d, body=%s", response.Code, response.Body.String())
	}
	response = fixture.request(http.MethodHead, fixture.objectPath, fixture.readToken, nil)
	if response.Code != http.StatusOK || response.Header().Get("Content-Length") != strconv.Itoa(len(replacement)) || response.Body.Len() != 0 {
		t.Fatalf("HEAD = %d length=%q body=%q", response.Code, response.Header().Get("Content-Length"), response.Body.String())
	}
}

func TestSCCacheCheckIsSyntheticAndPermissionAware(t *testing.T) {
	fixture := newSCCacheFixture(t, 512)
	response := fixture.request(http.MethodGet, fixture.checkPath, fixture.readToken, nil)
	if response.Code != http.StatusOK || response.Body.String() != sccacheCheckBody {
		t.Fatalf("check GET = %d, body=%q", response.Code, response.Body.String())
	}

	response = fixture.request(http.MethodPut, fixture.checkPath, fixture.writeToken, strings.NewReader(sccacheCheckBody))
	if response.Code < http.StatusOK || response.Code >= http.StatusMultipleChoices {
		t.Fatalf("RW check PUT = %d, body=%q", response.Code, response.Body.String())
	}
	response = fixture.request(http.MethodPut, fixture.checkPath, fixture.readToken, strings.NewReader(sccacheCheckBody))
	if response.Code != http.StatusForbidden {
		t.Fatalf("RO check PUT = %d, want %d; body=%q", response.Code, http.StatusForbidden, response.Body.String())
	}

	var entries int64
	if err := fixture.database.Model(&db.CompileCacheEntry{}).Count(&entries).Error; err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("synthetic check created %d compiler-cache entries", entries)
	}
}

func TestSCCacheCheckReportsUnderlyingStorageWriteFailure(t *testing.T) {
	fixture := newSCCacheFixture(t, 512)
	local, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "read-only-objects"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := compilecache.NewService(&failingProbeStorage{Storage: local}, fixture.database, compilecache.Limits{
		MaxBytes: 1024, MaxEntries: 1000, MaxEntryBytes: 512,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 1000,
		MaxConcurrentUploads: 1, MaxQueuedUploads: 1, MaxInflightUploadBytes: 512,
		UploadTimeout: time.Minute, MaxConcurrentDownloads: 1, DownloadTimeout: time.Minute,
		HighWatermarkPercent: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler.service = service
	response := fixture.request(http.MethodPut, fixture.checkPath, fixture.writeToken, strings.NewReader(sccacheCheckBody))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("check PUT against read-only storage = %d, want %d; body=%q",
			response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
}

func TestSCCacheWebDAVVirtualCollections(t *testing.T) {
	fixture := newSCCacheFixture(t, 512)
	base := "/sccache/v1/" + fixture.namespace
	paths := []string{base + "/", base + "/0", base + "/0/1", base + "/0/1/2"}
	for _, path := range paths {
		path := path
		t.Run("PROPFIND_"+strings.TrimPrefix(path, base), func(t *testing.T) {
			response := fixture.request(
				"PROPFIND", path, fixture.readToken,
				strings.NewReader(`<?xml version="1.0"?><propfind xmlns="DAV:"/>`),
				func(request *http.Request) { request.Header.Set("Depth", "0") },
			)
			body := strings.ToLower(response.Body.String())
			if response.Code != http.StatusMultiStatus || !strings.Contains(body, "multistatus") || !strings.Contains(body, "collection") {
				t.Fatalf("PROPFIND %s = %d, body=%q", path, response.Code, response.Body.String())
			}
		})
	}

	for _, path := range paths[1:] {
		response := fixture.request("MKCOL", path, fixture.writeToken, nil)
		if response.Code < http.StatusOK || response.Code >= http.StatusMultipleChoices {
			t.Errorf("MKCOL %s = %d, body=%q", path, response.Code, response.Body.String())
		}
	}
}

func TestSCCacheWebDAVRejectsInvalidProtocolRequests(t *testing.T) {
	fixture := newSCCacheFixture(t, 512)
	base := "/sccache/v1/" + fixture.namespace + "/"
	mismatchedShard := base + "0/1/3/" + sccacheTestKey

	tests := []struct {
		name      string
		method    string
		path      string
		body      io.Reader
		configure func(*http.Request)
		want      int
	}{
		{name: "mismatched key shard", method: http.MethodGet, path: mismatchedShard, want: http.StatusBadRequest},
		{name: "noncanonical key", method: http.MethodGet, path: base + sccacheTestKey, want: http.StatusBadRequest},
		{name: "PROPFIND missing depth", method: "PROPFIND", path: base, want: http.StatusBadRequest},
		{name: "PROPFIND recursive depth", method: "PROPFIND", path: base + "0", configure: func(request *http.Request) {
			request.Header.Set("Depth", "1")
		}, want: http.StatusBadRequest},
		{name: "GET body", method: http.MethodGet, path: fixture.objectPath, body: strings.NewReader("x"), want: http.StatusBadRequest},
		{name: "MKCOL body", method: "MKCOL", path: base + "0", body: strings.NewReader("x"), want: http.StatusBadRequest},
		{name: "unsupported method", method: http.MethodPost, path: fixture.objectPath, want: http.StatusMethodNotAllowed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var configure []func(*http.Request)
			if test.configure != nil {
				configure = append(configure, test.configure)
			}
			response := fixture.request(test.method, test.path, fixture.writeToken, test.body, configure...)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestSCCacheWebDAVEnforcesCredentialsAndBounds(t *testing.T) {
	fixture := newSCCacheFixture(t, 4)
	wrongNamespacePath := strings.Replace(fixture.objectPath, "/"+fixture.namespace+"/", "/team-b/", 1)
	for _, test := range []struct {
		name   string
		method string
		path   string
		token  string
		body   io.Reader
		want   int
	}{
		{name: "missing token", method: http.MethodGet, path: fixture.objectPath, want: http.StatusUnauthorized},
		{name: "namespace mismatch", method: http.MethodGet, path: wrongNamespacePath, token: fixture.writeToken, want: http.StatusForbidden},
		{name: "readonly artifact PUT", method: http.MethodPut, path: fixture.objectPath, token: fixture.readToken, body: strings.NewReader("four"), want: http.StatusForbidden},
		{name: "oversized artifact", method: http.MethodPut, path: fixture.objectPath, token: fixture.writeToken, body: strings.NewReader("12345"), want: http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := fixture.request(test.method, test.path, test.token, test.body)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%q", response.Code, test.want, response.Body.String())
			}
		})
	}

	response := fixture.request(
		http.MethodPut, fixture.objectPath, fixture.writeToken, strings.NewReader("four"),
		func(request *http.Request) { request.ContentLength = -1 },
	)
	if response.Code != http.StatusLengthRequired {
		t.Fatalf("PUT without Content-Length = %d, want %d; body=%q", response.Code, http.StatusLengthRequired, response.Body.String())
	}
}

func TestSCCacheDisabledRouteCannotBecomeSPAHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewSCCacheHandler(false, nil, nil)
	router.Any("/sccache/v1/:namespace/*path", handler.Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/sccache/v1/team-a/0/1/2/"+sccacheTestKey, nil))
	if response.Code != http.StatusNotFound || response.Body.Len() != 0 || !strings.Contains(response.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("disabled response = %d %q", response.Code, response.Body.String())
	}
}
