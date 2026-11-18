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

	"depsilo/internal/api/contract"
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

func (h *QuarantineHandler) ListEvents(c *gin.Context) {
	limit := parseIntParam(c, "limit", 50, 1, 500)
	offset := parseIntParam(c, "offset", 0, 0, 100000)
	ecosystems, err := parseEcosystemFilter(c, "ecosystem")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	action := strings.TrimSpace(c.Query("action"))
	pkg := strings.TrimSpace(c.Query("package"))

	filtered := func(withEcosystems bool) *gorm.DB {
		q := h.db.WithContext(c.Request.Context()).Model(&db.QuarantineEvent{})
		if withEcosystems && len(ecosystems) > 0 {
			q = q.Where("ecosystem IN ?", ecosystems)
		}
		if action != "" {
			q = q.Where("action = ?", action)
		}
		if pkg != "" {
			q = q.Where("package LIKE ?", "%"+pkg+"%")
		}
		return q
	}
	q := filtered(true)

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
	counts, err := countByEcosystem(filtered(false), "ecosystem")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, contract.QuarantineEventListResponse{Items: toQuarantineEvents(items), Total: total, Ecosystems: counts})
}

// ── GET /admin/quarantine/approvals ───────────────────────────────
//
// Lists currently-active operator approvals (the ApprovedVersion
// table). Persistent — no expiry — per the option-A locked-in
// decision; an admin who regrets an approval revokes it via the
// DELETE handler.

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
	c.JSON(http.StatusOK, contract.QuarantineApprovalListResponse{Items: toQuarantineApprovals(items), Total: total})
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
	c.JSON(http.StatusOK, contract.QuarantineApproveResponse{
		Ecosystem: coordinate.Ecosystem, Package: coordinate.Package, Version: coordinate.Version,
		Reason: req.Reason, ApprovedBy: actorID, CreatedAt: time.Now().UTC(),
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
	c.JSON(http.StatusOK, contract.QuarantineRevokeResponse{
		Ecosystem: coordinate.Ecosystem, Package: coordinate.Package, Version: coordinate.Version,
		RevokedBy: actorID, Reason: req.Reason,
	})
}

func toQuarantineEvents(rows []db.QuarantineEvent) []contract.QuarantineEvent {
	events := make([]contract.QuarantineEvent, len(rows))
	for i, row := range rows {
		events[i] = contract.QuarantineEvent{
			ID: row.ID, Ecosystem: row.Ecosystem, Package: row.Package, Version: row.Version,
			Action: row.Action, Reason: row.Reason, ThresholdSeconds: row.Threshold,
			AgeAtCallSeconds: row.AgeAtCall, ActorID: row.ActorID, ClientIP: row.ClientIP,
			CreatedAt: row.CreatedAt,
		}
	}
	return events
}

func toQuarantineApprovals(rows []db.ApprovedVersion) []contract.QuarantineApproval {
	approvals := make([]contract.QuarantineApproval, len(rows))
	for i, row := range rows {
		approvals[i] = contract.QuarantineApproval{
			ID: row.ID, Ecosystem: row.Ecosystem, Package: row.Package, Version: row.Version,
			Reason: row.Reason, ApprovedBy: row.ApprovedBy, CreatedAt: row.CreatedAt,
		}
	}
	return approvals
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
