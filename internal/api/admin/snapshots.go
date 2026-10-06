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

	"depsilo/internal/audit"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/snapshot"
)

// SnapshotsHandler exposes the freeze / golden-snapshot surface: promote the
// cached artifact set, export/import its manifest, and switch snapshot-only
// mode on or off.
type SnapshotsHandler struct {
	store *snapshot.Store
	db    *gorm.DB
}

func NewSnapshotsHandler(store *snapshot.Store, database *gorm.DB) *SnapshotsHandler {
	return &SnapshotsHandler{store: store, db: database}
}

func (h *SnapshotsHandler) List(c *gin.Context) {
	records, err := h.store.List(c.Request.Context())
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	activeID, activeName := h.store.Active()
	c.JSON(http.StatusOK, gin.H{
		"items":              records,
		"active_snapshot_id": activeID,
		"active_snapshot":    activeName,
	})
}

func (h *SnapshotsHandler) Get(c *gin.Context) {
	id, ok := snapshotID(c)
	if !ok {
		return
	}
	record, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	page, perPage := paginationParams(c, 20, 200)
	items, total, err := h.store.Items(c.Request.Context(), id, perPage, (page-1)*perPage)
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	activeID, _ := h.store.Active()
	c.JSON(http.StatusOK, gin.H{
		"snapshot": record,
		"items":    items,
		"total":    total,
		"page":     page,
		"active":   activeID == record.ID,
	})
}

type snapshotCreateRequest struct {
	Name string `json:"name"`
	Note string `json:"note"`
}

func (h *SnapshotsHandler) Create(c *gin.Context) {
	var body snapshotCreateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid snapshot payload"})
		return
	}
	record, err := h.store.CreateFromCache(c.Request.Context(), body.Name, body.Note, requestUsername(c))
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	h.audit(c, "snapshot_create", record)
	c.JSON(http.StatusCreated, record)
}

func (h *SnapshotsHandler) Delete(c *gin.Context) {
	id, ok := snapshotID(c)
	if !ok {
		return
	}
	record, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	if err := h.store.Delete(c.Request.Context(), id); err != nil {
		writeSnapshotError(c, err)
		return
	}
	h.audit(c, "snapshot_delete", record)
	c.JSON(http.StatusOK, gin.H{"deleted": record.ID})
}

func (h *SnapshotsHandler) Export(c *gin.Context) {
	id, ok := snapshotID(c)
	if !ok {
		return
	}
	record, err := h.store.Get(c.Request.Context(), id)
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	filename := "snapshot-" + sanitizeSnapshotFilename(record.Name) + ".json"
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Header("Content-Type", "application/json")
	c.Status(http.StatusOK)
	if err := h.store.Export(c.Request.Context(), id, c.Writer); err != nil {
		// The response may already be partially written; log the failure via
		// the error body only when nothing has been sent yet.
		if !c.Writer.Written() {
			writeSnapshotError(c, err)
		}
	}
}

func (h *SnapshotsHandler) Import(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	body := http.MaxBytesReader(c.Writer, c.Request.Body, maxSnapshotImportBytes)
	record, err := h.store.Import(c.Request.Context(), body, name, requestUsername(c))
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	h.audit(c, "snapshot_import", record)
	c.JSON(http.StatusCreated, record)
}

// maxSnapshotImportBytes bounds one uploaded manifest. The item count is
// bounded separately by snapshot.ImportItemLimit.
const maxSnapshotImportBytes = 256 << 20

type snapshotActivateRequest struct {
	SnapshotID uint `json:"snapshot_id"`
}

func (h *SnapshotsHandler) Activate(c *gin.Context) {
	var body snapshotActivateRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid snapshot payload"})
		return
	}
	record, err := h.store.Activate(c.Request.Context(), body.SnapshotID)
	if err != nil {
		writeSnapshotError(c, err)
		return
	}
	if record != nil {
		h.audit(c, "snapshot_use", record)
		c.JSON(http.StatusOK, gin.H{"active_snapshot_id": record.ID, "active_snapshot": record.Name})
		return
	}
	c.JSON(http.StatusOK, gin.H{"active_snapshot_id": 0, "active_snapshot": ""})
}

// audit records the snapshot lifecycle change in the shared audit stream.
// The action carries the operation; the snapshot name and ID travel in the
// package/version columns, mirroring how quarantine events name decisions.
func (h *SnapshotsHandler) audit(c *gin.Context, action string, record *db.Snapshot) {
	if h.db == nil || record == nil {
		return
	}
	entry := db.AuditLog{
		Action:      action,
		PackageName: record.Name,
		Version:     strconv.FormatUint(uint64(record.ID), 10),
		ClientIP:    c.ClientIP(),
		UserAgent:   c.Request.UserAgent(),
		StatusCode:  http.StatusOK,
		CreatedAt:   time.Now().UTC(),
	}
	if err := audit.AppendAuditRows(c.Request.Context(), h.db, []db.AuditLog{entry}); err != nil {
		zap.L().Warn("snapshot: write audit entry", zap.String("action", action), zap.Error(err))
	}
}

func snapshotID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid snapshot id"})
		return 0, false
	}
	return uint(id), true
}

func requestUsername(c *gin.Context) string {
	if value, ok := c.Get(middleware.ContextKeyUsername); ok {
		if name, ok := value.(string); ok && name != "" {
			return name
		}
	}
	return "admin"
}

func paginationParams(c *gin.Context, defaultPerPage, maxPerPage int) (page, perPage int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	perPage, _ = strconv.Atoi(c.DefaultQuery("per_page", strconv.Itoa(defaultPerPage)))
	if perPage < 1 || perPage > maxPerPage {
		perPage = defaultPerPage
	}
	return page, perPage
}

func sanitizeSnapshotFilename(name string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, name)
	cleaned = strings.Trim(cleaned, "-")
	if cleaned == "" {
		return "snapshot"
	}
	return cleaned
}

func writeSnapshotError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, snapshot.ErrSnapshotNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": err.Error()})
	case errors.Is(err, snapshot.ErrSnapshotNameTaken):
		c.JSON(http.StatusConflict, gin.H{"code": "NAME_TAKEN", "message": err.Error()})
	case errors.Is(err, snapshot.ErrSnapshotActive):
		c.JSON(http.StatusConflict, gin.H{"code": "SNAPSHOT_ACTIVE", "message": err.Error()})
	case errors.Is(err, snapshot.ErrEmptySnapshot):
		c.JSON(http.StatusBadRequest, gin.H{"code": "EMPTY_SNAPSHOT", "message": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "SNAPSHOT_FAILED", "message": err.Error()})
	}
}
