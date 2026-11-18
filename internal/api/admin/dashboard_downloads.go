package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

const (
	defaultRecentDownloadsLimit = 6
	maxRecentDownloadsLimit     = 20
)

func normalizeRecentDownloadsLimit(raw string) int {
	if raw == "" {
		return defaultRecentDownloadsLimit
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return defaultRecentDownloadsLimit
	}
	if limit < 1 {
		return 1
	}
	if limit > maxRecentDownloadsLimit {
		return maxRecentDownloadsLimit
	}
	return limit
}

// GetRecentDownloads returns a small snapshot of the most recently recorded
// artifact downloads. The monotonic ingestion ID is the cursor for this live
// feed; it keeps polling index-backed and avoids the COUNT + datetime sort used
// by paginated logs.
func (h *DashboardHandler) GetRecentDownloads(c *gin.Context) {
	limit := normalizeRecentDownloadsLimit(c.Query("limit"))
	items := make([]contract.RecentDownload, 0, limit)
	err := h.db.WithContext(c.Request.Context()).
		Model(&db.AuditLog{}).
		Select("id", "ecosystem", "package_name", "version", "cache_result", "latency_ms", "bytes_sent", "status_code", "created_at").
		Where("action = ?", "download").
		Order("id DESC").
		Limit(limit).
		Find(&items).Error
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, contract.RecentDownloadsResponse{Items: items})
}
