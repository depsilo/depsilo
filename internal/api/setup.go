package api

import (
	"context"
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

// setupBusyAttempts bounds how often the administrator transaction runs while
// SQLite reports the database busy. Each attempt may already wait for the
// connection's busy_timeout, so the bound also caps how long a request waits.
const setupBusyAttempts = 4

// defaultListenPort is the port "depsilo setup token" asks when it is given
// no other; it matches the [server] port default.
const defaultListenPort = 23333

// SetupTokenCommand returns the command that prints the current bootstrap
// token of the instance listening on port. The bare command asks the default
// port, so any other port must be named or the answer comes from a different
// instance.
func SetupTokenCommand(port int) string {
	if port <= 0 || port == defaultListenPort {
		return "depsilo setup token"
	}
	return "depsilo setup token --port " + strconv.Itoa(port)
}

// bootstrapTokenRequiredMessage explains how to obtain and send the token. It
// depends only on server state, never on the rejected candidate, so a missing,
// stale, and wrong token stay indistinguishable.
func bootstrapTokenRequiredMessage(port int) string {
	return "A valid bootstrap token is required to complete initial setup. " +
		"Send it in the " + BootstrapTokenHeader + " header. " +
		"The token changes every time Depsilo restarts unless DEPSILO_BOOTSTRAP_TOKEN sets it; " +
		`run "` + SetupTokenCommand(port) + `" on the server to print the current one.`
}

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
	setupStageAfterConfigBackup    setupStage = "after_config_backup"
	setupStageAfterConfigWrite     setupStage = "after_config_write"
	setupStageAfterConfigTempWrite setupStage = "after_config_temp_write"
	setupStageAfterConfigTempSync  setupStage = "after_config_temp_sync"
	setupStageBeforeConfigRename   setupStage = "before_config_rename"
	setupStageAfterConfigRename    setupStage = "after_config_rename"
	setupStageAfterConfigDirSync   setupStage = "after_config_directory_sync"
)

type setupStageHook func(setupStage) error
type setupConfigWriter func(string, config.SetupRequest, config.WriteStageHook) error
type setupConfigBackup func(string) (string, error)

// SetupHandler handles first-run setup endpoints.
type SetupHandler struct {
	cfg             *config.Config
	db              *gorm.DB
	mu              sync.Mutex
	scheduleRestart func()
	writeConfig     setupConfigWriter
	backupConfig    setupConfigBackup
	stageHook       setupStageHook
	commit          func(*gorm.DB) error
	// configBackedUp records that this process already kept the config file it
	// found. A retried publication then neither collects one more copy per
	// attempt nor mistakes its own half-published document for the operator's.
	// Guarded by mu.
	configBackedUp bool
	// previousConfigKept records whether that step found a file to keep. Once
	// it has run, a file at the config path may be this process's own
	// half-published document, so the path alone no longer answers whether an
	// operator's configuration is being replaced. Guarded by mu.
	previousConfigKept bool
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
		backupConfig: backupConfigBeforeSetup,
		commit: func(tx *gorm.DB) error {
			return tx.Commit().Error
		},
	}
}

func backupConfigBeforeSetup(path string) (string, error) {
	return config.BackupConfigBeforeSetup(path, time.Now())
}

// NeedsSetup reports whether initial setup is still pending. Other handlers
// use it to tell clients that the instance is not ready to be configured for
// use yet.
func (h *SetupHandler) NeedsSetup() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.IsDefault
}

// Status returns whether the server needs initial setup.
func (h *SetupHandler) Status(c *gin.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	c.Header("Cache-Control", "no-store")
	status := gin.H{
		"needs_setup":    h.cfg.IsDefault,
		"token_required": h.cfg.IsDefault,
	}
	if h.cfg.IsDefault {
		// The wizard writes a port into config.toml and warns before replacing
		// an existing file. Both facts matter only while setup is pending; this
		// unauthenticated route discloses nothing else.
		status["listen_port"] = h.cfg.Server.Port
		configExists := config.SetupReplacesConfig(h.cfg)
		if h.configBackedUp {
			configExists = h.previousConfigKept
		}
		status["config_exists"] = configExists
	}
	c.JSON(http.StatusOK, status)
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
			"message": bootstrapTokenRequiredMessage(h.cfg.Server.Port),
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
	// Setup must not rotate the key selected at startup. A managed key remains
	// in its durable secret file; an explicit key is copied unchanged into the
	// generated config so removing an environment override cannot rotate it.
	req.Auth.JWTSecret = h.cfg.Auth.JWTSecret
	req.Auth.Managed = h.cfg.ManagedJWTSecretPath != ""

	// Commit the database before publishing config.toml. The config file is a
	// restart marker only after the administrator is durable.
	if failure := h.commitAdministrator(c.Request.Context(), req); failure != nil {
		c.JSON(failure.status, gin.H{
			"code":    failure.code,
			"message": failure.message,
		})
		return
	}
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
	// A reopened setup replaces a document the operator may have written by
	// hand. Keep a private copy beside it before publishing the generated one;
	// without the copy the existing file must stay exactly as it is.
	if !h.configBackedUp {
		backupConfig := h.backupConfig
		if backupConfig == nil {
			backupConfig = backupConfigBeforeSetup
		}
		backupPath, err := backupConfig(configPath)
		if err != nil {
			zap.L().Error("failed to keep the existing config before setup replaces it", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{
				"code":    "WRITE_FAILED",
				"message": "Failed to save configuration",
			})
			return
		}
		h.configBackedUp = true
		h.previousConfigKept = backupPath != ""
		if backupPath != "" {
			zap.L().Info("kept the previous config before setup replaces it", zap.String("backup_path", backupPath))
		}
	}
	if err := h.runStageHook(setupStageAfterConfigBackup); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "WRITE_FAILED",
			"message": "Failed to save configuration",
		})
		return
	}
	if err := writeConfig(configPath, req, configStageHook); err != nil {
		// The transaction is already durable. A writer error may happen after
		// an atomic rename, so never roll back the administrator here.
		zap.L().Error("failed to write config after administrator commit", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    "WRITE_FAILED",
			"message": "Failed to save configuration",
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
	if err := config.ClearBootstrapToken(configPath); err != nil {
		// Setup has already committed. The HTTP gate is closed and the next
		// configured startup retries cleanup, so do not turn this into a failed
		// setup response or attempt to roll back durable state.
		zap.L().Warn("failed to remove completed bootstrap token", zap.Error(err))
	}

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

// setupFailure ends a setup attempt with an error response. cause is the
// database error behind it, when there is one; logMessage, when set, is logged
// with the cause once the failure is final.
type setupFailure struct {
	status     int
	code       string
	message    string
	cause      error
	logMessage string
}

func adminCreateFailure(message string) *setupFailure {
	return &setupFailure{
		status:  http.StatusInternalServerError,
		code:    "ADMIN_CREATE_FAILED",
		message: message,
	}
}

// commitAdministrator makes the administrator durable. Background writers
// share the database while setup is pending (the first blocklist sync writes
// for tens of seconds), and a commit of theirs between this transaction's read
// and its first write fails it with SQLITE_BUSY however long busy_timeout is.
// Nothing is durable before the commit and every failed attempt is rolled
// back, so the whole transaction is simply run again on a fresh snapshot, a
// bounded number of times.
func (h *SetupHandler) commitAdministrator(ctx context.Context, req config.SetupRequest) *setupFailure {
	for attempt := 1; ; attempt++ {
		failure := h.commitAdministratorOnce(ctx, req)
		if failure == nil {
			return nil
		}
		if !db.IsSQLiteBusy(failure.cause) || attempt >= setupBusyAttempts {
			if failure.logMessage != "" {
				zap.L().Error(failure.logMessage, zap.Error(failure.cause))
			}
			return failure
		}
		zap.L().Warn("database is busy during setup; retrying the administrator transaction",
			zap.Int("attempt", attempt), zap.Error(failure.cause))
		timer := time.NewTimer(time.Duration(attempt) * 50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failure
		case <-timer.C:
		}
	}
}

// commitAdministratorOnce runs one administrator transaction. SQLite normally
// runs its pool in WAL/NORMAL mode for throughput; setup uses one pinned
// connection in FULL mode so a returned commit is also a power-loss barrier.
func (h *SetupHandler) commitAdministratorOnce(ctx context.Context, req config.SetupRequest) *setupFailure {
	tx, releaseDurableConnection, err := db.BeginDurableTransaction(ctx, h.db)
	if err != nil {
		failure := adminCreateFailure("Failed to start administrator creation")
		failure.cause = err
		return failure
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
				return &setupFailure{
					status:  http.StatusConflict,
					code:    "ADMIN_CREDENTIALS_MISMATCH",
					message: "An administrator already exists; enter its current credentials to recover the configuration",
				}
			}
		} else {
			failure := adminCreateFailure("Failed to create the initial administrator")
			failure.cause = err
			failure.logMessage = "failed to create initial administrator"
			return failure
		}
	} else {
		createdInitialAdmin = true
	}
	if createdInitialAdmin {
		if err := h.runStageHook(setupStageAfterAdminCreated); err != nil {
			return adminCreateFailure("Failed to complete the initial administrator setup")
		}
		if err := saveOnboardingStatus(ctx, tx, onboardingStatusNotStarted); err != nil {
			failure := adminCreateFailure("Failed to initialize the first-run experience")
			failure.cause = err
			failure.logMessage = "failed to initialize onboarding state"
			return failure
		}
		if err := h.runStageHook(setupStageAfterOnboardingSaved); err != nil {
			return adminCreateFailure("Failed to complete the first-run experience")
		}
	}

	if err := h.runStageHook(setupStageBeforeDBCommit); err != nil {
		return adminCreateFailure("Failed to commit the initial administrator")
	}
	commit := h.commit
	if commit == nil {
		commit = func(transaction *gorm.DB) error {
			return transaction.Commit().Error
		}
	}
	if err := commit(tx); err != nil {
		failure := adminCreateFailure("Failed to commit the initial administrator")
		failure.cause = err
		failure.logMessage = "failed to commit initial administrator"
		return failure
	}
	committed = true
	return nil
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
