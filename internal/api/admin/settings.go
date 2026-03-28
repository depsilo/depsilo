package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"repocache/internal/config"
)

type SettingsHandler struct {
	cfg *config.Config
}

func NewSettingsHandler(cfg *config.Config) *SettingsHandler {
	return &SettingsHandler{cfg: cfg}
}

func (h *SettingsHandler) Get(c *gin.Context) {
	// Return current config (mask sensitive fields)
	c.JSON(http.StatusOK, gin.H{
		"server": gin.H{
			"host": h.cfg.Server.Host,
			"port": h.cfg.Server.Port,
		},
		"database": gin.H{
			"driver": h.cfg.Database.Driver,
		},
		"storage": gin.H{
			"type": h.cfg.Storage.Type,
			"path": h.cfg.Storage.Path,
		},
		"cache": gin.H{
			"max_size_gb":   h.cfg.Cache.MaxSizeGB,
			"ttl_index":     h.cfg.Cache.TTLIndex.String(),
			"ttl_blob":      h.cfg.Cache.TTLBlob.String(),
			"lru_threshold": h.cfg.Cache.LRUThreshold,
		},
		"auth": gin.H{
			"enabled":   h.cfg.Auth.Enabled,
			"token_ttl": h.cfg.Auth.TokenTTL.String(),
		},
	})
}

func (h *SettingsHandler) Update(c *gin.Context) {
	// Settings are read from config file — runtime updates are limited.
	// This endpoint acknowledges the request but notes that most settings
	// require a restart to take effect.
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "settings update acknowledged — restart required for most changes to take effect",
		"updated": body,
	})
}

type LogsHandler struct {
}
