package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

// QuarantineHandler serves the supply-chain quarantine admin endpoints:
// event log + active approvals + approve / revoke actions. Quarantine
// is an open-source wedge feature per docs/DIRECTION.md (NOT Pro-
// gated) so the routes mount under adminGroup, not proGroup.
type QuarantineHandler struct {
	db               *gorm.DB
	store            *quarantine.Store
	approvalsEnabled bool
}

func NewQuarantineHandler(database *gorm.DB, store *quarantine.Store, approvalsEnabled bool) *QuarantineHandler {
	return &QuarantineHandler{db: database, store: store, approvalsEnabled: approvalsEnabled}
}

// ── GET /admin/quarantine/events ──────────────────────────────────
//
// Lists recent QuarantineEvent rows with simple filtering. Used by
// the Monitor UI to show "what got blocked / bypassed / approved
// recently." Pagination uses the same offset/limit shape as the other
// admin list endpoints (audit-logs, access-logs) for consistency in
// the frontend.

type quarantineEventResp struct {
	Items []db.QuarantineEvent `json:"items"`
	Total int64                `json:"total"`
}

func (h *QuarantineHandler) ListEvents(c *gin.Context) {
	limit := parseIntParam(c, "limit", 50, 1, 500)
	offset := parseIntParam(c, "offset", 0, 0, 100000)
	ecosystem := strings.TrimSpace(c.Query("ecosystem"))
	action := strings.TrimSpace(c.Query("action"))
	pkg := strings.TrimSpace(c.Query("package"))

	q := h.db.WithContext(c.Request.Context()).Model(&db.QuarantineEvent{})
	if ecosystem != "" {
		q = q.Where("ecosystem = ?", ecosystem)
	}
	if action != "" {
		q = q.Where("action = ?", action)
	}
	if pkg != "" {
		q = q.Where("package LIKE ?", "%"+pkg+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}

	var items []db.QuarantineEvent
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, quarantineEventResp{Items: items, Total: total})
}

// ── GET /admin/quarantine/approvals ───────────────────────────────
//
// Lists currently-active operator approvals (the ApprovedVersion
// table). Persistent — no expiry — per the option-A locked-in
// decision; an admin who regrets an approval revokes it via the
// DELETE handler.

type quarantineApprovalsResp struct {
	Items []db.ApprovedVersion `json:"items"`
	Total int64                `json:"total"`
}

func (h *QuarantineHandler) ListApprovals(c *gin.Context) {
	limit := parseIntParam(c, "limit", 50, 1, 500)
	offset := parseIntParam(c, "offset", 0, 0, 100000)

	q := h.db.WithContext(c.Request.Context()).Model(&db.ApprovedVersion{})

	var total int64
	if err := q.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}

	var items []db.ApprovedVersion
	if err := q.Order("created_at DESC").Limit(limit).Offset(offset).Find(&items).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, quarantineApprovalsResp{Items: items, Total: total})
}

// ── POST /admin/quarantine/approve ────────────────────────────────
//
// Approves a (ecosystem, package, version) tuple. Reason is mandatory
// per the locked-in decisions — every approval must explain itself so
// security review has the "why" alongside the "what." Idempotent: an
// existing approval gets its reason + actor + created_at refreshed.

type approveReq struct {
	Ecosystem string `json:"ecosystem" binding:"required,max=32"`
	Package   string `json:"package"   binding:"required,max=256"`
	Version   string `json:"version"   binding:"required,max=128"`
	Reason    string `json:"reason"    binding:"required,min=3,max=512"`
}

func (h *QuarantineHandler) Approve(c *gin.Context) {
	if !h.approvalsEnabled {
		c.JSON(http.StatusConflict, gin.H{
			"code":    "MINIMUM_RELEASE_AGE_UNAVAILABLE",
			"message": "minimum-release-age approvals are unavailable until artifact-source and timestamp provenance are bound",
		})
		return
	}
	var req approveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}
	coordinate, err := quarantine.NormalizeCoordinate(req.Ecosystem, req.Package, req.Version)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_COORDINATE", "message": err.Error()})
		return
	}
	actorID := userIDFromContext(c)
	if err := h.store.Approve(c.Request.Context(), coordinate.Ecosystem, coordinate.Package, coordinate.Version, req.Reason, actorID); err != nil {
		zap.L().Error("quarantine approve",
			zap.String("ecosystem", coordinate.Ecosystem),
			zap.String("package", coordinate.Package),
			zap.String("version", coordinate.Version),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ecosystem":   coordinate.Ecosystem,
		"package":     coordinate.Package,
		"version":     coordinate.Version,
		"reason":      req.Reason,
		"approved_by": actorID,
		"created_at":  time.Now().UTC(),
	})
}

// ── DELETE /admin/quarantine/approvals/:id ────────────────────────
//
// Revokes an existing approval. Body carries a reason (required) so
// the audit trail can answer "why did this approval go away?" Looks
// the row up by ID, then delegates to Store.Revoke which writes both
// the delete and the audit event in one transaction.

type revokeReq struct {
	Reason string `json:"reason" binding:"required,min=3,max=512"`
}

func (h *QuarantineHandler) Revoke(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_ID", "message": err.Error()})
		return
	}
	var req revokeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}

	// Resolve the (eco, pkg, version) from the row id, then revoke
	// by triple — Store.Revoke is keyed by the natural key rather
	// than the surrogate id to keep the API symmetric with Approve.
	var row db.ApprovedVersion
	err = h.db.WithContext(c.Request.Context()).
		Where("id = ?", id).
		First(&row).Error
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND"})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}

	actorID := userIDFromContext(c)
	coordinate, err := quarantine.NormalizeCoordinate(row.Ecosystem, row.Package, row.Version)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"code": "INVALID_STORED_COORDINATE", "message": "stored approval cannot be interpreted safely; remove it during database migration",
		})
		return
	}
	if err := h.store.Revoke(c.Request.Context(), coordinate.Ecosystem, coordinate.Package, coordinate.Version, req.Reason, actorID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"ecosystem":  coordinate.Ecosystem,
		"package":    coordinate.Package,
		"version":    coordinate.Version,
		"revoked_by": actorID,
		"reason":     req.Reason,
	})
}

// userIDFromContext extracts the admin's user id from the auth
// middleware. Returns 0 when no user is bound (e.g. config-driven
// programmatic calls); audit events tolerate a 0 actor and just
// render the action as "system."
func userIDFromContext(c *gin.Context) uint {
	v, ok := c.Get("user_id")
	if !ok {
		return 0
	}
	if id, ok := v.(uint); ok {
		return id
	}
	return 0
}

// parseIntParam reads ?key=<n> from the request with bounds. Used
// for limit/offset across the list endpoints.
func parseIntParam(c *gin.Context, key string, def, min, max int) int {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}
