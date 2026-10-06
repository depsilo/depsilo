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
	db     *gorm.DB
	anchor *audit.Anchor
}

// NewAuditHandler creates a new AuditHandler.
func NewAuditHandler(database *gorm.DB) *AuditHandler {
	return &AuditHandler{db: database}
}

// SetAnchor points integrity checks at the running chain anchor, so a
// whole-database rewrite is compared against checkpoints that left the
// database, and the admin surface can report the remote feed's health.
func (h *AuditHandler) SetAnchor(anchor *audit.Anchor) {
	h.anchor = anchor
}

// List returns paginated audit log entries.
func (h *AuditHandler) List(c *gin.Context) {
	q := h.parseQuery(c)
	result, err := audit.RunQuery(h.db, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toAuditListResponse(result, principalCanViewCredentials(c)))
}

// Export returns audit log entries as a CSV download.
func (h *AuditHandler) Export(c *gin.Context) {
	items, err := audit.RunExportQuery(h.db, h.parseQuery(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}
	data, err := encodeAuditCSV(toAuditLogResponses(items, principalCanViewCredentials(c)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "EXPORT_ERROR", "message": err.Error()})
		return
	}

	filename := fmt.Sprintf("depsilo-audit-%s.csv", time.Now().Format("2006-01-02"))
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Data(http.StatusOK, "text/csv", data)
}

// Integrity recomputes the tamper-evident audit chain and reports the first
// broken row, the pre-chain prefix, and the current head.
func (h *AuditHandler) Integrity(c *gin.Context) {
	report, err := audit.VerifyChain(c.Request.Context(), h.db, 2000)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTEGRITY_CHECK_FAILED", "message": err.Error()})
		return
	}
	anchorPath := ""
	feed := audit.AnchorFeedStatus{}
	if h.anchor != nil {
		anchorPath = h.anchor.CheckpointPath()
		feed = h.anchor.FeedStatus()
	}
	anchors, err := audit.VerifyAnchors(c.Request.Context(), h.db, anchorPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTEGRITY_CHECK_FAILED", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"integrity": report, "anchors": anchors, "anchor_feed": feed})
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
