package admin

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
)

type SettingsHandler struct {
	cfg *config.Config
}

func NewSettingsHandler(cfg *config.Config) *SettingsHandler {
	return &SettingsHandler{cfg: cfg}
}

func (h *SettingsHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"server": gin.H{
			"host":      h.cfg.Server.Host,
			"port":      h.cfg.Server.Port,
			"log_level": h.cfg.Server.LogLevel,
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

// Update applies hot-reloadable settings changes at runtime.
// Fields that require restart (host, port, DB driver, storage type)
// are not accepted here.
func (h *SettingsHandler) Update(c *gin.Context) {
	var body struct {
		Cache *struct {
			MaxSizeGB    *int    `json:"max_size_gb"`
			TTLIndex     *string `json:"ttl_index"`
			TTLBlob      *string `json:"ttl_blob"`
			LRUThreshold *int    `json:"lru_threshold"`
		} `json:"cache"`
		Server *struct {
			LogLevel *string `json:"log_level"`
		} `json:"server"`
		Auth *struct {
			Enabled  *bool   `json:"enabled"`
			TokenTTL *string `json:"token_ttl"`
		} `json:"auth"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	updated := []string{}

	// Cache settings — hot reloadable
	if body.Cache != nil {
		if body.Cache.MaxSizeGB != nil {
			h.cfg.Cache.MaxSizeGB = *body.Cache.MaxSizeGB
			updated = append(updated, "cache.max_size_gb")
		}
		if body.Cache.TTLIndex != nil {
			if d, err := time.ParseDuration(*body.Cache.TTLIndex); err == nil {
				h.cfg.Cache.TTLIndex = d
				updated = append(updated, "cache.ttl_index")
			}
		}
		if body.Cache.TTLBlob != nil {
			if d, err := time.ParseDuration(*body.Cache.TTLBlob); err == nil {
				h.cfg.Cache.TTLBlob = d
				updated = append(updated, "cache.ttl_blob")
			}
		}
		if body.Cache.LRUThreshold != nil {
			h.cfg.Cache.LRUThreshold = *body.Cache.LRUThreshold
			updated = append(updated, "cache.lru_threshold")
		}
	}

	// Log level — hot reloadable
	if body.Server != nil && body.Server.LogLevel != nil {
		h.cfg.Server.LogLevel = *body.Server.LogLevel
		// Apply log level change immediately
		level, err := zap.ParseAtomicLevel(*body.Server.LogLevel)
		if err == nil {
			zap.L().Core().Enabled(level.Level())
		}
		updated = append(updated, "server.log_level")
	}

	// Auth settings — hot reloadable
	if body.Auth != nil {
		if body.Auth.Enabled != nil {
			h.cfg.Auth.Enabled = *body.Auth.Enabled
			updated = append(updated, "auth.enabled")
		}
		if body.Auth.TokenTTL != nil {
			if d, err := time.ParseDuration(*body.Auth.TokenTTL); err == nil {
				h.cfg.Auth.TokenTTL = d
				updated = append(updated, "auth.token_ttl")
			}
		}
	}

	zap.L().Info("settings updated", zap.Strings("fields", updated))

	c.JSON(http.StatusOK, gin.H{
		"message": "settings updated",
		"updated": updated,
	})
}

type LogsHandler struct {
}
