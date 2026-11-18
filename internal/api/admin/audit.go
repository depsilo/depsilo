package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
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
	q, err := h.parseQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	result, err := audit.RunQuery(h.db, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	ecosystems, err := audit.EcosystemCounts(h.db, q)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, toAuditListResponse(result, ecosystems, principalCanViewCredentials(c)))
}

// Export returns audit log entries as a CSV download.
func (h *AuditHandler) Export(c *gin.Context) {
	q, err := h.parseQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	items, err := audit.RunExportQuery(h.db, q)
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
	c.JSON(http.StatusOK, contract.AuditIntegrityResponse{
		AnchorFeed: contract.AuditAnchorFeed{
			FileConfigured: feed.FileConfigured, FileLastSuccessAt: feed.FileLastSuccessAt, FileLastError: feed.FileLastError,
			RemoteConfigured: feed.RemoteConfigured, RemoteLastSuccessAt: feed.RemoteLastSuccessAt,
			RemoteHeadID: feed.RemoteHeadID, RemoteLastError: feed.RemoteLastError,
		},
		Anchors: contract.AuditAnchorReport{
			Configured: anchors.Configured, Path: anchors.Path, Checkpoints: anchors.Checkpoints,
			InvalidLines: anchors.InvalidLines, LatestCheckedAt: anchors.LatestCheckedAt,
			LatestHeadID: anchors.LatestHeadID, LatestHeadHash: anchors.LatestHeadHash,
			OK: anchors.OK, BrokenAtID: anchors.BrokenAtID, Reason: anchors.Reason,
		},
		Integrity: contract.AuditChainReport{
			OK: report.OK, RowsScanned: report.RowsScanned, ChainedRows: report.ChainedRows,
			UnchainedRows: report.UnchainedRows, FirstChainedID: report.FirstChainedID,
			HeadID: report.HeadID, HeadHash: report.HeadHash, BrokenAtID: report.BrokenAtID,
			Reason: report.Reason, ScannedAt: report.ScannedAt,
		},
	})
}

func (h *AuditHandler) parseQuery(c *gin.Context) (audit.Query, error) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))

	ecosystems, err := parseEcosystemFilter(c, "ecosystem")
	if err != nil {
		return audit.Query{}, err
	}
	q := audit.Query{
		Ecosystems:  ecosystems,
		PackageName: c.Query("package"),
		ClientIP:    c.Query("ip"),
		CacheResult: c.Query("result"),
		Page:        page,
		PageSize:    pageSize,
	}

	// A malformed time used to be ignored silently, widening the result to
	// all time; it is now a 400 like the other Admin lists.
	window, err := parseTimeWindow(c)
	if err != nil {
		return audit.Query{}, err
	}
	q.StartTime, q.EndTime = window.Start, window.End

	return q, nil
}
