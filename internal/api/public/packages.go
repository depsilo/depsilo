package public

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depslio/internal/db"
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
	type packageItem struct {
		PackageName  string `json:"package_name"`
		AdapterType  string `json:"adapter_type"`
		VersionCount int64  `json:"version_count"`
		TotalSize    int64  `json:"total_size"`
		TotalHits    int64  `json:"total_hits"`
		LastAccessed string `json:"last_accessed"`
	}

	var items []packageItem
	offset := (page - 1) * perPage

	baseQuery.Session(&gorm.Session{}).
		Select("package_name, adapter_type, COUNT(*) as version_count, SUM(size) as total_size, SUM(hit_count) as total_hits, MAX(last_accessed) as last_accessed").
		Group("package_name, adapter_type").
		Order(orderClause).
		Offset(offset).
		Limit(perPage).
		Scan(&items)

	if items == nil {
		items = []packageItem{}
	}

	totalPages := (total + int64(perPage) - 1) / int64(perPage)

	c.JSON(http.StatusOK, gin.H{
		"total":       total,
		"page":        page,
		"per_page":    perPage,
		"total_pages": totalPages,
		"items":       items,
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
	versions := make([]gin.H, 0, len(entries))

	for _, e := range entries {
		totalHits += e.HitCount
		totalSize += e.Size

		// Extract filename from key (last segment after /)
		fileName := e.Key
		if idx := strings.LastIndex(e.Key, "/"); idx >= 0 {
			fileName = e.Key[idx+1:]
		}

		versions = append(versions, gin.H{
			"file_name":     fileName,
			"size":          e.Size,
			"hit_count":     e.HitCount,
			"cached_at":     e.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			"last_accessed": e.LastAccessed.Format("2006-01-02T15:04:05Z07:00"),
			"content_type":  e.ContentType,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"name":         name,
		"adapter_type": adapterType,
		"versions":     versions,
		"total_hits":   totalHits,
		"total_size":   totalSize,
	})
}
