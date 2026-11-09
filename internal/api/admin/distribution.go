package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"depsilo/internal/audit"
	"depsilo/internal/db"
	"depsilo/internal/distribution"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type distributionStore interface {
	Stage(context.Context, []byte, string) (*distribution.Set, error)
	List(context.Context, int, int) ([]distribution.Set, int64, error)
	Get(context.Context, string) (*distribution.Set, error)
	Put(context.Context, string, string, io.Reader) (*distribution.UploadResult, error)
	Verify(context.Context, string) (*distribution.Verification, error)
	Delete(context.Context, string) error
	Usage(context.Context) (*distribution.Usage, error)
	Prune(context.Context) (int64, error)
	PrepareExport(context.Context, string) (*distribution.Export, error)
	Import(context.Context, io.Reader, string) (*distribution.ImportResult, error)
}

// DistributionHandler exposes an operator-owned preparation catalog. It does
// not publish package routes or accept a remotely trusted transfer bundle.
type DistributionHandler struct {
	store distributionStore
	db    *gorm.DB
}

func NewDistributionHandler(store *distribution.Store, database *gorm.DB) *DistributionHandler {
	h := &DistributionHandler{db: database}
	if store != nil {
		h.store = store
	}
	return h
}

func (h *DistributionHandler) enabled(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if h.store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "DISTRIBUTION_DISABLED", "message": "dependency-set preparation is not enabled"})
		return false
	}
	return true
}

func (h *DistributionHandler) List(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	page, size := paginationParams(c, 20, 100)
	// Keep malformed/huge offsets out of database arithmetic.
	if page > 1_000_000 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "page is out of range"})
		return
	}
	items, total, err := h.store.List(c.Request.Context(), size, (page-1)*size)
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	usage, err := h.store.Usage(c.Request.Context())
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	// List metadata stays small even when each manifest declares thousands of
	// objects. The full immutable declaration is available through Get.
	summaries := make([]gin.H, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, gin.H{"id": item.ID, "name": item.Name, "revision": item.Revision,
			"created_at": item.CreatedAt, "verified_at": item.VerifiedAt, "checked_at": item.CheckedAt,
			"content_status": item.ContentStatus, "provenance_status": item.ProvenanceStatus,
			"install_validation": item.InstallValidation, "serving": item.Serving,
			"target": item.Manifest.Target, "object_count": len(item.Manifest.Objects)})
	}
	c.JSON(http.StatusOK, gin.H{"items": summaries, "total": total, "page": page, "per_page": size,
		"usage": usage, "scope": "catalog_only", "serving": false})
}

func (h *DistributionHandler) Get(c *gin.Context) {
	if !h.enabled(c) || !distributionID(c, "id") {
		return
	}
	record, err := h.store.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	c.JSON(http.StatusOK, record)
}

func (h *DistributionHandler) Stage(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, distribution.MaxManifestBytes)
	raw, err := io.ReadAll(body)
	if err != nil {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"code": "MANIFEST_TOO_LARGE", "message": "manifest exceeds the request limit or could not be read"})
		return
	}
	record, err := h.store.Stage(c.Request.Context(), raw, requestUsername(c))
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	h.audit(c, "dist_stage", record.ID)
	c.JSON(http.StatusCreated, record)
}

func (h *DistributionHandler) Put(c *gin.Context) {
	if !h.enabled(c) || !distributionID(c, "id") || !distributionID(c, "digest") {
		return
	}
	// The module verifies the declared length and digest while streaming. A
	// chunked request is permitted; Content-Length is never integrity evidence.
	result, err := h.store.Put(c.Request.Context(), c.Param("id"), c.Param("digest"), c.Request.Body)
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	h.audit(c, "dist_upload", c.Param("id"))
	c.JSON(http.StatusOK, result)
}

func (h *DistributionHandler) Verify(c *gin.Context) {
	if !h.enabled(c) || !distributionID(c, "id") {
		return
	}
	result, err := h.store.Verify(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	h.audit(c, "dist_verify", c.Param("id"))
	c.JSON(http.StatusOK, result)
}

func (h *DistributionHandler) Delete(c *gin.Context) {
	if !h.enabled(c) || !distributionID(c, "id") {
		return
	}
	if err := h.store.Delete(c.Request.Context(), c.Param("id")); err != nil {
		writeDistributionError(c, err)
		return
	}
	h.audit(c, "dist_delete", c.Param("id"))
	c.Status(http.StatusNoContent)
}

func (h *DistributionHandler) Prune(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	removed, err := h.store.Prune(c.Request.Context())
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	h.audit(c, "dist_prune", fmt.Sprintf("removed_bytes:%d", removed))
	c.JSON(http.StatusOK, gin.H{"removed_bytes": removed, "scope": "catalog_only", "serving": false})
}

// Export streams a signed full bundle with an exact Content-Length. If content
// fails verification mid-stream the body ends short, which HTTP clients report
// as an unexpected EOF instead of a complete download.
func (h *DistributionHandler) Export(c *gin.Context) {
	if !h.enabled(c) || !distributionID(c, "id") {
		return
	}
	export, err := h.store.PrepareExport(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeDistributionError(c, err)
		return
	}
	c.Header("Content-Type", "application/x-tar")
	c.Header("Content-Length", strconv.FormatInt(export.Size, 10))
	c.Header("Content-Disposition", `attachment; filename="`+export.Filename+`"`)
	c.Header("X-Depsilo-Set-ID", export.SetID)
	c.Header("X-Depsilo-Signer-Key-ID", export.SignerKeyID)
	// Signed material leaves with the first bytes, so record the export before
	// streaming and record an incomplete stream separately.
	h.audit(c, "dist_export", export.SetID)
	c.Status(http.StatusOK)
	if err := export.Write(c.Request.Context(), c.Writer); err != nil {
		zap.L().Warn("distribution: bundle export ended before completion", zap.String("set_id", export.SetID), zap.Error(distributionLogError(err)))
		h.auditStatus(c, "dist_export_incomplete", export.SetID, http.StatusInternalServerError)
	}
}

// Import accepts a streamed bundle body. Its size is bounded by the signed
// declarations and the configured storage limit, not by a request body cap.
func (h *DistributionHandler) Import(c *gin.Context) {
	if !h.enabled(c) {
		return
	}
	result, err := h.store.Import(c.Request.Context(), c.Request.Body, requestUsername(c))
	if err != nil {
		status, code, message := distributionErrorResponse(err)
		body := gin.H{"code": code, "message": message}
		if result != nil {
			// The signed identity is known and Stage may already have created a
			// catalog entry, so the attempt is audited and returned for retry.
			body["import"] = result
			h.auditStatus(c, "dist_import_failed", result.SetID, status)
		}
		c.JSON(status, body)
		return
	}
	h.audit(c, "dist_import", result.SetID)
	c.JSON(http.StatusOK, result)
}

// Storage and archive errors can contain paths; log only the error class.
func distributionLogError(err error) error {
	for _, known := range []error{distribution.ErrIntegrity, distribution.ErrIncomplete, context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, known) {
			return known
		}
	}
	return errors.New("write failed")
}

var distributionDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

func distributionID(c *gin.Context, parameter string) bool {
	if distributionDigest.MatchString(c.Param(parameter)) {
		return true
	}
	c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "expected a lowercase SHA-256 identifier"})
	return false
}

func writeDistributionError(c *gin.Context, err error) {
	status, code, message := distributionErrorResponse(err)
	c.JSON(status, gin.H{"code": code, "message": message})
}

func distributionErrorResponse(err error) (int, string, string) {
	status, code, message := http.StatusInternalServerError, "DISTRIBUTION_UNAVAILABLE", "dependency-set operation could not be completed"
	switch {
	case errors.Is(err, distribution.ErrBundle):
		status, code, message = http.StatusBadRequest, "INVALID_BUNDLE", "transfer bundle is malformed, reordered, or contains undeclared members"
	case errors.Is(err, distribution.ErrUntrusted):
		status, code, message = http.StatusUnprocessableEntity, "UNTRUSTED_SIGNER", "transfer bundle is not signed by a configured trusted transfer key"
	case errors.Is(err, distribution.ErrSigningUnavailable):
		status, code, message = http.StatusServiceUnavailable, "TRANSFER_SIGNING_NOT_CONFIGURED", "distribution.transfer_signing_key_file is not configured"
	case errors.Is(err, distribution.ErrInvalid):
		status, code, message = http.StatusBadRequest, "INVALID_MANIFEST", "invalid dependency-set manifest, identifier, or object declaration"
	case errors.Is(err, distribution.ErrNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "dependency set or declared object was not found"
	case errors.Is(err, distribution.ErrActive):
		status, code, message = http.StatusConflict, "SET_ACTIVE", "an active dependency set cannot be deleted"
	case errors.Is(err, distribution.ErrConflict):
		status, code, message = http.StatusConflict, "CONFLICT", "dependency-set revision conflicts with existing content"
	case errors.Is(err, distribution.ErrQuota):
		status, code, message = http.StatusRequestEntityTooLarge, "QUOTA_EXCEEDED", "distribution storage or object limit exceeded"
	case errors.Is(err, distribution.ErrIntegrity):
		status, code, message = http.StatusUnprocessableEntity, "INTEGRITY_FAILED", "object length or SHA-256 verification failed"
	case errors.Is(err, distribution.ErrIncomplete):
		status, code, message = http.StatusConflict, "SET_INCOMPLETE", "dependency set contains missing or invalid objects; inspect and upload the declared content"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status, code, message = http.StatusRequestTimeout, "REQUEST_CANCELED", "dependency-set operation was canceled or timed out"
	}
	// Storage errors can contain local paths, signed URLs or credential details.
	// Only fixed messages cross this HTTP interface.
	return status, code, message
}

func (h *DistributionHandler) audit(c *gin.Context, action, id string) {
	h.auditStatus(c, action, id, http.StatusOK)
}

func (h *DistributionHandler) auditStatus(c *gin.Context, action, id string, status int) {
	if h.db == nil {
		return
	}
	userAgent := c.Request.UserAgent()
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}
	entry := db.AuditLog{Action: action, PackageName: "distribution", Version: id,
		ClientIP: c.ClientIP(), UserAgent: userAgent, StatusCode: status, CreatedAt: time.Now().UTC()}
	if err := audit.AppendAuditRows(c.Request.Context(), h.db, []db.AuditLog{entry}); err != nil {
		zap.L().Warn("distribution: could not append lifecycle audit", zap.String("action", action))
	}
}
