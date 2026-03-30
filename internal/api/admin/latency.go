package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
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
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "Upstream not found"})
		return
	}

	type LatencyPoint struct {
		Time      time.Time `json:"time"`
		LatencyMs int64     `json:"latency_ms"`
		Healthy   bool      `json:"healthy"`
	}

	var points []LatencyPoint
	h.db.Model(&db.UpstreamLatencyLog{}).
		Select("created_at as time, latency_ms, healthy").
		Where("name = ? AND created_at >= ?", upstream.Name, since).
		Order("created_at ASC").
		Find(&points)

	c.JSON(http.StatusOK, gin.H{
		"upstream_name": upstream.Name,
		"points":        points,
	})
}
