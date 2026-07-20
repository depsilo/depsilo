package admin

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

type LatencyHandler struct {
	db *gorm.DB
}

const maxLatencyHistoryPoints = 44

type latencyPoint struct {
	Time      time.Time `json:"time"`
	LatencyMs int64     `json:"latency_ms"`
	Healthy   bool      `json:"healthy"`
}

type upstreamLatencySeries struct {
	UpstreamID uint           `json:"upstream_id"`
	Points     []latencyPoint `json:"points"`
}

const latencySeriesQuery = `
	WITH ranked AS (
		SELECT
			id,
			upstream_id,
			created_at,
			latency_ms,
			healthy,
			ROW_NUMBER() OVER (
				PARTITION BY upstream_id
				ORDER BY created_at DESC, id DESC
			) AS row_rank
		FROM upstream_latency_logs
		WHERE upstream_id IN (SELECT id FROM upstream_records)
			AND created_at >= ?
	)
	SELECT id, upstream_id, created_at AS time, latency_ms, healthy
	FROM ranked
	WHERE row_rank <= ?
	ORDER BY upstream_id ASC, created_at ASC, id ASC
`

func NewLatencyHandler(database *gorm.DB) *LatencyHandler {
	return &LatencyHandler{db: database}
}

func latencyRangeDuration(rangeParam string) time.Duration {
	switch rangeParam {
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

func (h *LatencyHandler) GetLatencyHistory(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Invalid upstream ID"})
		return
	}

	since := time.Now().Add(-latencyRangeDuration(c.DefaultQuery("range", "24h")))

	// Get upstream name
	var upstream db.UpstreamRecord
	if err := h.db.First(&upstream, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Upstream not found"})
			return
		}
		zap.L().Error("load upstream for latency history", zap.Uint64("upstream_id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to load upstream"})
		return
	}

	points := make([]latencyPoint, 0)
	result := h.db.Model(&db.UpstreamLatencyLog{}).
		Select("created_at as time, latency_ms, healthy").
		Where("upstream_id = ? AND created_at >= ?", upstream.ID, since).
		Order("created_at ASC, id ASC").
		Find(&points)
	if result.Error != nil {
		zap.L().Error("load upstream latency history", zap.Uint("upstream_id", upstream.ID), zap.Error(result.Error))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to load latency history"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"upstream_name": upstream.Name,
		"points":        points,
	})
}

// GetLatencySeries returns the recent history for every upstream in one
// database query. Ranking newest-first keeps the query bounded per upstream;
// the outer ordering restores the chronological order expected by the UI.
func (h *LatencyHandler) GetLatencySeries(c *gin.Context) {
	since := time.Now().Add(-latencyRangeDuration(c.DefaultQuery("range", "24h")))
	type latencyRow struct {
		ID         uint      `gorm:"column:id"`
		UpstreamID uint      `gorm:"column:upstream_id"`
		Time       time.Time `gorm:"column:time"`
		LatencyMs  int64     `gorm:"column:latency_ms"`
		Healthy    bool      `gorm:"column:healthy"`
	}

	rows := make([]latencyRow, 0)
	result := h.db.WithContext(c.Request.Context()).Raw(
		latencySeriesQuery,
		since,
		maxLatencyHistoryPoints,
	).Scan(&rows)
	if result.Error != nil {
		zap.L().Error("load upstream latency series", zap.Error(result.Error))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "failed to load latency series"})
		return
	}

	series := make([]upstreamLatencySeries, 0)
	for _, row := range rows {
		if len(series) == 0 || series[len(series)-1].UpstreamID != row.UpstreamID {
			series = append(series, upstreamLatencySeries{
				UpstreamID: row.UpstreamID,
				Points:     make([]latencyPoint, 0, maxLatencyHistoryPoints),
			})
		}
		current := &series[len(series)-1]
		current.Points = append(current.Points, latencyPoint{
			Time:      row.Time,
			LatencyMs: row.LatencyMs,
			Healthy:   row.Healthy,
		})
	}

	c.JSON(http.StatusOK, gin.H{"series": series})
}
