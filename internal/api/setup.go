package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
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
	"depsilo/internal/db"
)

const BootstrapTokenHeader = "X-Depsilo-Bootstrap-Token"

const maxSetupRequestBodyBytes int64 = 1 << 20

const (
	setupRestartExec               = "exec"
	setupRestartSupervisorRequired = "supervisor_required"
)

// setupStage identifies a durable boundary in the first-run setup protocol.
// The hook is nil in production; tests use it to model an abrupt process stop
// at a boundary without terminating the test process itself.
type setupStage string

const (
	setupStageAfterAdminCreated    setupStage = "after_admin_created"
	setupStageAfterOnboardingSaved setupStage = "after_onboarding_saved"
	setupStageBeforeDBCommit       setupStage = "before_db_commit"
	setupStageAfterDBCommit        setupStage = "after_db_commit"
	setupStageAfterConfigWrite     setupStage = "after_config_write"
	setupStageAfterConfigTempWrite setupStage = "after_config_temp_write"
	setupStageAfterConfigTempSync  setupStage = "after_config_temp_sync"
	setupStageBeforeConfigRename   setupStage = "before_config_rename"
	setupStageAfterConfigRename    setupStage = "after_config_rename"
	setupStageAfterConfigDirSync   setupStage = "after_config_directory_sync"
)

type setupStageHook func(setupStage) error
type setupConfigWriter func(string, config.SetupRequest, config.WriteStageHook) error

// SetupHandler handles first-run setup endpoints.
type SetupHandler struct {
	cfg             *config.Config
	db              *gorm.DB
	mu              sync.Mutex
	scheduleRestart func()
	writeConfig     setupConfigWriter
	stageHook       setupStageHook
	commit          func(*gorm.DB) error
}

// NewSetupHandler creates a new setup handler.
func NewSetupHandler(cfg *config.Config, database *gorm.DB) *SetupHandler {
	return &SetupHandler{
		cfg:             cfg,
		db:              database,
		scheduleRestart: scheduleSetupRestart,
		writeConfig: func(path string, req config.SetupRequest, hook config.WriteStageHook) error {
			if hook == nil {
				return config.WriteConfig(path, req)
			}
			return config.WriteConfigWithStageHook(path, req, hook)
		},
		commit: func(tx *gorm.DB) error {
			return tx.Commit().Error
		},
	}
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
	// Carry the effective database target into the generated document. The
	// browser never supplies these hidden fields; they bind the file published
	// after commit to the same database transaction that just succeeded,
	// including installations using DEPSILO_DATABASE_DSN.
	req.Database.Driver = h.cfg.Database.Driver
	req.Database.DSN = h.cfg.Database.DSN

	// Commit the database before publishing config.toml. The config file is a
	// restart marker only after the administrator is durable. SQLite normally
	// runs its pool in WAL/NORMAL mode for throughput; setup uses one pinned
	// connection in FULL mode so a returned commit is also a power-loss barrier.
	tx, releaseDurableConnection, err := db.BeginDurableTransaction(c.Request.Context(), h.db)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "ADMIN_CREATE_FAILED",
			"message": "Failed to start administrator creation",
		})
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback().Error
		}
		if releaseErr := releaseDurableConnection(); releaseErr != nil {
			zap.L().Warn("failed to release durable setup connection", zap.Error(releaseErr))
		}
	}()
	createdInitialAdmin := false
	if err := CreateInitialAdmin(tx, req.Admin.Username, req.Admin.Password); err != nil {
		if errors.Is(err, ErrInitialAdminExists) {
			if verifyErr := VerifyExistingAdminCredentials(tx, req.Admin.Username, req.Admin.Password); verifyErr != nil {
				c.JSON(http.StatusConflict, gin.H{
					"code":    "ADMIN_CREDENTIALS_MISMATCH",
					"message": "An administrator already exists; enter its current credentials to recover the configuration",
				})
				return
			}
		} else {
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
		if err := h.runStageHook(setupStageAfterAdminCreated); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    "ADMIN_CREATE_FAILED",
				"message": "Failed to complete the initial administrator setup",
			})
			return
		}
		if err := saveOnboardingStatus(c.Request.Context(), tx, onboardingStatusNotStarted); err != nil {
			zap.L().Error("failed to initialize onboarding state", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    "ADMIN_CREATE_FAILED",
				"message": "Failed to initialize the first-run experience",
			})
			return
		}
		if err := h.runStageHook(setupStageAfterOnboardingSaved); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    "ADMIN_CREATE_FAILED",
				"message": "Failed to complete the first-run experience",
			})
			return
		}
	}

	if err := h.runStageHook(setupStageBeforeDBCommit); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "ADMIN_CREATE_FAILED",
			"message": "Failed to commit the initial administrator",
		})
		return
	}
	commit := h.commit
	if commit == nil {
		commit = func(transaction *gorm.DB) error {
			return transaction.Commit().Error
		}
	}
	if err := commit(tx); err != nil {
		zap.L().Error("failed to commit initial administrator", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "ADMIN_CREATE_FAILED",
			"message": "Failed to commit the initial administrator",
		})
		return
	}
	committed = true
	if err := h.runStageHook(setupStageAfterDBCommit); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "ADMIN_CREATE_FAILED",
			"message": "Failed to complete setup",
		})
		return
	}

	configPath := h.cfg.ConfigPath
	writeConfig := h.writeConfig
	if writeConfig == nil {
		writeConfig = func(path string, request config.SetupRequest, hook config.WriteStageHook) error {
			if hook == nil {
				return config.WriteConfig(path, request)
			}
			return config.WriteConfigWithStageHook(path, request, hook)
		}
	}
	var configStageHook config.WriteStageHook
	if h.stageHook != nil {
		configStageHook = func(stage config.WriteStage) error {
			mapped, err := setupStageForConfigWrite(stage)
			if err != nil {
				return err
			}
			return h.runStageHook(mapped)
		}
	}
	if err := writeConfig(configPath, req, configStageHook); err != nil {
		// The transaction is already durable. A writer error may happen after
		// an atomic rename, so never roll back the administrator here.
		zap.L().Error("failed to write config after administrator commit", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "WRITE_FAILED",
			"message": err.Error(),
		})
		return
	}
	if err := h.runStageHook(setupStageAfterConfigWrite); err != nil {
		// This is a post-commit fault. Leave durable state untouched so restart
		// can use either the written config or the interactive recovery path.
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "WRITE_FAILED",
			"message": "Failed to complete setup",
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

func (h *SetupHandler) runStageHook(stage setupStage) error {
	if h.stageHook == nil {
		return nil
	}
	return h.stageHook(stage)
}

func setupStageForConfigWrite(stage config.WriteStage) (setupStage, error) {
	switch stage {
	case config.WriteStageAfterTempWrite:
		return setupStageAfterConfigTempWrite, nil
	case config.WriteStageAfterTempSync:
		return setupStageAfterConfigTempSync, nil
	case config.WriteStageBeforeRename:
		return setupStageBeforeConfigRename, nil
	case config.WriteStageAfterRename:
		return setupStageAfterConfigRename, nil
	case config.WriteStageAfterDirectorySync:
		return setupStageAfterConfigDirSync, nil
	default:
		return "", fmt.Errorf("unknown config publication stage %d", stage)
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
