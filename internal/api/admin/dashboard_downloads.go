package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"depsilo/internal/accesslog"
	"depsilo/internal/api/contract"
	"depsilo/internal/db"
)

const (
	defaultRecentDownloadsLimit = 6
	maxRecentDownloadsLimit     = 20
	recentDownloadsScanBatch    = 100
	maxRecentDownloadsScan      = 1000
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
// artifact downloads, read from the access log: package request outcomes are
// request history, not audit events. The monotonic access-log ID is the
// cursor for this live feed; scanning it newest first stays index-backed and
// avoids the COUNT + datetime sort used by paginated logs. Artifact versus
// metadata is the cache-kind classification the access row's key carries, so
// the scan reads a bounded number of rows past metadata requests.
func (h *DashboardHandler) GetRecentDownloads(c *gin.Context) {
	limit := normalizeRecentDownloadsLimit(c.Query("limit"))
	items := make([]contract.RecentDownload, 0, limit)
	var beforeID uint
	for scanned := 0; len(items) < limit && scanned < maxRecentDownloadsScan; {
		var rows []db.AccessLog
		query := h.db.WithContext(c.Request.Context()).
			Select("id", "adapter_type", "cache_key", "package_name", "hit", "latency_ms", "bytes_sent", "status_code", "created_at").
			Order("id DESC").
			Limit(recentDownloadsScanBatch)
		if beforeID > 0 {
			query = query.Where("id < ?", beforeID)
		}
		if err := query.Find(&rows).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
			return
		}
		for _, row := range rows {
			if len(items) == limit {
				break
			}
			if db.ClassifyCacheKind(row.AdapterType, row.CacheKey) == db.CacheKindMetadata {
				continue
			}
			items = append(items, contract.RecentDownload{
				ID: row.ID, Ecosystem: row.AdapterType, PackageName: row.PackageName,
				Version:     accesslog.RecordedVersion(row.AdapterType, row.CacheKey, row.StatusCode),
				CacheResult: recentDownloadResult(row.Hit, row.StatusCode),
				LatencyMs:   row.LatencyMs, BytesSent: row.BytesSent, StatusCode: row.StatusCode,
				CreatedAt: row.CreatedAt,
			})
		}
		scanned += len(rows)
		if len(rows) < recentDownloadsScanBatch {
			break
		}
		beforeID = rows[len(rows)-1].ID
	}

	c.JSON(http.StatusOK, contract.RecentDownloadsResponse{Items: items})
}

// recentDownloadResult keeps the hit/miss/error vocabulary this feed has
// always reported: a server error is an error whatever the cache did.
func recentDownloadResult(hit bool, statusCode int) string {
	switch {
	case statusCode >= 500:
		return "error"
	case hit:
		return "hit"
	default:
		return "miss"
	}
}
