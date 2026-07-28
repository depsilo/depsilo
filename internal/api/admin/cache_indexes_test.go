package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
	"depsilo/internal/upstreamupdates"
)

type cacheIndexesTestResponse struct {
	Items   []cacheIndexItem    `json:"items"`
	Total   int64               `json:"total"`
	Page    int                 `json:"page"`
	Summary []cacheIndexSummary `json:"summary"`
}

func TestCacheIndexesListsMetadataAndSummarizesByEcosystem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []db.CacheEntry{
		{Key: "pypi/simple/av/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata, PackageName: "av", ETag: `"av-v1"`, ExpiresAt: now.Add(time.Hour), LastAccessed: now, UpdatedAt: now},
		{Key: "npm/react/metadata.json", AdapterType: "npm", CacheKind: db.CacheKindMetadata, PackageName: "react", ExpiresAt: now.Add(-time.Hour), LastAccessed: now, UpdatedAt: now.Add(-time.Minute)},
		{Key: "huggingface/__query__/metadata/hash/api/models/acme/model/tree/main", AdapterType: "huggingface", CacheKind: db.CacheKindMetadata, ExpiresAt: now.Add(time.Hour)},
		{Key: "pypi/files/av.whl", AdapterType: "pypi", CacheKind: db.CacheKindArtifact, PackageName: "av", ExpiresAt: now.Add(time.Hour), LastAccessed: now},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	h := NewCacheHandler(database, nil, 1)
	router := gin.New()
	router.GET("/cache/indexes", h.ListIndexes)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/cache/indexes?page=1&page_size=10", nil)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var response cacheIndexesTestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 2 || len(response.Items) != 2 || len(response.Summary) != 2 {
		t.Fatalf("response = %+v", response)
	}
	statuses := map[string]string{}
	for _, item := range response.Items {
		statuses[item.PackageName] = item.Status
	}
	if statuses["av"] != "fresh" || statuses["react"] != "stale" {
		t.Fatalf("statuses = %#v", statuses)
	}
	for _, item := range response.Items {
		if item.PackageName == "av" && item.ETag != `"av-v1"` {
			t.Fatalf("av ETag = %q", item.ETag)
		}
	}
}

func TestCacheIndexesFiltersAndRejectsBadStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	rows := []db.CacheEntry{
		{Key: "pypi/simple/av/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata, PackageName: "av", ExpiresAt: now.Add(time.Hour)},
		{Key: "npm/react/metadata.json", AdapterType: "npm", CacheKind: db.CacheKindMetadata, PackageName: "react", ExpiresAt: now.Add(-time.Hour)},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	h := NewCacheHandler(database, nil, 1)
	router := gin.New()
	router.GET("/cache/indexes", h.ListIndexes)

	for _, path := range []string{
		"/cache/indexes?adapter_type=pypi",
		"/cache/indexes?status=fresh",
		"/cache/indexes?search=av",
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
		var response cacheIndexesTestResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Total != 1 || len(response.Items) != 1 || response.Items[0].PackageName != "av" {
			t.Fatalf("%s response = %+v", path, response)
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/cache/indexes?status=unknown", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status code = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestCacheIndexPublicPath(t *testing.T) {
	metadata := func(adapterType, key string) db.CacheEntry {
		return db.CacheEntry{AdapterType: adapterType, CacheKind: db.CacheKindMetadata, Key: key}
	}
	tests := []struct {
		name    string
		entry   db.CacheEntry
		want    string
		wantErr bool
	}{
		{name: "pypi", entry: metadata("pypi", "pypi/simple/pillow/index.html"), want: "/pypi/simple/pillow/"},
		{name: "npm", entry: metadata("npm", "npm/react/metadata.json"), want: "/npm/react"},
		{name: "npm scoped", entry: metadata("npm", "npm/@types/node/metadata.json"), want: "/npm/@types/node"},
		{name: "go list", entry: metadata("go", "go/example.com/acme/mod/@v/list"), want: "/go/example.com/acme/mod/@v/list"},
		{name: "go latest", entry: metadata("go", "go/example.com/acme/mod/@latest"), want: "/go/example.com/acme/mod/@latest"},
		{name: "cargo config", entry: metadata("cargo", "cargo/config.json"), want: "/crates/config.json"},
		{name: "cargo sparse index", entry: metadata("cargo", "cargo/index/pi/ll/pillow"), want: "/crates/pi/ll/pillow"},
		{name: "maven", entry: metadata("maven", "maven/org/acme/lib/maven-metadata.xml"), want: "/maven/org/acme/lib/maven-metadata.xml"},
		{name: "rubygems", entry: metadata("rubygems", "rubygems/versions"), want: "/rubygems/versions"},
		{name: "composer", entry: metadata("composer", "composer/p2/vendor/package.json"), want: "/composer/p2/vendor/package.json"},
		{name: "nuget", entry: metadata("nuget", "nuget/v3/index.json"), want: "/nuget/v3/index.json"},
		{name: "conda", entry: metadata("conda", "conda/linux-64/repodata.json"), want: "/conda/linux-64/repodata.json"},
		{name: "cran", entry: metadata("cran", "cran/src/contrib/PACKAGES.gz"), want: "/cran/src/contrib/PACKAGES.gz"},
		{name: "alpine", entry: metadata("alpine", "alpine/v3.20/main/x86_64/APKINDEX.tar.gz"), want: "/alpine/v3.20/main/x86_64/APKINDEX.tar.gz"},
		{name: "helm", entry: metadata("helm", "helm/index.yaml"), want: "/helm/index.yaml"},
		{name: "huggingface model API", entry: metadata("huggingface", "huggingface/api/models/google/flan-t5-base/tree/main"), want: "/huggingface/api/models/google/flan-t5-base/tree/main"},
		{name: "huggingface dataset API", entry: metadata("huggingface", "huggingface/api/datasets/org/corpus"), want: "/huggingface/api/datasets/org/corpus"},
		{name: "huggingface opaque query cannot be reconstructed", entry: metadata("huggingface", "huggingface/__query__/metadata/abc/api/models/acme/model/tree/main"), wantErr: true},
		{name: "huggingface file is not index metadata", entry: metadata("huggingface", "huggingface/acme/model/resolve/main/config.json"), wantErr: true},
		{name: "apt repeated repo", entry: metadata("apt", "apt/ubuntu/ubuntu/dists/jammy/InRelease"), want: "/apt/ubuntu/dists/jammy/InRelease"},
		{name: "apt normalized legacy key", entry: metadata("apt", "apt/ubuntu/dists/jammy/Release"), wantErr: true},
		{name: "unsupported adapter", entry: metadata("docker", "docker/library/alpine/tags/list"), wantErr: true},
		{name: "artifact", entry: db.CacheEntry{AdapterType: "pypi", CacheKind: db.CacheKindArtifact, Key: "pypi/files/pkg.whl"}, wantErr: true},
		{name: "wrong prefix", entry: metadata("pypi", "extra:private/simple/pkg/index.html"), wantErr: true},
		{name: "invalid go metadata shape", entry: metadata("go", "go/example.com/mod/@v/v1.0.0.zip"), wantErr: true},
		{name: "invalid npm scope", entry: metadata("npm", "npm/@/pkg/metadata.json"), wantErr: true},
		{name: "apt artifact route", entry: metadata("apt", "apt/ubuntu/ubuntu/by-hash/file"), wantErr: true},
		{name: "plain traversal", entry: metadata("go", "go/../escape/@latest"), wantErr: true},
		{name: "encoded traversal", entry: metadata("go", "go/%252e%252e/escape/@latest"), wantErr: true},
		{name: "query injection", entry: metadata("helm", "helm/index.yaml?target=/admin"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CacheIndexPublicPath(tt.entry)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("CacheIndexPublicPath(%+v) = %q, want error", tt.entry, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CacheIndexPublicPath(%+v): %v", tt.entry, err)
			}
			if got != tt.want {
				t.Fatalf("CacheIndexPublicPath(%+v) = %q, want %q", tt.entry, got, tt.want)
			}
		})
	}
}

func TestRefreshIndexValidationAndResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	entries := []db.CacheEntry{
		{Key: "pypi/simple/av/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata, PackageName: "av"},
		{Key: "pypi/files/av.whl", AdapterType: "pypi", CacheKind: db.CacheKindArtifact, PackageName: "av"},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatal(err)
	}

	h := NewCacheHandler(database, nil, 1)
	router := gin.New()
	router.POST("/cache/indexes/:id/refresh", h.RefreshIndex)
	request := func(id string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/cache/indexes/"+id+"/refresh", nil))
		return recorder
	}

	tests := []struct {
		name string
		id   string
		want int
		code string
	}{
		{name: "malformed id", id: "bad", want: http.StatusBadRequest, code: "BAD_REQUEST"},
		{name: "zero id", id: "0", want: http.StatusBadRequest, code: "BAD_REQUEST"},
		{name: "missing entry", id: "99999", want: http.StatusNotFound, code: "NOT_FOUND"},
		{name: "artifact", id: strconv.FormatUint(uint64(entries[1].ID), 10), want: http.StatusUnprocessableEntity, code: "NOT_REFRESHABLE"},
		{name: "callback unavailable", id: strconv.FormatUint(uint64(entries[0].ID), 10), want: http.StatusServiceUnavailable, code: "INDEX_REFRESH_UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := request(tt.id)
			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, tt.want, recorder.Body.String())
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Code != tt.code {
				t.Fatalf("code = %q, want %q; body = %s", body.Code, tt.code, recorder.Body.String())
			}
		})
	}

	refreshErr := errors.New("https://alice:secret@example.test/private?token=hidden")
	h.SetIndexRefresher(func(context.Context, db.CacheEntry) (upstreamupdates.RefreshOutcome, error) {
		return upstreamupdates.RefreshOutcome{}, refreshErr
	})
	recorder := request(strconv.FormatUint(uint64(entries[0].ID), 10))
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("failed refresh status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var failure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code != "INDEX_REFRESH_FAILED" || failure.Message != "index refresh failed; inspect server logs" {
		t.Fatalf("failed refresh response = %+v", failure)
	}
	for _, secret := range []string{"alice", "secret", "private", "token", "hidden"} {
		if strings.Contains(recorder.Body.String(), secret) {
			t.Fatalf("failed refresh response disclosed %q: %s", secret, recorder.Body.String())
		}
	}

	var refreshed db.CacheEntry
	h.SetIndexRefresher(func(ctx context.Context, entry db.CacheEntry) (upstreamupdates.RefreshOutcome, error) {
		if ctx == nil {
			t.Fatal("refresh context is nil")
		}
		refreshed = entry
		return upstreamupdates.RefreshOutcome{}, nil
	})
	recorder = request(strconv.FormatUint(uint64(entries[0].ID), 10))
	if recorder.Code != http.StatusOK {
		t.Fatalf("successful refresh status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if refreshed.ID != entries[0].ID || refreshed.Key != entries[0].Key {
		t.Fatalf("refresher entry = %+v, want ID %d key %q", refreshed, entries[0].ID, entries[0].Key)
	}
	var success struct {
		ID      uint   `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &success); err != nil {
		t.Fatal(err)
	}
	if success.ID != entries[0].ID || success.Message != "index refreshed" {
		t.Fatalf("successful refresh response = %+v", success)
	}
}
