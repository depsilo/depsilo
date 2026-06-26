package api

import (
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
)

// SetupHandler handles first-run setup endpoints.
type SetupHandler struct {
	cfg *config.Config
}

// NewSetupHandler creates a new setup handler.
func NewSetupHandler(cfg *config.Config) *SetupHandler {
	return &SetupHandler{cfg: cfg}
}

// Status returns whether the server needs initial setup.
func (h *SetupHandler) Status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"needs_setup": h.cfg.IsDefault,
		"config_path": h.cfg.ConfigPath,
	})
}

// Complete receives wizard data, writes config.toml, and restarts.
func (h *SetupHandler) Complete(c *gin.Context) {
	if !h.cfg.IsDefault {
		c.JSON(http.StatusConflict, gin.H{
			"code":    "ALREADY_CONFIGURED",
			"message": "Server is already configured",
		})
		return
	}

	var req config.SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_BODY",
			"message": err.Error(),
		})
		return
	}

	// Validate
	if req.Server.Port < 1 || req.Server.Port > 65535 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_PORT",
			"message": "Port must be between 1 and 65535",
		})
		return
	}
	if req.Storage.Path == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_STORAGE_PATH",
			"message": "Storage path is required",
		})
		return
	}

	// Check at least one ecosystem is enabled
	hasEcosystem := false
	for _, eco := range req.Ecosystems {
		if eco.Enabled {
			hasEcosystem = true
			break
		}
	}
	if !hasEcosystem {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "NO_ECOSYSTEM",
			"message": "At least one ecosystem must be enabled",
		})
		return
	}

	// Write config file
	configPath := h.cfg.ConfigPath
	if err := config.WriteConfig(configPath, req); err != nil {
		zap.L().Error("failed to write config", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "WRITE_FAILED",
			"message": err.Error(),
		})
		return
	}

	zap.L().Info("setup complete, config written", zap.String("path", configPath))

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"message": "Configuration saved. Server restarting...",
	})

	// Delayed in-place restart to allow the HTTP response to be sent first.
	// We re-exec the current binary (same PID, same args + env) instead of
	// os.Exit(0): a plain exit would leave the server dead unless an external
	// supervisor restarts it (docker restart-policy / systemd), which is not the
	// case for `make dev` or a foreground run. The re-exec'd process reloads the
	// freshly written config, so IsDefault becomes false and it starts normally.
	// The listening socket has CLOEXEC set, so it is released on exec and the new
	// process rebinds the same port (Go sets SO_REUSEADDR).
	go func() {
		time.Sleep(1 * time.Second)
		zap.L().Info("restarting server after setup...")
		exe, err := os.Executable()
		if err != nil {
			zap.L().Error("cannot resolve executable path for restart; exiting instead", zap.Error(err))
			os.Exit(0)
		}
		if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
			zap.L().Error("re-exec failed; exiting instead", zap.Error(err))
			os.Exit(0)
		}
	}()
}
