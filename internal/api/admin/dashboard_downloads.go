package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
)

const (
	defaultRecentDownloadsLimit = 6
	maxRecentDownloadsLimit     = 20
)

// recentDownloadResponse is intentionally narrower than AuditLog. The
// dashboard needs package activity, not client identity or upstream details.
type recentDownloadResponse struct {
	ID          uint      `json:"id"`
	Ecosystem   string    `json:"ecosystem"`
	PackageName string    `json:"package_name"`
	Version     string    `json:"version"`
	CacheResult string    `json:"cache_result"`
	LatencyMs   int64     `json:"latency_ms"`
	BytesSent   int64     `json:"bytes_sent"`
	StatusCode  int       `json:"status_code"`
	CreatedAt   time.Time `json:"created_at"`
}

type recentDownloadsResponse struct {
	Items []recentDownloadResponse `json:"items"`
}

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
	items := make([]recentDownloadResponse, 0, limit)
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

	c.JSON(http.StatusOK, recentDownloadsResponse{Items: items})
}
