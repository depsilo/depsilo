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

func TestRecordProjectDownloadPreservesEncodedHuggingFaceRevisionAndSkipsHEAD(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "project-hf.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.ProjectPackage{}); err != nil {
		t.Fatal(err)
	}

	record := func(method string) {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(
			method,
			"/huggingface/acme/model/resolve/refs%2Fpr%2F1/model.bin",
			nil,
		)
		context.Status(http.StatusOK)
		recordProjectDownload(database, 7, context)
	}
	record(http.MethodHead)
	record(http.MethodGet)

	var packages []db.ProjectPackage
	if err := database.Find(&packages).Error; err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 {
		t.Fatalf("project packages = %d, want 1", len(packages))
	}
	if got := packages[0]; got.PackageName != "acme/model" ||
		got.Version != "refs/pr/1" || got.DownloadCount != 1 {
		t.Fatalf("Hugging Face project package = %#v", got)
	}
}

func TestRecordProjectDownloadUsesResolvedHuggingFaceCommit(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "project-hf-commit.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.ProjectPackage{}); err != nil {
		t.Fatal(err)
	}
	const commit = "0123456789abcdef0123456789abcdef01234567"
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodGet,
		"/huggingface/acme/model/resolve/main/model.bin",
		nil,
	)
	context.Header("X-Repo-Commit", commit)
	context.Status(http.StatusOK)
	recordProjectDownload(database, 8, context)

	var got db.ProjectPackage
	if err := database.First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.PackageName != "acme/model" || got.Version != commit {
		t.Fatalf("project package = %#v", got)
	}
}

func TestRecordProjectDownloadTracksRangeOnlyArtifactWithoutCountingEverySegment(t *testing.T) {
	database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "project-range.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.ProjectPackage{}); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(
			http.MethodGet,
			"/pypi/files/requests-2.32.3-py3-none-any.whl",
			nil,
		)
		context.Status(http.StatusPartialContent)
		recordProjectDownload(database, 9, context)
	}

	var got db.ProjectPackage
	if err := database.First(&got).Error; err != nil {
		t.Fatal(err)
	}
	if got.PackageName != "requests" || got.Version != "2.32.3" {
		t.Fatalf("project package = %#v", got)
	}
	if got.DownloadCount != 1 {
		t.Fatalf("range-only download count = %d, want 1", got.DownloadCount)
	}
}

func TestProjectScopedRequestWithTokenIsRecordedOnlyForSlugProject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		tokenUsesSlugProj bool
	}{
		{name: "same project token", tokenUsesSlugProj: true},
		{name: "different project token", tokenUsesSlugProj: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "project-routing.db")), &gorm.Config{
				Logger: logger.Default.LogMode(logger.Silent),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := database.AutoMigrate(&db.Project{}, &db.ProjectPackage{}); err != nil {
				t.Fatal(err)
			}

			const (
				slugToken  = "depsilo_proj_slug"
				otherToken = "depsilo_proj_other"
			)
			slugProject := db.Project{
				Name:      "Slug Project",
				Slug:      "slug-project",
				TokenHash: HashProjectToken(slugToken),
			}
			if err := database.Create(&slugProject).Error; err != nil {
				t.Fatal(err)
			}
			otherProject := db.Project{
				Name:      "Other Project",
				Slug:      "other-project",
				TokenHash: HashProjectToken(otherToken),
			}
			if err := database.Create(&otherProject).Error; err != nil {
				t.Fatal(err)
			}

			token := otherToken
			if test.tokenUsesSlugProj {
				token = slugToken
			}

			router := gin.New()
			router.Use(ProjectTokenMiddleware(database))
			project := router.Group("/p/:slug")
			project.Use(ProjectSlugMiddleware(database))
			project.GET("/pypi/files/:filename", func(c *gin.Context) {
				projectID, ok := c.Get(ProjectIDKey)
				if !ok || projectID != slugProject.ID {
					t.Errorf("downstream project_id = (%v, %v), want %d", projectID, ok, slugProject.ID)
				}
				c.String(http.StatusOK, "artifact bytes")
			})

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodGet,
				"/p/slug-project/pypi/files/requests-2.32.3-py3-none-any.whl",
				nil,
			)
			request.Header.Set("Authorization", "Bearer "+token)
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("response = (%d, %q)", recorder.Code, recorder.Body.String())
			}

			var packages []db.ProjectPackage
			if err := database.Order("project_id").Find(&packages).Error; err != nil {
				t.Fatal(err)
			}
			if len(packages) != 1 {
				t.Fatalf("project package rows = %#v, want one slug-project row", packages)
			}
			got := packages[0]
			if got.ProjectID != slugProject.ID || got.DownloadCount != 1 {
				t.Fatalf("project package = %#v, want slug project %d counted once", got, slugProject.ID)
			}
		})
	}
}
