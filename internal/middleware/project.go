package middleware

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/db"
	ecosystemcatalog "depsilo/internal/ecosystem"
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
		originalEscapedPath := c.Request.URL.EscapedPath()
		prefix := "/p/" + slug
		if strings.HasPrefix(originalPath, prefix) {
			c.Request.URL.Path = strings.TrimPrefix(originalPath, prefix)
			if c.Request.URL.Path == "" {
				c.Request.URL.Path = "/"
			}
			if c.Request.URL.RawPath != "" {
				escapedPrefix := "/p/" + url.PathEscape(slug)
				if strings.HasPrefix(originalEscapedPath, escapedPrefix) {
					c.Request.URL.RawPath = strings.TrimPrefix(originalEscapedPath, escapedPrefix)
					if c.Request.URL.RawPath == "" {
						c.Request.URL.RawPath = "/"
					}
				} else {
					// RawPath is only a hint. Drop it if it cannot be kept
					// consistent with the rewritten decoded Path.
					c.Request.URL.RawPath = ""
				}
			}
		}

		c.Next()

		// After the response: record the package before this request returns.
		recordProjectDownload(database, project.ID, c)
	}
}

// ProjectTokenMiddleware checks Bearer tokens against project tokens.
// Runs on normal (non-/p/) routes. If token matches a project, sets project_id.
// If no token or token doesn't match a project, proceeds without project tracking.
func ProjectTokenMiddleware(database *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// A /p/:slug route has an explicit project owner and is handled by
		// ProjectSlugMiddleware later in the chain. Gin has already populated
		// route params before global middleware runs, so bypass token attribution
		// entirely here; otherwise both middleware post-handlers record the same
		// response (or, with a different token, attribute it to two projects).
		if c.Param("slug") != "" {
			c.Next()
			return
		}

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
	status := c.Writer.Status()
	if (status != http.StatusOK && status != http.StatusPartialContent) ||
		c.Request.Method != http.MethodGet {
		return
	}

	// Determine ecosystem and cache key from the request path
	path := c.Request.URL.EscapedPath()
	ecosystem, cacheKey := inferEcosystemAndKey(path)
	if ecosystem == "" || cacheKey == "" {
		return
	}

	if !packagekey.IsPackageFile(ecosystem, cacheKey) {
		return
	}

	name := packagekey.ExtractName(ecosystem, cacheKey)
	version := packagekey.ExtractVersion(ecosystem, cacheKey)
	if ecosystem == "huggingface" {
		if commit := c.Writer.Header().Get("X-Repo-Commit"); isCanonicalCommit(commit) {
			version = commit
		}
	}
	if name == "" {
		return
	}

	// This runs after the response has been produced, but remains inside the
	// request handler. Keeping the upsert synchronous here bounds concurrency
	// to the HTTP server and lets graceful shutdown wait before closing the DB.
	now := time.Now()
	updates := map[string]interface{}{
		"last_seen_at": now,
		"updated_at":   now,
	}
	// A ranged transfer can consist of many 206 requests. It still establishes
	// project ownership, but counting every segment as another download would
	// inflate usage. A complete 200 response retains the historical per-request
	// download count.
	if status == http.StatusOK {
		updates["download_count"] = gorm.Expr("download_count + 1")
	}
	result := database.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "project_id"}, {Name: "ecosystem"},
			{Name: "package_name"}, {Name: "version"},
		},
		DoUpdates: clause.Assignments(updates),
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
}

func isCanonicalCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for i := range value {
		if (value[i] < '0' || value[i] > '9') && (value[i] < 'a' || value[i] > 'f') {
			return false
		}
	}
	return true
}

// inferEcosystemAndKey derives the ecosystem type and a pseudo cache key from a request path.
func inferEcosystemAndKey(path string) (string, string) {
	for _, definition := range ecosystemcatalog.All() {
		prefix := definition.Route + "/"
		if strings.HasPrefix(path, prefix) {
			// Build a cache key similar to what the adapter would produce
			rest := strings.TrimPrefix(path, prefix)
			return definition.Name, definition.Name + "/" + rest
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
