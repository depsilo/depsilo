package middleware

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"depsilo/internal/db"
	"depsilo/internal/ecosystem"
)

func TestInferEcosystemAndKeyUsesCatalogRoutes(t *testing.T) {
	for _, definition := range ecosystem.All() {
		t.Run(definition.Name, func(t *testing.T) {
			name, key := inferEcosystemAndKey(definition.Route + "/package/file")
			if name != definition.Name {
				t.Fatalf("ecosystem = %q, want %q", name, definition.Name)
			}
			if want := definition.Name + "/package/file"; key != want {
				t.Fatalf("key = %q, want %q", key, want)
			}
		})
	}
}

func TestInferEcosystemAndKeyRejectsUnknownRoute(t *testing.T) {
	name, key := inferEcosystemAndKey("/unknown/package/file")
	if name != "" || key != "" {
		t.Fatalf("inferEcosystemAndKey() = (%q, %q), want empty values", name, key)
	}
}

func TestRecordProjectDownloadCompletesBeforeReturn(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "project.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&db.ProjectPackage{}); err != nil {
		t.Fatalf("migrate project packages: %v", err)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/pypi/files/requests-2.32.3-py3-none-any.whl", nil)
	context.Status(http.StatusOK)

	recordProjectDownload(database, 42, context)
	recordProjectDownload(database, 42, context)

	var got db.ProjectPackage
	if err := database.First(&got).Error; err != nil {
		t.Fatalf("load project package immediately after record: %v", err)
	}
	if got.ProjectID != 42 || got.Ecosystem != "pypi" || got.PackageName != "requests" || got.Version != "2.32.3" {
		t.Fatalf("project package = %#v", got)
	}
	if got.DownloadCount != 2 {
		t.Fatalf("download count = %d, want 2", got.DownloadCount)
	}
}
