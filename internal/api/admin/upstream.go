package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"repocache/internal/db"
)

type UpstreamHandler struct {
	db *gorm.DB
}

func NewUpstreamHandler(database *gorm.DB) *UpstreamHandler {
	return &UpstreamHandler{db: database}
}

func (h *UpstreamHandler) List(c *gin.Context) {
	var upstreams []db.UpstreamRecord
	h.db.Order("adapter_type, priority").Find(&upstreams)
	c.JSON(http.StatusOK, upstreams)
}

type upstreamRequest struct {
	AdapterType string `json:"adapter_type" binding:"required"`
	Name        string `json:"name" binding:"required"`
	URL         string `json:"url" binding:"required"`
	Proxy       string `json:"proxy"`
	Priority    int    `json:"priority"`
}

func (h *UpstreamHandler) Create(c *gin.Context) {
	var req upstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	record := db.UpstreamRecord{
		AdapterType: req.AdapterType,
		Name:        req.Name,
		URL:         req.URL,
		Proxy:       req.Proxy,
		Priority:    req.Priority,
		Healthy:     true,
		SuccessRate: 1.0,
	}

	if err := h.db.Create(&record).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"code": "CONFLICT", "message": "upstream name already exists"})
		return
	}

	c.JSON(http.StatusCreated, record)
}

func (h *UpstreamHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var record db.UpstreamRecord
	if err := h.db.First(&record, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "upstream not found"})
		return
	}

	var req upstreamRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	h.db.Model(&record).Updates(map[string]interface{}{
		"adapter_type": req.AdapterType,
		"name":         req.Name,
		"url":          req.URL,
		"proxy":        req.Proxy,
		"priority":     req.Priority,
	})

	h.db.First(&record, id)
	c.JSON(http.StatusOK, record)
}

func (h *UpstreamHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	result := h.db.Delete(&db.UpstreamRecord{}, id)
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "upstream not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *UpstreamHandler) Check(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var record db.UpstreamRecord
	if err := h.db.First(&record, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "upstream not found"})
		return
	}

	// TODO: perform actual health check request in Phase 6
	c.JSON(http.StatusOK, gin.H{
		"id":      record.ID,
		"name":    record.Name,
		"healthy": record.Healthy,
		"message": "health check queued",
	})
}
