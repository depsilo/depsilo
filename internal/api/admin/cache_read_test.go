package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"depsilo/internal/db"
	"depsilo/internal/upstreamupdates"
	"github.com/gin-gonic/gin"
)

func TestCacheReadFailuresDoNotBecomeEmptyInventory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	// No cache table: both read interfaces must preserve the database failure.
	h := NewCacheHandler(database, nil, 20)
	router := gin.New()
	router.GET("/cache", h.List)
	router.GET("/distribution", h.GetDistribution)
	for _, path := range []string{"/cache", "/distribution"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusInternalServerError || !strings.Contains(recorder.Body.String(), `"code":"DB_ERROR"`) {
			t.Fatalf("%s returned %d: %s", path, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "no such table") {
			t.Fatal("database detail leaked")
		}
	}
}

func TestCacheInventoryFiltersByPersistedKind(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	rows := []db.CacheEntry{{Key: "pypi/files/six.whl", AdapterType: "pypi", CacheKind: db.CacheKindArtifact}, {Key: "pypi/simple/six/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata}}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.GET("/cache", NewCacheHandler(database, nil, 20).List)
	for _, kind := range []string{"artifact", "metadata"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/cache?cache_kind="+kind, nil))
		var response struct {
			Total int             `json:"total"`
			Items []db.CacheEntry `json:"items"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 200 || response.Total != 1 || len(response.Items) != 1 || string(response.Items[0].CacheKind) != kind {
			t.Fatalf("unexpected %s inventory: %s", kind, recorder.Body.String())
		}
	}
}

func TestCacheIndexesDescribeRefreshCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	rows := []db.CacheEntry{
		{Key: "pypi/simple/six/index.html", AdapterType: "pypi", CacheKind: db.CacheKindMetadata},
		{Key: "custom/metadata", AdapterType: "custom", CacheKind: db.CacheKindMetadata},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	h := NewCacheHandler(database, nil, 20)
	router := gin.New()
	router.GET("/indexes", h.ListIndexes)
	for _, configured := range []bool{false, true} {
		if configured {
			h.SetIndexRefresher(func(context.Context, db.CacheEntry) (upstreamupdates.RefreshOutcome, error) {
				return upstreamupdates.RefreshOutcome{}, nil
			})
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/indexes", nil))
		var response cacheIndexesTestResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 200 || len(response.Items) != 2 {
			t.Fatalf("unexpected response %d %s", recorder.Code, recorder.Body.String())
		}
		for _, item := range response.Items {
			if want := configured && item.AdapterType == "pypi"; item.RefreshSupported != want {
				t.Fatalf("configured=%v adapter=%s refresh=%v", configured, item.AdapterType, item.RefreshSupported)
			}
		}
	}
}
