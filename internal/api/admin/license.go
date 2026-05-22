package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"depsilo/internal/entitlement"
	"depsilo/internal/license"
	"depsilo/internal/trial"
)

// LicenseHandler handles license-and-trial status, key mutation, and trial activation.
type LicenseHandler struct {
	lic     *license.Manager
	trial   *trial.Manager
	checker *entitlement.Checker
}

// NewLicenseHandler creates the handler.
func NewLicenseHandler(lic *license.Manager, t *trial.Manager, c *entitlement.Checker) *LicenseHandler {
	return &LicenseHandler{lic: lic, trial: t, checker: c}
}

// GetStatus returns the unified entitlement status.
func (h *LicenseHandler) GetStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.checker.Status())
}

// Revalidate triggers a paid-license re-validation in the background.
func (h *LicenseHandler) Revalidate(c *gin.Context) {
	h.lic.Revalidate()
	c.JSON(http.StatusOK, gin.H{"message": "revalidation triggered"})
}

// ActivateTrial creates the singleton TrialRecord. Refuses if a paid license
// is already active (TRIAL_NOT_NEEDED) or a trial was already used (TRIAL_ALREADY_USED).
func (h *LicenseHandler) ActivateTrial(c *gin.Context) {
	// Block when user is already paid Pro.
	if h.checker.Status().Source == entitlement.SourcePaid {
		c.JSON(http.StatusConflict, gin.H{"code": "TRIAL_NOT_NEEDED"})
		return
	}

	userID := uint(0)
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(uint); ok {
			userID = id
		}
	}

	_, err := h.trial.Activate(c.Request.Context(), userID, c.ClientIP())
	switch {
	case errors.Is(err, trial.ErrTrialAlreadyUsed):
		c.JSON(http.StatusConflict, gin.H{"code": "TRIAL_ALREADY_USED"})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
	default:
		c.JSON(http.StatusOK, h.checker.Status())
	}
}

// SetKeyRequest is the PUT /key body.
type SetKeyRequest struct {
	Key string `json:"key" binding:"required,max=256"`
}

// SetKey persists a license key and triggers a synchronous validation.
// Always returns 200 with the updated Status; the frontend reads status.is_pro
// to determine success vs saved-pending.
func (h *LicenseHandler) SetKey(c *gin.Context) {
	var req SetKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_BODY", "message": err.Error()})
		return
	}
	req.Key = strings.TrimSpace(req.Key)
	if req.Key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "INVALID_KEY", "message": "key must be non-empty"})
		return
	}

	userID := uint(0)
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(uint); ok {
			userID = id
		}
	}

	// Persist + synchronous validate. Validation network failures are NOT
	// surfaced as 5xx; status.license_error captures them.
	_ = h.lic.SetKey(c.Request.Context(), req.Key, userID)
	c.JSON(http.StatusOK, h.checker.Status())
}

// ClearKey deletes the DB-stored key (if any) and resets state.
func (h *LicenseHandler) ClearKey(c *gin.Context) {
	userID := uint(0)
	if v, ok := c.Get("user_id"); ok {
		if id, ok := v.(uint); ok {
			userID = id
		}
	}
	if err := h.lic.ClearKey(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, h.checker.Status())
}
