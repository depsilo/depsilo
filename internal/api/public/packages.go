package public

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

type PackagesHandler struct {
	db *gorm.DB
}

func NewPackagesHandler(database *gorm.DB) *PackagesHandler {
	return &PackagesHandler{db: database}
}

func (h *PackagesHandler) List(c *gin.Context) {
	q := c.Query("q")
	adapterType := c.Query("type")
	sort := c.DefaultQuery("sort", "total_hits")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))

	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}

	// Build base query conditions
	baseQuery := h.db.Model(&db.CacheEntry{}).Where("package_name != ''")
	if q != "" {
		baseQuery = baseQuery.Where("package_name LIKE ?", "%"+q+"%")
	}
	if adapterType != "" {
		baseQuery = baseQuery.Where("adapter_type = ?", adapterType)
	}

	// Count total distinct packages
	var total int64
	baseQuery.Session(&gorm.Session{}).
		Select("COUNT(DISTINCT(package_name || '|' || adapter_type))").
		Scan(&total)

	// Determine sort order
	orderClause := "total_hits DESC"
	switch sort {
	case "hits", "total_hits":
		orderClause = "total_hits DESC"
	case "recent", "last_accessed":
		orderClause = "last_accessed DESC"
	case "size", "total_size":
		orderClause = "total_size DESC"
	case "name":
		orderClause = "package_name ASC"
	}

	// Aggregation query
	type packageRow struct {
		PackageName  string
		AdapterType  string
		VersionCount int64
		TotalSize    int64
		TotalHits    int64
		LastAccessed string
	}

	var rows []packageRow
	offset := (page - 1) * perPage

	baseQuery.Session(&gorm.Session{}).
		Select("package_name, adapter_type, COUNT(*) as version_count, SUM(size) as total_size, SUM(hit_count) as total_hits, MAX(last_accessed) as last_accessed").
		Group("package_name, adapter_type").
		Order(orderClause).
		Offset(offset).
		Limit(perPage).
		Scan(&rows)

	items := make([]contract.PublicPackageSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, contract.PublicPackageSummary{
			PackageName: row.PackageName, AdapterType: row.AdapterType, VersionCount: row.VersionCount,
			TotalSize: row.TotalSize, TotalHits: row.TotalHits, LastAccessed: row.LastAccessed,
		})
	}

	totalPages := (total + int64(perPage) - 1) / int64(perPage)

	c.JSON(http.StatusOK, contract.PublicPackageListResponse{
		Total: total, Page: page, PerPage: perPage, TotalPages: totalPages, Items: items,
	})
}

func (h *PackagesHandler) Detail(c *gin.Context) {
	adapterType := c.Param("type")
	name := c.Param("name")

	var entries []db.CacheEntry
	h.db.Where("adapter_type = ? AND package_name = ?", adapterType, name).
		Order("hit_count DESC").
		Find(&entries)

	if len(entries) == 0 {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    "NOT_FOUND",
			"message": "package not found",
		})
		return
	}

	var totalHits int64
	var totalSize int64
	versions := make([]contract.PublicPackageFile, 0, len(entries))

	for _, e := range entries {
		totalHits += e.HitCount
		totalSize += e.Size

		// Extract filename from key (last segment after /)
		fileName := e.Key
		if idx := strings.LastIndex(e.Key, "/"); idx >= 0 {
			fileName = e.Key[idx+1:]
		}

		versions = append(versions, contract.PublicPackageFile{
			FileName:     fileName,
			Size:         e.Size,
			HitCount:     e.HitCount,
			CachedAt:     e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			LastAccessed: e.LastAccessed.Format("2006-01-02T15:04:05Z07:00"),
			ContentType:  e.ContentType,
		})
	}

	c.JSON(http.StatusOK, contract.PublicPackageDetail{
		Name: name, AdapterType: adapterType, Versions: versions, TotalHits: totalHits, TotalSize: totalSize,
	})
}
