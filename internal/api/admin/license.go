package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/license"
)

// LicenseHandler handles license status and revalidation API endpoints.
type LicenseHandler struct {
	mgr *license.Manager
}

// NewLicenseHandler creates a new LicenseHandler.
func NewLicenseHandler(mgr *license.Manager) *LicenseHandler {
	return &LicenseHandler{mgr: mgr}
}

// GetStatus returns the current license status.
func (h *LicenseHandler) GetStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.mgr.Status())
}

// Revalidate triggers a manual license re-validation.
func (h *LicenseHandler) Revalidate(c *gin.Context) {
	h.mgr.Revalidate()
	c.JSON(http.StatusOK, gin.H{"message": "revalidation triggered"})
}
