package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/audit"
)

// AuditHandler handles Pro audit log API endpoints.
type AuditHandler struct {
	db *gorm.DB
}

// NewAuditHandler creates a new AuditHandler.
func NewAuditHandler(database *gorm.DB) *AuditHandler {
	return &AuditHandler{db: database}
}

// List returns paginated audit log entries.
func (h *AuditHandler) List(c *gin.Context) {
	q := h.parseQuery(c)
	result, err := audit.RunQuery(h.db, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toAuditListResponse(result))
}

// Export returns audit log entries as a CSV download.
func (h *AuditHandler) Export(c *gin.Context) {
	items, err := audit.RunExportQuery(h.db, h.parseQuery(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}
	data, err := encodeAuditCSV(toAuditLogResponses(items))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}

	filename := fmt.Sprintf("depsilo-audit-%s.csv", time.Now().Format("2006-01-02"))
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/csv", data)
}

func (h *AuditHandler) parseQuery(c *gin.Context) audit.Query {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	packageName, hasPackage := c.GetQuery("package")
	if !hasPackage {
		packageName = c.Query("search")
		if packageName != "" {
			zap.L().Warn("deprecated admin query parameter", zap.String("endpoint", "audit-logs"), zap.String("parameter", "search"), zap.String("replacement", "package"))
		}
	}

	q := audit.Query{
		Ecosystem:   c.Query("ecosystem"),
		PackageName: packageName,
		ClientIP:    c.Query("ip"),
		CacheResult: c.Query("result"),
		Page:        page,
		PageSize:    pageSize,
	}

	if s := c.Query("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			q.StartTime = t
		}
	}
	if s := c.Query("end"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			q.EndTime = t
		}
	}

	return q
}
