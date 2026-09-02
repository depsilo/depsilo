package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/upstreamupdates"
)

type CacheHandler struct {
	db             *gorm.DB
	retention      *cache.Retention
	maxSizeGB      int
	indexRefresher upstreamupdates.Refresher
}

type cacheIndexItem struct {
	ID           uint      `json:"id"`
	Key          string    `json:"key"`
	AdapterType  string    `json:"adapter_type"`
	PackageName  string    `json:"package_name"`
	Size         int64     `json:"size"`
	HitCount     int64     `json:"hit_count"`
	ETag         string    `json:"etag"`
	LastModified string    `json:"last_modified"`
	LastAccessed time.Time `json:"last_accessed"`
	ExpiresAt    time.Time `json:"expires_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Status       string    `json:"status"`
}

type cacheIndexSummary struct {
	AdapterType string  `json:"adapter_type"`
	Total       int64   `json:"total"`
	Fresh       int64   `json:"fresh"`
	Stale       int64   `json:"stale"`
	LastUpdated *string `json:"last_updated"`
}

// ListIndexes returns mutable package-manager indexes separately from cached
// artifacts. cache_kind is persisted by cache.Manager and legacy rows are
// classified during migration, so this query does not depend on key guessing.
func (h *CacheHandler) ListIndexes(c *gin.Context) {
	page := parseIntParam(c, "page", 1, 1, 100000)
	pageSize := parseIntParam(c, "page_size", 50, 1, 200)
	search := strings.TrimSpace(c.Query("search"))
	adapterType := strings.TrimSpace(c.Query("adapter_type"))
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "fresh" && status != "stale" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "status must be fresh or stale"})
		return
	}

	now := time.Now().UTC()
	query := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{}).
		Where("cache_kind = ?", db.CacheKindMetadata).
		Where("NOT (adapter_type = ? AND key LIKE ?)", "huggingface", "huggingface/__query__/%")
	if search != "" {
		like := "%" + search + "%"
		query = query.Where("package_name LIKE ? OR key LIKE ?", like, like)
	}
	if adapterType != "" && adapterType != "all" {
		query = query.Where("adapter_type = ?", adapterType)
	}
	if status == "fresh" {
		query = query.Where("expires_at > ?", now)
	} else if status == "stale" {
		query = query.Where("expires_at <= ?", now)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	var rows []db.CacheEntry
	if err := query.Order("adapter_type ASC, package_name ASC, last_accessed DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	items := make([]cacheIndexItem, len(rows))
	for i, row := range rows {
		rowStatus := "stale"
		if row.ExpiresAt.After(now) {
			rowStatus = "fresh"
		}
		items[i] = cacheIndexItem{
			ID: row.ID, Key: row.Key, AdapterType: row.AdapterType,
			PackageName: row.PackageName, Size: row.Size, HitCount: row.HitCount,
			ETag: row.ETag, LastModified: row.LastModified,
			LastAccessed: row.LastAccessed, ExpiresAt: row.ExpiresAt,
			UpdatedAt: row.UpdatedAt, Status: rowStatus,
		}
	}

	var summary []cacheIndexSummary
	if err := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{}).
		Select(`adapter_type, COUNT(*) AS total,
			SUM(CASE WHEN expires_at > ? THEN 1 ELSE 0 END) AS fresh,
			SUM(CASE WHEN expires_at <= ? THEN 1 ELSE 0 END) AS stale,
			MAX(updated_at) AS last_updated`, now, now).
		Where("cache_kind = ?", db.CacheKindMetadata).
		Where("NOT (adapter_type = ? AND key LIKE ?)", "huggingface", "huggingface/__query__/%").
		Group("adapter_type").Order("adapter_type ASC").Scan(&summary).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items, "total": total, "page": page, "page_size": pageSize,
		"summary": summary,
	})
}

func NewCacheHandler(database *gorm.DB, retention *cache.Retention, maxSizeGB int) *CacheHandler {
	return &CacheHandler{db: database, retention: retention, maxSizeGB: maxSizeGB}
}

// SetIndexRefresher configures the callback used by RefreshIndex. It is
// expected to be called once while routes are being assembled.
func (h *CacheHandler) SetIndexRefresher(refresher upstreamupdates.Refresher) {
	h.indexRefresher = refresher
}

// RefreshIndex refreshes a single mutable package index immediately.
func (h *CacheHandler) RefreshIndex(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var entry db.CacheEntry
	if err := h.db.WithContext(c.Request.Context()).First(&entry, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "cache entry not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	if entry.CacheKind != db.CacheKindMetadata {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code": "NOT_REFRESHABLE", "message": "only index metadata can be refreshed",
		})
		return
	}
	if h.indexRefresher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "INDEX_REFRESH_UNAVAILABLE", "message": "index refresh is not configured",
		})
		return
	}
	if _, err := h.indexRefresher(c.Request.Context(), entry); err != nil {
		zap.L().Warn("manual index refresh failed",
			zap.Uint("cache_entry_id", entry.ID),
			zap.String("adapter_type", entry.AdapterType),
			zap.String("error_type", fmt.Sprintf("%T", err)),
		)
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "INDEX_REFRESH_FAILED", "message": "index refresh failed; inspect server logs",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "index refreshed", "id": entry.ID})
}

// CacheIndexPublicPath maps a persisted cache key back to the public GET route
// that originally populated it. Only standard mutable index shapes are
// accepted; artifacts, custom adapter IDs and unsafe paths are rejected.
func CacheIndexPublicPath(entry db.CacheEntry) (string, error) {
	if entry.CacheKind != db.CacheKindMetadata {
		return "", fmt.Errorf("cache entry %d is not index metadata", entry.ID)
	}

	var publicPath string

	switch entry.AdapterType {
	case "pypi":
		const prefix, suffix = "pypi/simple/", "/index.html"
		if !strings.HasPrefix(entry.Key, prefix) || !strings.HasSuffix(entry.Key, suffix) {
			return "", unsupportedIndexKey(entry)
		}
		pkg := strings.TrimSuffix(strings.TrimPrefix(entry.Key, prefix), suffix)
		if pkg == "" || strings.Contains(pkg, "/") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/pypi/simple/" + pkg + "/"

	case "npm":
		const suffix = "/metadata.json"
		prefix := "npm/"
		if strings.HasPrefix(entry.Key, packagekey.NPMExactIdentityCachePrefix) {
			prefix = packagekey.NPMExactIdentityCachePrefix
		}
		if !strings.HasPrefix(entry.Key, prefix) || !strings.HasSuffix(entry.Key, suffix) {
			return "", unsupportedIndexKey(entry)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(entry.Key, prefix), suffix)
		parts := strings.Split(name, "/")
		validScopedName := len(parts) == 2 && len(parts[0]) > 1 &&
			strings.HasPrefix(parts[0], "@") && parts[1] != ""
		if name == "" || (len(parts) != 1 && !validScopedName) {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/npm/" + name

	case "go":
		rest, ok := trimCachePrefix(entry.Key, "go/")
		if !ok {
			return "", unsupportedIndexKey(entry)
		}
		if !strings.HasSuffix(rest, "/@v/list") && !strings.HasSuffix(rest, "/@latest") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/go/" + rest

	case "cargo":
		switch {
		case entry.Key == "cargo/config.json":
			publicPath = "/crates/config.json"
		case strings.HasPrefix(entry.Key, "cargo/index/"):
			rest, ok := trimCachePrefix(entry.Key, "cargo/index/")
			if !ok {
				return "", unsupportedIndexKey(entry)
			}
			publicPath = "/crates/" + rest
		default:
			return "", unsupportedIndexKey(entry)
		}

	case "maven", "rubygems", "composer", "nuget", "conda", "cran", "alpine", "helm":
		prefix := entry.AdapterType + "/"
		rest, ok := trimCachePrefix(entry.Key, prefix)
		if !ok {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/" + entry.AdapterType + "/" + rest

	case "huggingface":
		rest, ok := trimCachePrefix(entry.Key, "huggingface/")
		if !ok ||
			(!strings.HasPrefix(rest, "api/models/") && !strings.HasPrefix(rest, "api/datasets/")) ||
			strings.HasPrefix(rest, "__query__/") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/huggingface/" + rest

	case "apt":
		rest, ok := trimCachePrefix(entry.Key, "apt/")
		if !ok {
			return "", unsupportedIndexKey(entry)
		}
		repo, storedPath, ok := strings.Cut(rest, "/")
		if !ok || repo == "" || storedPath == "" {
			return "", unsupportedIndexKey(entry)
		}
		// CacheKey(repo, upstreamPath) stores both repo and the full
		// upstream path, so normal keys contain repo twice.
		if !strings.HasPrefix(storedPath, repo+"/") {
			// Older normalized keys do not round-trip through the current APT
			// handler (which stores the repo twice). Reject them instead of
			// refreshing a different cache row and reporting false success.
			return "", fmt.Errorf("legacy APT index key %q must be re-cached before manual refresh", entry.Key)
		}
		storedPath = strings.TrimPrefix(storedPath, repo+"/")
		if !strings.HasPrefix(storedPath, "dists/") && !strings.HasPrefix(storedPath, "pool/") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/apt/" + repo + "/" + storedPath

	default:
		return "", fmt.Errorf("index refresh is not supported for adapter %q", entry.AdapterType)
	}

	if err := validateInternalProxyPath(publicPath); err != nil {
		return "", fmt.Errorf("unsafe cache index path for %q: %w", entry.Key, err)
	}
	return publicPath, nil
}

func trimCachePrefix(key, prefix string) (string, bool) {
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	return rest, rest != ""
}

func unsupportedIndexKey(entry db.CacheEntry) error {
	return fmt.Errorf("unsupported %s index cache key %q", entry.AdapterType, entry.Key)
}

func validateInternalProxyPath(path string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("invalid absolute path")
	}
	decoded := path
	for {
		if strings.ContainsAny(decoded, "\\?#\x00\r\n") {
			return errors.New("path contains a forbidden character")
		}
		unescaped, err := url.PathUnescape(decoded)
		if err != nil {
			return errors.New("path contains invalid escaping")
		}
		if unescaped == decoded {
			break
		}
		decoded = unescaped
	}
	if strings.Contains(decoded, "//") {
		return errors.New("path contains an empty segment")
	}
	for _, r := range decoded {
		if r < 0x20 || r == 0x7f {
			return errors.New("path contains a control character")
		}
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return errors.New("path traversal is not allowed")
		}
	}
	return nil
}

func (h *CacheHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")
	adapterType := c.Query("adapter_type")
	if adapterType == "" {
		adapterType = c.Query("type")
	}

	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	query := h.db.Model(&db.CacheEntry{})
	if search != "" {
		query = query.Where("key LIKE ?", "%"+search+"%")
	}
	if adapterType != "" {
		query = query.Where("adapter_type = ?", adapterType)
	}

	var total int64
	query.Count(&total)

	var entries []db.CacheEntry
	query.Order("last_accessed DESC").Offset(offset).Limit(pageSize).Find(&entries)

	c.JSON(http.StatusOK, gin.H{
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"items":     entries,
	})
}

func (h *CacheHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	removal, err := h.retention.Remove(c.Request.Context(), uint(id))
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{
			"message":         "deleted",
			"deleted":         1,
			"reclaimed_bytes": removal.ReclaimedBytes,
		})
	case errors.Is(err, cache.ErrCacheEntryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "cache entry not found"})
	default:
		zap.L().Error("remove cache entry",
			zap.Uint64("cache_entry_id", id),
			zap.Bool("object_removed", removal.ObjectRemoved),
			zap.Bool("metadata_removed", removal.MetadataRemoved),
			zap.Int64("reclaimed_bytes", removal.ReclaimedBytes),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":             "CACHE_REMOVE_INCOMPLETE",
			"message":          "cache entry removal did not complete",
			"deleted":          0,
			"failed":           1,
			"reclaimed_bytes":  removal.ReclaimedBytes,
			"object_removed":   removal.ObjectRemoved,
			"metadata_removed": removal.MetadataRemoved,
		})
	}
}

func (h *CacheHandler) Cleanup(c *gin.Context) {
	report, err := h.retention.Reclaim(c.Request.Context(), cache.ReclaimModeManual)
	if err != nil {
		zap.L().Error("manual cache cleanup", zap.Error(err))
		code := "CACHE_CLEANUP_PARTIAL"
		message := "cache cleanup did not complete"
		if errors.Is(err, cache.ErrReclaimTargetNotReached) {
			code = "CACHE_RECLAIM_TARGET_NOT_REACHED"
			message = "cache cleanup incomplete because storage remains above target"
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":            code,
			"message":         message,
			"deleted":         report.Removed,
			"failed":          report.Failed,
			"reclaimed_bytes": report.ReclaimedBytes,
			"examined":        report.Examined,
			"expired_removed": report.ExpiredRemoved,
			"lru_removed":     report.LRURemoved,
			"usage_before":    report.UsageBefore,
			"usage_after":     report.UsageAfter,
		})
		return
	}

	zap.L().Info("cache cleanup completed",
		zap.Int("deleted", report.Removed),
		zap.Int64("reclaimed_bytes", report.ReclaimedBytes),
	)
	c.JSON(http.StatusOK, gin.H{
		"message":         "cleanup completed",
		"deleted":         report.Removed,
		"failed":          report.Failed,
		"reclaimed_bytes": report.ReclaimedBytes,
		"examined":        report.Examined,
		"expired_removed": report.ExpiredRemoved,
		"lru_removed":     report.LRURemoved,
		"usage_before":    report.UsageBefore,
		"usage_after":     report.UsageAfter,
	})
}

func (h *CacheHandler) GetDistribution(c *gin.Context) {
	type TypeBreakdown struct {
		Type      string `json:"type"`
		Size      int64  `json:"size"`
		FileCount int64  `json:"file_count"`
	}

	type PackageSize struct {
		Name     string `json:"name"`
		Type     string `json:"type"`
		Size     int64  `json:"size"`
		HitCount int64  `json:"hit_count"`
	}

	var byType []TypeBreakdown
	h.db.Model(&db.CacheEntry{}).
		Select("adapter_type as type, COALESCE(SUM(size), 0) as size, COUNT(*) as file_count").
		Group("adapter_type").
		Find(&byType)

	var topPackages []PackageSize
	h.db.Model(&db.CacheEntry{}).
		Select("package_name as name, adapter_type as type, COALESCE(SUM(size), 0) as size, COALESCE(SUM(hit_count), 0) as hit_count").
		Where("package_name != ''").
		Group("package_name, adapter_type").
		Order("size DESC").
		Limit(30).
		Find(&topPackages)

	var totalSize int64
	h.db.Model(&db.CacheEntry{}).Select("COALESCE(SUM(size), 0)").Scan(&totalSize)

	maxSize := int64(h.maxSizeGB) * 1024 * 1024 * 1024
	usagePercent := float64(0)
	if maxSize > 0 {
		usagePercent = float64(totalSize) / float64(maxSize) * 100
	}

	c.JSON(http.StatusOK, gin.H{
		"total_size":    totalSize,
		"max_size":      maxSize,
		"usage_percent": usagePercent,
		"by_type":       byType,
		"top_packages":  topPackages,
	})
}
