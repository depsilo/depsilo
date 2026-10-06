package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/config"
)

// ComplianceHandler exposes the non-secret state of the CRA compliance
// profile so the Admin UI can explain what CRA exports will contain.
type ComplianceHandler struct {
	config config.ComplianceConfig
}

func NewComplianceHandler(compliance config.ComplianceConfig) *ComplianceHandler {
	return &ComplianceHandler{config: compliance}
}

func (h *ComplianceHandler) Profile(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"organization":               h.config.Organization,
		"contact":                    h.config.Contact,
		"signing_configured":         h.config.SigningKeyFile != "",
		"component_annotation_count": len(h.config.Components),
	})
}
