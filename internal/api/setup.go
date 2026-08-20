package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/config"
)

const BootstrapTokenHeader = "X-Depsilo-Bootstrap-Token"

const maxSetupRequestBodyBytes int64 = 1 << 20

const (
	setupRestartExec               = "exec"
	setupRestartSupervisorRequired = "supervisor_required"
)

// SetupHandler handles first-run setup endpoints.
type SetupHandler struct {
	cfg             *config.Config
	db              *gorm.DB
	mu              sync.Mutex
	scheduleRestart func()
}

// NewSetupHandler creates a new setup handler.
func NewSetupHandler(cfg *config.Config, database *gorm.DB) *SetupHandler {
	return &SetupHandler{cfg: cfg, db: database, scheduleRestart: scheduleSetupRestart}
}

// Status returns whether the server needs initial setup.
func (h *SetupHandler) Status(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"needs_setup":    h.cfg.IsDefault,
		"token_required": h.cfg.IsDefault,
	})
}

// Complete receives wizard data, writes config.toml, and restarts.
func (h *SetupHandler) Complete(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.cfg.IsDefault {
		c.JSON(http.StatusConflict, gin.H{
			"code":    "ALREADY_CONFIGURED",
			"message": "Server is already configured",
		})
		return
	}
	if !h.validBootstrapToken(c.GetHeader(BootstrapTokenHeader)) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    "BOOTSTRAP_TOKEN_REQUIRED",
			"message": "A valid bootstrap token is required to complete initial setup",
		})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxSetupRequestBodyBytes)
	var req config.SetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_BODY",
			"message": "Request body must be valid setup JSON",
		})
		return
	}

	if issue := validateAndNormalizeSetupRequest(&req); issue != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    issue.code,
			"message": issue.message,
		})
		return
	}
	if err := ValidateInitialAdminCredentials(req.Admin.Username, req.Admin.Password); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    "INVALID_ADMIN_CREDENTIALS",
			"message": err.Error(),
		})
		return
	}

	// Create the initial administrator and config as one setup operation. The
	// database transaction is rolled back if the durable config write fails.
	tx := h.db.Begin()
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "ADMIN_CREATE_FAILED",
			"message": "Failed to start administrator creation",
		})
		return
	}
	createdInitialAdmin := false
	if err := CreateInitialAdmin(tx, req.Admin.Username, req.Admin.Password); err != nil {
		if errors.Is(err, ErrInitialAdminExists) {
			if verifyErr := VerifyExistingAdminCredentials(tx, req.Admin.Username, req.Admin.Password); verifyErr != nil {
				tx.Rollback()
				c.JSON(http.StatusConflict, gin.H{
					"code":    "ADMIN_CREDENTIALS_MISMATCH",
					"message": "An administrator already exists; enter its current credentials to recover the configuration",
				})
				return
			}
		} else {
			tx.Rollback()
			zap.L().Error("failed to create initial administrator", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    "ADMIN_CREATE_FAILED",
				"message": "Failed to create the initial administrator",
			})
			return
		}
	} else {
		createdInitialAdmin = true
	}
	if createdInitialAdmin {
		if err := saveOnboardingStatus(c.Request.Context(), tx, onboardingStatusNotStarted); err != nil {
			tx.Rollback()
			zap.L().Error("failed to initialize onboarding state", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    "ADMIN_CREATE_FAILED",
				"message": "Failed to initialize the first-run experience",
			})
			return
		}
	}

	configPath := h.cfg.ConfigPath
	if err := config.WriteConfig(configPath, req); err != nil {
		tx.Rollback()
		zap.L().Error("failed to write config", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "WRITE_FAILED",
			"message": err.Error(),
		})
		return
	}
	if err := tx.Commit().Error; err != nil {
		if removeErr := os.Remove(configPath); removeErr != nil && !os.IsNotExist(removeErr) {
			zap.L().Error("failed to remove config after administrator commit failure", zap.Error(removeErr))
		}
		zap.L().Error("failed to commit initial administrator", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "ADMIN_CREATE_FAILED",
			"message": "Failed to commit the initial administrator",
		})
		return
	}

	h.cfg.IsDefault = false
	h.cfg.BootstrapToken = ""

	zap.L().Info("setup complete, config written", zap.String("path", configPath))

	restartStrategy := setupRestartStrategy(runtime.GOOS)
	message := "Configuration saved. Server restarting..."
	if restartStrategy == setupRestartSupervisorRequired {
		message = "Configuration saved. Restart the server process to apply it."
	}
	c.JSON(http.StatusOK, gin.H{
		"status":           "ok",
		"message":          message,
		"reconnect_url":    setupReconnectURL(c.Request, req.Server.Port),
		"restart_strategy": restartStrategy,
	})

	if restartStrategy == setupRestartExec {
		h.scheduleRestart()
	}
}

func (h *SetupHandler) validBootstrapToken(candidate string) bool {
	if h.cfg.BootstrapToken == "" || candidate == "" {
		return false
	}
	expectedDigest := sha256.Sum256([]byte(h.cfg.BootstrapToken))
	candidateDigest := sha256.Sum256([]byte(strings.TrimSpace(candidate)))
	return subtle.ConstantTimeCompare(expectedDigest[:], candidateDigest[:]) == 1
}

func setupRestartStrategy(goos string) string {
	if goos == "windows" {
		return setupRestartSupervisorRequired
	}
	return setupRestartExec
}

func setupReconnectURL(request *http.Request, port int) string {
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	}
	hostname := (&url.URL{Host: request.Host}).Hostname()
	if hostname == "" {
		hostname = "localhost"
	}
	return (&url.URL{
		Scheme: scheme,
		Host:   net.JoinHostPort(hostname, strconv.Itoa(port)),
		Path:   "/",
	}).String()
}

func scheduleSetupRestart() {
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
