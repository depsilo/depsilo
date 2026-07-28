package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
)

type dashboardTopPackage struct {
	Name     string `json:"name"`
	HitCount int64  `json:"hit_count"`
}

type dashboardTopPackageFixture struct {
	Ecosystem string
	Name      string
	Requests  int64
}

func TestDashboardTopPackages_GlobalAcrossAllEcosystems(t *testing.T) {
	fixtures := []dashboardTopPackageFixture{
		{Ecosystem: "npm", Name: "react", Requests: 12},
		{Ecosystem: "maven", Name: "junit", Requests: 11},
		{Ecosystem: "pypi", Name: "numpy", Requests: 10},
		{Ecosystem: "apt", Name: "curl", Requests: 9},
		{Ecosystem: "npm", Name: "typescript", Requests: 8},
		{Ecosystem: "maven", Name: "guava", Requests: 7},
		{Ecosystem: "cargo", Name: "serde", Requests: 6},
		{Ecosystem: "go", Name: "gin", Requests: 5},
		{Ecosystem: "rubygems", Name: "rails", Requests: 4},
		{Ecosystem: "composer", Name: "laravel", Requests: 3},
		{Ecosystem: "nuget", Name: "newtonsoft-json", Requests: 2},
		{Ecosystem: "conda", Name: "pandas", Requests: 1},
	}

	for _, useRollup := range []bool{false, true} {
		t.Run(fmt.Sprintf("useRollup=%t", useRollup), func(t *testing.T) {
			database, router := newDashboardResponseTestServer(t, useRollup, 0)
			insertDashboardTopPackageFixtures(t, database, useRollup, fixtures)

			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}

			var response struct {
				TopPackages map[string][]dashboardTopPackage `json:"top_packages"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("decode dashboard response: %v", err)
			}

			got := make(map[string]int64)
			total := 0
			for ecosystem, packages := range response.TopPackages {
				for i, pkg := range packages {
					if pkg.Name == "" {
						t.Fatalf("top_packages[%q] contains an empty package name", ecosystem)
					}
					if i > 0 && packages[i-1].HitCount < pkg.HitCount {
						t.Fatalf("top_packages[%q] is not descending: %#v", ecosystem, packages)
					}
					got[ecosystem+"/"+pkg.Name] = pkg.HitCount
					total++
				}
			}
			if total != 10 {
				t.Fatalf("global top package count = %d, want 10; response = %#v", total, response.TopPackages)
			}

			for _, fixture := range fixtures[:10] {
				key := fixture.Ecosystem + "/" + fixture.Name
				if got[key] != fixture.Requests {
					t.Errorf("%s request count = %d, want %d", key, got[key], fixture.Requests)
				}
			}
			for _, fixture := range fixtures[10:] {
				key := fixture.Ecosystem + "/" + fixture.Name
				if _, ok := got[key]; ok {
					t.Errorf("%s should be outside the global top 10", key)
				}
			}
		})
	}
}

func TestDashboardCacheUsagePercentIsOptional(t *testing.T) {
	const gib = int64(1024 * 1024 * 1024)
	t.Run("reports percentage from cache metadata", func(t *testing.T) {
		database, router := newDashboardResponseTestServer(t, false, 20)
		entries := []db.CacheEntry{
			{Key: "npm/a", Size: 2 * gib},
			{Key: "maven/b", Size: 3 * gib},
		}
		if err := database.Create(&entries).Error; err != nil {
			t.Fatalf("create cache entries: %v", err)
		}

		response := getDashboardCacheUsageResponse(t, router)
		if response.CacheUsagePercent == nil || *response.CacheUsagePercent != 25 {
			t.Fatalf("cache_usage_percent = %v, want 25", response.CacheUsagePercent)
		}
	})

	t.Run("omits percentage for non-positive maximum", func(t *testing.T) {
		_, router := newDashboardResponseTestServer(t, false, 0)
		response := getDashboardCacheUsageResponse(t, router)
		if response.CacheUsagePercent != nil {
			t.Fatalf("cache_usage_percent = %v, want omitted", *response.CacheUsagePercent)
		}
	})

	t.Run("omits percentage when metadata query fails", func(t *testing.T) {
		database, router := newDashboardResponseTestServer(t, false, 20)
		if err := database.Migrator().DropTable(&db.CacheEntry{}); err != nil {
			t.Fatalf("drop cache_entries: %v", err)
		}
		response := getDashboardCacheUsageResponse(t, router)
		if response.CacheUsagePercent != nil {
			t.Fatalf("cache_usage_percent = %v, want omitted", *response.CacheUsagePercent)
		}
	})
}

func getDashboardCacheUsageResponse(t *testing.T, router http.Handler) struct {
	CacheUsagePercent *float64 `json:"cache_usage_percent"`
} {
	t.Helper()

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		CacheUsagePercent *float64 `json:"cache_usage_percent"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode dashboard response: %v", err)
	}
	return response
}

func newDashboardResponseTestServer(t *testing.T, useRollup bool, maxSizeGB int) (*gorm.DB, http.Handler) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "dashboard-top-packages.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open dashboard database: %v", err)
	}
	if err := database.AutoMigrate(
		&db.CacheEntry{},
		&db.AccessLog{},
		&db.AccessLogHourly{},
		&db.AccessLogPackageDaily{},
	); err != nil {
		t.Fatalf("migrate dashboard database: %v", err)
	}

	handler := NewDashboardHandler(database, nil, nil, useRollup, maxSizeGB)
	router := gin.New()
	router.GET("/dashboard", handler.GetDashboard)
	return database, router
}

func insertDashboardTopPackageFixtures(t *testing.T, database *gorm.DB, useRollup bool, fixtures []dashboardTopPackageFixture) {
	t.Helper()

	if useRollup {
		rows := make([]db.AccessLogPackageDaily, 0, len(fixtures)*2+1)
		for _, fixture := range fixtures {
			rows = append(rows,
				db.AccessLogPackageDaily{
					Date:         "2026-07-27",
					AdapterType:  fixture.Ecosystem,
					PackageName:  fixture.Name,
					Hit:          false,
					RequestCount: fixture.Requests - 1,
				},
				db.AccessLogPackageDaily{
					Date:         "2026-07-28",
					AdapterType:  fixture.Ecosystem,
					PackageName:  fixture.Name,
					Hit:          true,
					RequestCount: 1,
				},
			)
		}
		rows = append(rows, db.AccessLogPackageDaily{
			Date:         "2026-07-28",
			AdapterType:  "npm",
			PackageName:  "",
			RequestCount: 100,
		})
		if err := database.Create(&rows).Error; err != nil {
			t.Fatalf("create package rollups: %v", err)
		}
		return
	}

	now := time.Now().UTC()
	rows := make([]db.AccessLog, 0, 178)
	for _, fixture := range fixtures {
		for range fixture.Requests {
			rows = append(rows, db.AccessLog{
				AdapterType: fixture.Ecosystem,
				PackageName: fixture.Name,
				CreatedAt:   now,
			})
		}
	}
	for range 100 {
		rows = append(rows, db.AccessLog{
			AdapterType: "npm",
			PackageName: "",
			CreatedAt:   now,
		})
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatalf("create raw access logs: %v", err)
	}
}
