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

func NewLatencyHandler(database *gorm.DB) *LatencyHandler {
	return &LatencyHandler{db: database}
}

func (h *LatencyHandler) GetLatencyHistory(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": "Invalid upstream ID"})
		return
	}

	rangeParam := c.DefaultQuery("range", "24h")
	var duration time.Duration
	switch rangeParam {
	case "1h":
		duration = time.Hour
	case "6h":
		duration = 6 * time.Hour
	case "24h":
		duration = 24 * time.Hour
	case "7d":
		duration = 7 * 24 * time.Hour
	default:
		duration = 24 * time.Hour
	}

	since := time.Now().Add(-duration)

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

	type LatencyPoint struct {
		Time      time.Time `json:"time"`
		LatencyMs int64     `json:"latency_ms"`
		Healthy   bool      `json:"healthy"`
	}

	points := make([]LatencyPoint, 0)
	result := h.db.Model(&db.UpstreamLatencyLog{}).
		Select("created_at as time, latency_ms, healthy").
		Where("upstream_id = ? AND created_at >= ?", upstream.ID, since).
		Order("datetime(created_at) ASC").
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
