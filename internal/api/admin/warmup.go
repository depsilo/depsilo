package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	npmadapter "depsilo/internal/adapter/npm"
	pypiadapter "depsilo/internal/adapter/pypi"
	"depsilo/internal/asyncruntime"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	depsdb "depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/packagepolicy"
	"depsilo/internal/upstream"
)

const (
	maxWarmupPackages     = 100
	maxWarmupPackageBytes = 256
	maxWarmupMetadataSize = 8 << 20
	maxWarmupJobs         = 32
	warmupJobRetention    = 24 * time.Hour
	maxWarmupDuration     = 10 * time.Minute
	maxWarmupHistoryBytes = 2 << 20
	warmupHistoryTimeout  = 5 * time.Second
)

type WarmupHandler struct {
	cacheMgr   *cache.Manager
	pools      map[string]*upstream.Pool
	cfg        *config.Config
	tasks      asyncruntime.Submitter
	running    atomic.Bool
	jobsMu     sync.Mutex
	jobs       map[string]*warmupJob
	db         *gorm.DB
	historyErr error
}

type warmupJob struct {
	ID        string
	Principal uint
	Ecosystem string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
	Items     []warmupItem
	cancel    context.CancelFunc
}

type warmupItem struct {
	Package string `json:"package"`
	Status  string `json:"status"`
	Detail  string `json:"detail,omitempty"`
}

// NewWarmupHandler binds warmup work and its bounded history to one server.
// A history failure disables this handler until restart; it must never replace
// unreadable durable history with an empty map.
func NewWarmupHandler(tasks asyncruntime.Submitter, cacheMgr *cache.Manager, pools map[string]*upstream.Pool, cfg *config.Config, database *gorm.DB) *WarmupHandler {
	h := &WarmupHandler{cacheMgr: cacheMgr, pools: pools, cfg: cfg, tasks: tasks, db: database, jobs: make(map[string]*warmupJob)}
	if err := h.loadPersistedJobs(); err != nil {
		h.failHistoryLocked(err)
	}
	return h
}

const warmupStateKey = "admin.warmup.jobs"

type durableWarmupState struct {
	Jobs []durableWarmupJob `json:"jobs"`
}
type durableWarmupJob struct {
	ID        string       `json:"id"`
	Principal uint         `json:"principal"`
	Ecosystem string       `json:"ecosystem"`
	Status    string       `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
	Items     []warmupItem `json:"items"`
}

// Called before the handler is published. Existing state is validated in full
// before any recovery or retention change is persisted.
func (h *WarmupHandler) loadPersistedJobs() error {
	if h.db == nil {
		return nil
	} // isolated unit callers can run the fetch seam without an HTTP job store
	ctx, cancel := context.WithTimeout(context.Background(), warmupHistoryTimeout)
	defer cancel()
	var state depsdb.ControlPlaneState
	// SQLite is the deployment authority. Read at most the byte budget plus
	// one sentinel byte, including when an old or corrupt value is oversized.
	err := h.db.WithContext(ctx).Select("key, substr(CAST(value AS BLOB), 1, ?) AS value", maxWarmupHistoryBytes+1).
		Where("key = ?", warmupStateKey).First(&state).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var saved durableWarmupState
	if len(state.Value) > maxWarmupHistoryBytes {
		return errors.New("warmup history exceeds limit")
	}
	if err := json.Unmarshal([]byte(state.Value), &saved); err != nil {
		return err
	}
	if saved.Jobs == nil || len(saved.Jobs) > maxWarmupJobs {
		return errors.New("invalid warmup history")
	}
	jobs := make(map[string]*warmupJob, len(saved.Jobs))
	changed := false
	now := time.Now().UTC()
	for _, item := range saved.Jobs {
		if err := validateWarmupHistoryJob(item); err != nil {
			return err
		}
		if _, exists := jobs[item.ID]; exists {
			return errors.New("duplicate warmup history ID")
		}
		if !warmupTerminal(item.Status) {
			item.Status, item.UpdatedAt = "interrupted", now
			for i := range item.Items {
				if item.Items[i].Status == "queued" || item.Items[i].Status == "running" {
					item.Items[i].Status = "interrupted"
					item.Items[i].Detail = "service restarted before this package completed"
				}
			}
			changed = true
		}
		jobs[item.ID] = &warmupJob{ID: item.ID, Principal: item.Principal, Ecosystem: item.Ecosystem, Status: item.Status, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt, Items: item.Items, cancel: func() {}}
	}
	h.jobs = jobs
	if h.pruneWarmupJobs(now) {
		changed = true
	}
	if changed {
		return h.saveHistoryLocked()
	}
	return nil
}

func validateWarmupHistoryJob(job durableWarmupJob) error {
	id, err := hex.DecodeString(job.ID)
	if err != nil || len(id) != 12 || job.Principal == 0 || job.CreatedAt.IsZero() || job.UpdatedAt.Before(job.CreatedAt) ||
		(!warmupTerminal(job.Status) && job.Status != "queued" && job.Status != "running" && job.Status != "cancelling") {
		return errors.New("invalid warmup history job")
	}
	packages := make([]string, len(job.Items))
	for i, item := range job.Items {
		packages[i] = item.Package
		switch item.Status {
		case "queued", "running", "succeeded", "failed", "cancelled", "interrupted":
		default:
			return errors.New("invalid warmup history item status")
		}
		switch item.Detail {
		case "", "metadata cached", "request failed", "warmup stopped before this package completed", "service restarted before this package completed":
		default:
			return errors.New("invalid warmup history item detail")
		}
	}
	normalized, err := normalizeWarmupPackages(job.Ecosystem, packages)
	if err != nil || len(normalized) != len(packages) {
		return errors.New("invalid warmup history packages")
	}
	for i, name := range normalized {
		if name != packages[i] {
			return errors.New("non-normalized warmup history package")
		}
	}
	return nil
}

// jobsMu covers both state transitions and their SQLite commit. Readers never
// observe a successful transition before the corresponding history write.
func (h *WarmupHandler) saveHistoryLocked() error {
	if h.historyErr != nil {
		return h.historyErr
	}
	if h.db == nil {
		return nil
	}
	saved := durableWarmupState{Jobs: make([]durableWarmupJob, 0, len(h.jobs))}
	for _, job := range h.jobs {
		saved.Jobs = append(saved.Jobs, durableWarmupJob{ID: job.ID, Principal: job.Principal, Ecosystem: job.Ecosystem, Status: job.Status, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt, Items: job.Items})
	}
	b, err := json.Marshal(saved)
	if err == nil && len(b) > maxWarmupHistoryBytes {
		err = errors.New("warmup history exceeds limit")
	}
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), warmupHistoryTimeout)
		defer cancel()
		state := depsdb.ControlPlaneState{Key: warmupStateKey, Value: string(b), UpdatedAt: time.Now().UTC()}
		err = h.db.WithContext(ctx).Save(&state).Error
	}
	if err != nil {
		h.failHistoryLocked(err)
	}
	return err
}

// persistJobs is retained for focused package tests and non-HTTP callers.
func (h *WarmupHandler) persistJobs() error {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	return h.saveHistoryLocked()
}

func (h *WarmupHandler) failHistoryLocked(err error) {
	h.historyErr = err
	// Raw database errors may contain the serialized package history.
	zap.L().Warn("warmup history unavailable; warmup operations disabled until restart")
}

func (h *WarmupHandler) historyReadyLocked(c *gin.Context) bool {
	if h.historyErr == nil {
		return true
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"code": "WARMUP_HISTORY_UNAVAILABLE", "message": "warmup history is unavailable; repair storage and restart the service"})
	return false
}

// Warmup accepts a list of packages and pre-fetches their index into cache.
// POST /api/v1/admin/cache/warmup
// Body: { "ecosystem": "pypi", "packages": ["numpy", "requests", "torch"] }
func (h *WarmupHandler) Warmup(c *gin.Context) {
	var body struct {
		Ecosystem string   `json:"ecosystem" binding:"required"`
		Packages  []string `json:"packages" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "request body must contain an ecosystem and package list"})
		return
	}
	body.Ecosystem = strings.ToLower(strings.TrimSpace(body.Ecosystem))
	packages, err := normalizeWarmupPackages(body.Ecosystem, body.Packages)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	h.startWarmup(c, body.Ecosystem, packages)
}

func (h *WarmupHandler) startWarmup(c *gin.Context, ecosystem string, packages []string) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || principal.ID == 0 || !principal.CanWrite {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "write permission is required"})
		return
	}
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if !h.historyReadyLocked(c) {
		return
	}
	pool, ok := h.pools[ecosystem]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "the selected ecosystem is not active"})
		return
	}
	if !h.running.CompareAndSwap(false, true) {
		c.JSON(http.StatusConflict, gin.H{"code": "WARMUP_RUNNING", "message": "a cache warmup is already in progress"})
		return
	}
	release := func() { h.running.Store(false) }
	if h.tasks == nil {
		release()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SERVER_SHUTTING_DOWN", "message": "cache warmup is unavailable"})
		return
	}
	if h.pruneWarmupJobs(time.Now().UTC()) {
		if err := h.saveHistoryLocked(); err != nil {
			release()
			h.historyReadyLocked(c)
			return
		}
	}
	if len(h.jobs) >= maxWarmupJobs {
		release()
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "WARMUP_QUEUE_FULL", "message": "too many warmup jobs"})
		return
	}
	jobIDBytes := make([]byte, 12)
	if _, err := rand.Read(jobIDBytes); err != nil {
		release()
		c.JSON(http.StatusInternalServerError, gin.H{"code": "WARMUP_UNAVAILABLE", "message": "could not create warmup job"})
		return
	}
	now := time.Now().UTC()
	jobCtx, cancelJob := context.WithCancel(context.Background())
	job := &warmupJob{ID: hex.EncodeToString(jobIDBytes), Principal: principal.ID, Ecosystem: ecosystem, Status: "queued", CreatedAt: now, UpdatedAt: now, Items: make([]warmupItem, len(packages)), cancel: cancelJob}
	for i, pkg := range packages {
		job.Items[i] = warmupItem{Package: pkg, Status: "queued"}
	}
	h.jobs[job.ID] = job
	if h.saveHistoryLocked() != nil {
		cancelJob()
		delete(h.jobs, job.ID)
		release()
		h.historyReadyLocked(c)
		return
	}
	if err := h.tasks.Submit(func(ctx context.Context) {
		defer release()
		defer cancelJob()
		taskCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		stop := context.AfterFunc(jobCtx, cancel)
		defer stop()
		h.runWarmupJob(taskCtx, job, ecosystem, packages, pool)
	}); err != nil {
		cancelJob()
		delete(h.jobs, job.ID)
		err = h.saveHistoryLocked()
		release()
		if err != nil {
			h.historyReadyLocked(c)
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SERVER_SHUTTING_DOWN", "message": "cache warmup is unavailable"})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"message": "warmup started", "packages": len(packages), "job_id": job.ID, "status": "queued"})
}

// Caller holds jobsMu, or is constructing an unpublished handler.
func (h *WarmupHandler) pruneWarmupJobs(now time.Time) bool {
	removed := false
	for id, job := range h.jobs {
		if warmupTerminal(job.Status) && now.Sub(job.UpdatedAt) >= warmupJobRetention {
			delete(h.jobs, id)
			removed = true
		}
	}
	if len(h.jobs) >= maxWarmupJobs {
		var oldestID string
		var oldest time.Time
		for id, job := range h.jobs {
			if !warmupTerminal(job.Status) {
				continue
			}
			if oldestID == "" || job.UpdatedAt.Before(oldest) {
				oldestID, oldest = id, job.UpdatedAt
			}
		}
		if oldestID != "" {
			delete(h.jobs, oldestID)
			removed = true
		}
	}
	return removed
}

// Status reads a committed, bounded view of a job owned by this principal.
func (h *WarmupHandler) Status(c *gin.Context) {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if !h.historyReadyLocked(c) {
		return
	}
	job := h.ownedJobLocked(c, false)
	if job == nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"job_id": job.ID, "ecosystem": job.Ecosystem, "status": job.Status, "created_at": job.CreatedAt, "updated_at": job.UpdatedAt, "packages": len(job.Items), "items": job.Items})
}

func (h *WarmupHandler) ownedJobLocked(c *gin.Context, write bool) *warmupJob {
	job := h.jobs[strings.TrimSpace(c.Param("id"))]
	if job == nil || (warmupTerminal(job.Status) && time.Since(job.UpdatedAt) >= warmupJobRetention) {
		c.JSON(http.StatusNotFound, gin.H{"code": "WARMUP_NOT_FOUND", "message": "warmup job not found"})
		return nil
	}
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || principal.ID != job.Principal || (write && !principal.CanWrite) {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "permission for this warmup job is required"})
		return nil
	}
	return job
}

// Cancel requests cooperative cancellation; published objects remain cached.
func (h *WarmupHandler) Cancel(c *gin.Context) {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if !h.historyReadyLocked(c) {
		return
	}
	job := h.ownedJobLocked(c, true)
	if job == nil {
		return
	}
	if job.Status == "queued" || job.Status == "running" {
		job.Status, job.UpdatedAt = "cancelling", time.Now().UTC()
		job.cancel()
		if h.saveHistoryLocked() != nil {
			h.historyReadyLocked(c)
			return
		}
	}
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "status": job.Status})
}

// Retry uses the same admission path and only the original failed inputs.
func (h *WarmupHandler) Retry(c *gin.Context) {
	h.jobsMu.Lock()
	if !h.historyReadyLocked(c) {
		h.jobsMu.Unlock()
		return
	}
	job := h.ownedJobLocked(c, true)
	if job == nil {
		h.jobsMu.Unlock()
		return
	}
	if !warmupTerminal(job.Status) {
		c.JSON(http.StatusConflict, gin.H{"code": "WARMUP_NOT_TERMINAL", "message": "warmup must finish before retrying"})
		h.jobsMu.Unlock()
		return
	}
	failed := make([]string, 0, len(job.Items))
	for _, item := range job.Items {
		if item.Status == "failed" {
			failed = append(failed, item.Package)
		}
	}
	ecosystem := job.Ecosystem
	h.jobsMu.Unlock()
	if len(failed) == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "WARMUP_NO_FAILED_ITEMS", "message": "warmup has no failed items to retry"})
		return
	}
	h.startWarmup(c, ecosystem, failed)
}

func (h *WarmupHandler) doWarmup(parent context.Context, ecosystem string, packages []string, pool *upstream.Pool) {
	h.runWarmupJob(parent, nil, ecosystem, packages, pool)
}

func (h *WarmupHandler) runWarmupJob(parent context.Context, job *warmupJob, ecosystem string, packages []string, pool *upstream.Pool) {
	normalized, err := normalizeWarmupPackages(ecosystem, packages)
	if err != nil {
		zap.L().Warn("cache warmup rejected", zap.String("ecosystem", ecosystem), zap.Error(err))
		return
	}
	packages = normalized
	if job != nil {
		h.jobsMu.Lock()
		if h.historyErr != nil {
			h.jobsMu.Unlock()
			return
		}
		if job.Status == "cancelling" {
			h.finishWarmupJobLocked(job, "cancelled")
			h.jobsMu.Unlock()
			return
		}
		job.Status, job.UpdatedAt = "running", time.Now().UTC()
		if h.saveHistoryLocked() != nil {
			h.jobsMu.Unlock()
			return
		}
		h.jobsMu.Unlock()
	}
	warmupContext, cancelWarmup := context.WithTimeout(parent, maxWarmupDuration)
	defer cancelWarmup()
	selector := upstream.NewPassiveRecoverySelector(pool)
	ttl := h.cfg.Cache.TTLIndex

	for _, pkg := range packages {
		if warmupContext.Err() != nil {
			break
		}
		cacheKey, upstreamPath, err := warmupTarget(ecosystem, pkg)
		if err != nil {
			if job != nil && !h.updateWarmupItem(job, pkg, "failed", "request failed") {
				return
			}
			zap.L().Warn("warmup target invalid", zap.String("ecosystem", ecosystem), zap.Error(err))
			continue
		}

		ctx, cancel := context.WithTimeout(warmupContext, 2*time.Minute)
		err = h.cacheMgr.Prefetch(ctx, cacheKey, ecosystem, ttl, func(fetchCtx context.Context) (io.ReadCloser, string, int64, string, error) {
			ups, err := selector.Select(fetchCtx)
			if err != nil {
				return nil, "", 0, "", err
			}
			result, err := ups.Fetch(fetchCtx, upstreamPath)
			if err != nil {
				return nil, "", 0, ups.Name, err
			}
			if result.Body == nil {
				return nil, "", 0, ups.Name, errors.New("upstream returned an empty body")
			}
			body, readErr := io.ReadAll(io.LimitReader(result.Body, maxWarmupMetadataSize+1))
			closeErr := result.Body.Close()
			if err := errors.Join(readErr, closeErr); err != nil {
				return nil, "", 0, ups.Name, err
			}
			if len(body) > maxWarmupMetadataSize {
				return nil, "", 0, ups.Name, errors.New("upstream metadata exceeds warmup limit")
			}
			var rewritten []byte
			if ecosystem == "npm" {
				rewritten, err = npmadapter.PreparePackument(
					body,
					pkg,
					result.URL,
					ups.ProvenanceSourceID(),
				)
			} else {
				rewritten, err = rewriteWarmupIndex(ecosystem, body)
			}
			if err != nil {
				return nil, "", 0, ups.Name, err
			}
			contentType := result.ContentType
			if contentType == "" {
				if ecosystem == "npm" {
					contentType = "application/json"
				} else {
					contentType = "text/html"
				}
			}
			return io.NopCloser(strings.NewReader(string(rewritten))), contentType, int64(len(rewritten)), ups.Name, nil
		})
		cancel()

		if err != nil {
			if job != nil && !h.updateWarmupItem(job, pkg, "failed", "request failed") {
				return
			}
			zap.L().Warn("warmup fetch failed",
				zap.String("package", pkg),
				zap.String("ecosystem", ecosystem),
				zap.Error(err),
			)
		} else {
			if job != nil && !h.updateWarmupItem(job, pkg, "succeeded", "metadata cached") {
				return
			}
			zap.L().Debug("warmup cached",
				zap.String("package", pkg),
				zap.String("ecosystem", ecosystem),
			)
		}
	}
	if job != nil {
		h.jobsMu.Lock()
		status := job.Status
		if warmupContext.Err() != nil || status == "cancelling" {
			terminalStatus := "interrupted"
			if status == "cancelling" {
				terminalStatus = "cancelled"
			}
			h.finishWarmupJobLocked(job, terminalStatus)
		} else if status != "interrupted" {
			h.finishWarmupJobLocked(job, "succeeded")
		}
		h.jobsMu.Unlock()
	}

	zap.L().Info("cache warmup completed",
		zap.String("ecosystem", ecosystem),
		zap.Int("packages", len(packages)),
	)
}

func (h *WarmupHandler) updateWarmupItem(job *warmupJob, pkg, status, detail string) bool {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	if h.historyErr != nil {
		return false
	}
	for i := range job.Items {
		if job.Items[i].Package == pkg {
			job.Items[i].Status, job.Items[i].Detail = status, detail
			break
		}
	}
	job.UpdatedAt = time.Now().UTC()
	return h.saveHistoryLocked() == nil
}

func (h *WarmupHandler) finishWarmupJob(job *warmupJob, status string) {
	h.jobsMu.Lock()
	defer h.jobsMu.Unlock()
	h.finishWarmupJobLocked(job, status)
}

func (h *WarmupHandler) finishWarmupJobLocked(job *warmupJob, status string) {
	for i := range job.Items {
		if (status == "cancelled" || status == "interrupted") && (job.Items[i].Status == "queued" || job.Items[i].Status == "running") {
			job.Items[i].Status, job.Items[i].Detail = status, "warmup stopped before this package completed"
		}
	}
	if status == "succeeded" {
		succeeded, failed := 0, 0
		for _, item := range job.Items {
			switch item.Status {
			case "succeeded":
				succeeded++
			case "failed":
				failed++
			}
		}
		switch {
		case succeeded == len(job.Items):
			status = "succeeded"
		case succeeded == 0 && failed == len(job.Items):
			status = "failed"
		default:
			status = "partial"
		}
	}
	job.Status, job.UpdatedAt = status, time.Now().UTC()
	_ = h.saveHistoryLocked()
}

func warmupTerminal(status string) bool {
	switch status {
	case "succeeded", "partial", "failed", "interrupted", "cancelled":
		return true
	default:
		return false
	}
}

func normalizeWarmupPackages(ecosystem string, raw []string) ([]string, error) {
	if ecosystem != "pypi" && ecosystem != "npm" {
		return nil, errors.New("cache warmup currently supports only PyPI and npm")
	}
	if len(raw) == 0 {
		return nil, errors.New("at least one package is required")
	}
	if len(raw) > maxWarmupPackages {
		return nil, fmt.Errorf("package list exceeds the %d item limit", maxWarmupPackages)
	}
	dialect, err := packagepolicy.DialectFor(ecosystem)
	if err != nil {
		return nil, err
	}

	packages := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" || strings.HasPrefix(name, "#") || strings.HasPrefix(name, "-") {
			continue
		}
		if ecosystem == "pypi" {
			name = stripPyPISpecifier(name)
		} else {
			name = stripNPMSpecifier(name)
		}
		name = strings.TrimSpace(name)
		if len(name) > maxWarmupPackageBytes {
			return nil, fmt.Errorf("package names must be at most %d bytes", maxWarmupPackageBytes)
		}
		canonical, err := dialect.NormalizePackageName(name)
		if err != nil {
			return nil, fmt.Errorf("invalid %s package name %q: %w", ecosystem, name, err)
		}
		if _, duplicate := seen[canonical]; duplicate {
			continue
		}
		seen[canonical] = struct{}{}
		packages = append(packages, name)
	}
	if len(packages) == 0 {
		return nil, errors.New("at least one valid package is required")
	}
	return packages, nil
}

func stripPyPISpecifier(name string) string {
	for _, separator := range []string{"==", ">=", "<=", "!=", "~=", ">", "<", "["} {
		if index := strings.Index(name, separator); index > 0 {
			name = name[:index]
		}
	}
	return name
}

func stripNPMSpecifier(name string) string {
	if strings.HasPrefix(name, "@") {
		slash := strings.Index(name, "/")
		if slash > 1 {
			if version := strings.LastIndex(name, "@"); version > slash {
				return name[:version]
			}
		}
		return name
	}
	if version := strings.Index(name, "@"); version > 0 {
		return name[:version]
	}
	return name
}

func warmupTarget(ecosystem, pkg string) (cacheKey, upstreamPath string, err error) {
	switch ecosystem {
	case "pypi":
		return pypiadapter.IndexCacheKey("pypi", pkg), "/simple/" + pkg + "/", nil
	case "npm":
		if strings.HasPrefix(pkg, "@") {
			parts := strings.SplitN(strings.TrimPrefix(pkg, "@"), "/", 2)
			if len(parts) != 2 {
				return "", "", errors.New("invalid scoped npm package")
			}
			return npmadapter.ScopedMetadataCacheKey(parts[0], parts[1]), "/" + pkg, nil
		}
		return npmadapter.MetadataCacheKey(pkg), "/" + pkg, nil
	default:
		return "", "", errors.New("unsupported warmup ecosystem")
	}
}

func rewriteWarmupIndex(ecosystem string, body []byte) ([]byte, error) {
	switch ecosystem {
	case "pypi":
		return []byte(pypiadapter.RewriteURLs(string(body), "", "/pypi")), nil
	default:
		return nil, errors.New("unsupported warmup ecosystem")
	}
}
