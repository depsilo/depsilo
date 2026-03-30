package admin

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

type AccessLogHandler struct {
	db *gorm.DB
}

func NewAccessLogHandler(database *gorm.DB) *AccessLogHandler {
	return &AccessLogHandler{db: database}
}

func (h *AccessLogHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	search := c.Query("search")
	adapterType := c.Query("adapter_type")
	if adapterType == "" {
		adapterType = c.Query("type")
	}
	hitFilter := c.Query("hit") // "true" | "false" | ""

	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	query := h.db.Model(&db.AccessLog{})
	if search != "" {
		query = query.Where("package_name LIKE ?", "%"+search+"%")
	}
	if adapterType != "" {
		query = query.Where("adapter_type = ?", adapterType)
	}
	if hitFilter == "true" {
		query = query.Where("hit = ?", true)
	} else if hitFilter == "false" {
		query = query.Where("hit = ?", false)
	}

	var total int64
	query.Count(&total)

	var logs []db.AccessLog
	query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs)

	c.JSON(http.StatusOK, gin.H{
		"total":     total,
		"page":      page,
		"page_size": pageSize,
		"items":     logs,
	})
}
