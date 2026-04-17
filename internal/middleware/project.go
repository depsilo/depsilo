package middleware

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/cache"
	"depsilo/internal/db"
)

const ProjectIDKey = "project_id"

// ProjectSlugMiddleware extracts project from URL path /p/:slug/...
// and records package downloads for the project.
func ProjectSlugMiddleware(database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		slug := c.Param("slug")
		if slug == "" {
			c.Next()
			return
		}

		var project db.Project
		if err := database.Where("slug = ?", slug).First(&project).Error; err != nil {
			c.JSON(404, gin.H{"code": "PROJECT_NOT_FOUND", "message": "project not found"})
			c.Abort()
			return
		}

		c.Set(ProjectIDKey, project.ID)

		// Strip /p/{slug} prefix from URL for downstream handlers
		originalPath := c.Request.URL.Path
		prefix := "/p/" + slug
		if strings.HasPrefix(originalPath, prefix) {
			c.Request.URL.Path = strings.TrimPrefix(originalPath, prefix)
			if c.Request.URL.Path == "" {
				c.Request.URL.Path = "/"
			}
		}

		c.Next()

		// After response: record package download asynchronously
		recordProjectDownload(database, project.ID, c)
	}
}

// ProjectTokenMiddleware checks Bearer tokens against project tokens.
// Runs on normal (non-/p/) routes. If token matches a project, sets project_id.
// If no token or token doesn't match a project, proceeds without project tracking.
func ProjectTokenMiddleware(database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer depsilo_proj_") {
			c.Next()
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		hash := hashToken(token)

		var project db.Project
		if err := database.Where("token_hash = ?", hash).First(&project).Error; err != nil {
			// Token doesn't match any project — proceed without tracking
			c.Next()
			return
		}

		c.Set(ProjectIDKey, project.ID)
		c.Next()

		// After response: record package download asynchronously
		recordProjectDownload(database, project.ID, c)
	}
}

// recordProjectDownload records a package download for a project.
// Only records actual package files (not metadata).
func recordProjectDownload(database *gorm.DB, projectID uint, c *gin.Context) {
	if c.Writer.Status() != 200 {
		return
	}

	// Determine ecosystem and cache key from the request path
	path := c.Request.URL.Path
	ecosystem, cacheKey := inferEcosystemAndKey(path)
	if ecosystem == "" || cacheKey == "" {
		return
	}

	if !cache.IsPackageFile(ecosystem, cacheKey) {
		return
	}

	name := cache.ExtractPackageName(ecosystem, cacheKey)
	version := cache.ExtractPackageVersion(ecosystem, cacheKey)
	if name == "" {
		return
	}

	now := time.Now()
	go func() {
		result := database.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "project_id"}, {Name: "ecosystem"},
				{Name: "package_name"}, {Name: "version"},
			},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"last_seen_at":   now,
				"download_count": gorm.Expr("download_count + 1"),
				"updated_at":     now,
			}),
		}).Create(&db.ProjectPackage{
			ProjectID:     projectID,
			Ecosystem:     ecosystem,
			PackageName:   name,
			Version:       version,
			FirstSeenAt:   now,
			LastSeenAt:    now,
			DownloadCount: 1,
		})
		if result.Error != nil {
			zap.L().Debug("failed to record project download", zap.Error(result.Error))
		}
	}()
}

// inferEcosystemAndKey derives the ecosystem type and a pseudo cache key from a request path.
func inferEcosystemAndKey(path string) (string, string) {
	prefixes := map[string]string{
		"/pypi/":     "pypi",
		"/npm/":      "npm",
		"/apt/":      "apt",
		"/go/":       "go",
		"/crates/":   "cargo",
		"/maven/":    "maven",
		"/rubygems/": "rubygems",
		"/composer/": "composer",
		"/nuget/":    "nuget",
		"/conda/":    "conda",
		"/cran/":     "cran",
		"/helm/":     "helm",
		"/v2/":       "docker",
	}

	for prefix, eco := range prefixes {
		if strings.HasPrefix(path, prefix) {
			// Build a cache key similar to what the adapter would produce
			rest := strings.TrimPrefix(path, prefix)
			return eco, eco + "/" + rest
		}
	}
	return "", ""
}

// HashProjectToken hashes a project token for storage.
func HashProjectToken(token string) string {
	return hashToken(token)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
}
