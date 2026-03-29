package admin

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depslio/internal/cache"
	"depslio/internal/db"
)

type CacheHandler struct {
	db      *gorm.DB
	storage cache.Storage
}

func NewCacheHandler(database *gorm.DB, storage cache.Storage) *CacheHandler {
	return &CacheHandler{db: database, storage: storage}
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
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var entry db.CacheEntry
	if err := h.db.First(&entry, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "cache entry not found"})
		return
	}

	// Delete from storage
	if err := h.storage.Delete(c.Request.Context(), entry.StoragePath); err != nil {
		zap.L().Warn("failed to delete cache file", zap.String("key", entry.Key), zap.Error(err))
	}

	h.db.Delete(&entry)
	c.JSON(http.StatusOK, gin.H{"message": "deleted"})
}

func (h *CacheHandler) Cleanup(c *gin.Context) {
	now := time.Now()

	// Delete expired entries
	var expired []db.CacheEntry
	h.db.Where("expires_at < ?", now).Find(&expired)

	deleted := 0
	for _, entry := range expired {
		if err := h.storage.Delete(c.Request.Context(), entry.StoragePath); err != nil {
			zap.L().Warn("cleanup: failed to delete file", zap.String("key", entry.Key), zap.Error(err))
		}
		h.db.Delete(&entry)
		deleted++
	}

	zap.L().Info("cache cleanup completed", zap.Int("deleted", deleted))
	c.JSON(http.StatusOK, gin.H{
		"message": "cleanup completed",
		"deleted": deleted,
	})
}
