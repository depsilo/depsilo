package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestCompileCacheStatusListsClientEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "compile-cache-status.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheEntry{}, &db.CompileCacheDeletion{}); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := compilecache.NewService(storage, database, compilecache.Limits{
		MaxBytes: 1024, MaxEntries: 100, MaxEntryBytes: 512,
		NamespaceMaxBytes: 1024, NamespaceMaxEntries: 100,
		MaxConcurrentUploads: 1, MaxInflightUploadBytes: 512, UploadTimeout: time.Minute,
		MaxConcurrentDownloads: 1, DownloadTimeout: time.Minute, HighWatermarkPercent: 90,
	})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewCompileCacheHandler(database, service, true, "https://cache.example.test/")
	router := gin.New()
	router.GET("/status", handler.Status)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/status", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Endpoint  string `json:"endpoint"`
		Endpoints struct {
			CCache  string `json:"ccache"`
			SCCache string `json:"sccache"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Endpoints.CCache != "https://cache.example.test/ccache/v1/{namespace}" ||
		body.Endpoints.SCCache != "https://cache.example.test/sccache/v1/{namespace}" {
		t.Fatalf("endpoints = %+v", body.Endpoints)
	}
	if body.Endpoint != body.Endpoints.CCache {
		t.Fatalf("legacy endpoint = %q, ccache endpoint = %q", body.Endpoint, body.Endpoints.CCache)
	}
}

func TestCompileCacheCredentialLifecycleUsesConfiguredEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "compile-cache-admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheCredential{}); err != nil {
		t.Fatal(err)
	}
	handler := NewCompileCacheHandler(database, nil, true, "https://cache.example.test/")
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", uint(7))
		c.Next()
	})
	router.POST("/credentials", handler.CreateCredential)
	router.GET("/credentials", handler.ListCredentials)
	router.DELETE("/credentials/:id", handler.DeleteCredential)

	request := httptest.NewRequest(http.MethodPost, "/credentials", bytes.NewBufferString(
		`{"name":"ci-writer","namespace":"team-a","permissions":"readwrite"}`,
	))
	request.Header.Set("Content-Type", "application/json")
	request.Host = "attacker.invalid"
	created := httptest.NewRecorder()
	router.ServeHTTP(created, request)
	if created.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", created.Code, created.Body.String())
	}
	if got := created.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("Cache-Control = %q", got)
	}
	var response struct {
		ID        uint   `json:"id"`
		Token     string `json:"token"`
		Endpoints struct {
			CCache  string `json:"ccache"`
			SCCache string `json:"sccache"`
		} `json:"endpoints"`
		CCacheRemoteStorage string     `json:"ccache_remote_storage"`
		SCCacheConfig       string     `json:"sccache_config"`
		Endpoint            string     `json:"endpoint"`
		RemoteStorage       string     `json:"remote_storage"`
		ExpiresAt           *time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Endpoint != "https://cache.example.test/ccache/v1/team-a" {
		t.Fatalf("endpoint = %q", response.Endpoint)
	}
	if response.RemoteStorage != response.Endpoint+"|@bearer-token="+response.Token {
		t.Fatalf("remote_storage = %q", response.RemoteStorage)
	}
	if response.Endpoints.CCache != response.Endpoint {
		t.Fatalf("endpoints.ccache = %q", response.Endpoints.CCache)
	}
	if response.Endpoints.SCCache != "https://cache.example.test/sccache/v1/team-a" {
		t.Fatalf("endpoints.sccache = %q", response.Endpoints.SCCache)
	}
	if response.CCacheRemoteStorage != response.RemoteStorage {
		t.Fatalf("ccache_remote_storage = %q", response.CCacheRemoteStorage)
	}
	wantSCCacheConfig := "[cache.webdav]\n" +
		"endpoint = \"https://cache.example.test/sccache/v1/team-a\"\n" +
		"token = \"" + response.Token + "\""
	if response.SCCacheConfig != wantSCCacheConfig {
		t.Fatalf("sccache_config = %q, want %q", response.SCCacheConfig, wantSCCacheConfig)
	}
	if strings.Contains(response.SCCacheConfig, "RW_MODE") || strings.Contains(response.SCCacheConfig, "rw_mode") {
		t.Fatalf("sccache_config included unsupported RW_MODE: %q", response.SCCacheConfig)
	}
	if strings.Contains(response.Endpoint, request.Host) {
		t.Fatal("endpoint trusted the request Host header")
	}
	if response.ExpiresAt == nil || time.Until(*response.ExpiresAt) < 89*24*time.Hour || time.Until(*response.ExpiresAt) > 91*24*time.Hour {
		t.Fatalf("default expiry = %v, want about 90 days", response.ExpiresAt)
	}
	var credential db.CompileCacheCredential
	if err := database.First(&credential, response.ID).Error; err != nil {
		t.Fatal(err)
	}
	if credential.CreatedBy != 7 || credential.TokenHash == "" || strings.Contains(created.Body.String(), credential.TokenHash) {
		t.Fatalf("stored credential metadata is invalid or leaked: %+v", credential)
	}
	if _, err := compilecache.NewAuthorizer(database).Authenticate(
		context.Background(), "Bearer "+response.Token, "team-a", true,
	); err != nil {
		t.Fatalf("new credential authentication: %v", err)
	}
	listed := httptest.NewRecorder()
	router.ServeHTTP(listed, httptest.NewRequest(http.MethodGet, "/credentials", nil))
	for _, privateField := range []string{"token_hash", "created_by", "revoked_at", "revoked_by"} {
		if strings.Contains(listed.Body.String(), privateField) {
			t.Fatalf("credential list leaked %s: %s", privateField, listed.Body.String())
		}
	}

	revoked := httptest.NewRecorder()
	credentialPath := "/credentials/" + strconv.FormatUint(uint64(response.ID), 10)
	router.ServeHTTP(revoked, httptest.NewRequest(http.MethodDelete, credentialPath, nil))
	if revoked.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, body=%s", revoked.Code, revoked.Body.String())
	}
	if err := database.First(&credential, response.ID).Error; err != nil {
		t.Fatal(err)
	}
	if credential.RevokedAt == nil || credential.RevokedBy == nil || *credential.RevokedBy != 7 {
		t.Fatalf("revocation audit fields = %+v", credential)
	}
	if _, err := compilecache.NewAuthorizer(database).Authenticate(
		context.Background(), "Bearer "+response.Token, "team-a", true,
	); !errors.Is(err, compilecache.ErrUnauthorized) {
		t.Fatalf("revoked authentication error = %v", err)
	}

	// Revocation is idempotent for automation retries while unknown IDs remain
	// distinguishable as 404.
	revokedAgain := httptest.NewRecorder()
	router.ServeHTTP(revokedAgain, httptest.NewRequest(http.MethodDelete, credentialPath, nil))
	if revokedAgain.Code != http.StatusNoContent {
		t.Fatalf("second revoke = %d", revokedAgain.Code)
	}
}

func TestCompileCacheRemoteStorageMatchesClientAndCredentialMode(t *testing.T) {
	for name, test := range map[string]struct {
		endpoint    string
		permissions string
		want        string
	}{
		"legacy HTTP writer": {
			endpoint: "http://127.0.0.1:23333/ccache/v1/team", permissions: "readwrite",
			want: "http://127.0.0.1:23333/ccache/v1/team|bearer-token=token",
		},
		"legacy HTTP reader": {
			endpoint: "http://127.0.0.1:23333/ccache/v1/team", permissions: "readonly",
			want: "http://127.0.0.1:23333/ccache/v1/team|read-only=true|bearer-token=token",
		},
		"HTTPS helper writer": {
			endpoint: "https://cache.example.test/ccache/v1/team", permissions: "readwrite",
			want: "https://cache.example.test/ccache/v1/team|@bearer-token=token",
		},
		"HTTPS helper reader": {
			endpoint: "https://cache.example.test/ccache/v1/team", permissions: "readonly",
			want: "https://cache.example.test/ccache/v1/team|read-only=true|@bearer-token=token",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := compileCacheRemoteStorage(test.endpoint, "token", test.permissions); got != test.want {
				t.Fatalf("remote storage = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCompileCacheReadonlyCredentialReturnsBothClientConfigurations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "compile-cache-readonly.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.CompileCacheCredential{}); err != nil {
		t.Fatal(err)
	}
	handler := NewCompileCacheHandler(database, nil, true, "https://cache.example.test")
	router := gin.New()
	router.POST("/credentials", handler.CreateCredential)
	request := httptest.NewRequest(http.MethodPost, "/credentials", bytes.NewBufferString(
		`{"name":"consumer","namespace":"team-a","permissions":"readonly","ttl_days":0}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create readonly = %d, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Token               string     `json:"token"`
		CCacheRemoteStorage string     `json:"ccache_remote_storage"`
		SCCacheConfig       string     `json:"sccache_config"`
		ExpiresAt           *time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.CCacheRemoteStorage, "|read-only=true|") ||
		!strings.Contains(body.CCacheRemoteStorage, "@bearer-token="+body.Token) {
		t.Fatalf("readonly ccache configuration = %q", body.CCacheRemoteStorage)
	}
	if !strings.Contains(body.SCCacheConfig, "/sccache/v1/team-a") ||
		!strings.Contains(body.SCCacheConfig, `token = "`+body.Token+`"`) ||
		strings.Contains(strings.ToLower(body.SCCacheConfig), "rw_mode") {
		t.Fatalf("readonly sccache configuration = %q", body.SCCacheConfig)
	}
	if body.ExpiresAt != nil {
		t.Fatalf("ttl_days=0 expiry = %v, want nil", body.ExpiresAt)
	}
	authorizer := compilecache.NewAuthorizer(database)
	if _, err := authorizer.Authenticate(context.Background(), "Bearer "+body.Token, "team-a", false); err != nil {
		t.Fatalf("readonly credential read: %v", err)
	}
	if _, err := authorizer.Authenticate(context.Background(), "Bearer "+body.Token, "team-a", true); !errors.Is(err, compilecache.ErrForbidden) {
		t.Fatalf("readonly credential write error = %v, want ErrForbidden", err)
	}
}
