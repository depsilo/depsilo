package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"depsilo/internal/api/contract"
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
	c.JSON(http.StatusOK, contract.ComplianceProfile{
		Organization:             h.config.Organization,
		Contact:                  h.config.Contact,
		SigningConfigured:        h.config.SigningKeyFile != "",
		ComponentAnnotationCount: len(h.config.Components),
	})
}
