package admin

import (
	"context"
	"crypto/rand"
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

	npmadapter "depsilo/internal/adapter/npm"
	pypiadapter "depsilo/internal/adapter/pypi"
	"depsilo/internal/asyncruntime"
	"depsilo/internal/cache"
	"depsilo/internal/config"
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
)

type WarmupHandler struct {
	cacheMgr *cache.Manager
	pools    map[string]*upstream.Pool
	cfg      *config.Config
	tasks    asyncruntime.Submitter
	running  atomic.Bool
	jobsMu   sync.Mutex
	jobs     map[string]*warmupJob
}

type warmupJob struct {
	mu        sync.RWMutex
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

// NewWarmupHandler binds warmup work to the server's async runtime.
func NewWarmupHandler(tasks asyncruntime.Submitter, cacheMgr *cache.Manager, pools map[string]*upstream.Pool, cfg *config.Config) *WarmupHandler {
	return &WarmupHandler{
		cacheMgr: cacheMgr,
		pools:    pools,
		cfg:      cfg,
		tasks:    tasks,
		jobs:     make(map[string]*warmupJob),
	}
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

	pool, ok := h.pools[body.Ecosystem]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "the selected ecosystem is not active"})
		return
	}
	if !h.running.CompareAndSwap(false, true) {
		c.JSON(http.StatusConflict, gin.H{"code": "WARMUP_RUNNING", "message": "a cache warmup is already in progress"})
		return
	}
	release := func() { h.running.Store(false) }

	total := len(packages)
	if h.tasks == nil {
		release()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SERVER_SHUTTING_DOWN", "message": "cache warmup is unavailable"})
		return
	}
	jobIDBytes := make([]byte, 12)
	if _, err := rand.Read(jobIDBytes); err != nil {
		release()
		c.JSON(http.StatusInternalServerError, gin.H{"code": "WARMUP_UNAVAILABLE", "message": "could not create warmup job"})
		return
	}
	now := time.Now().UTC()
	principal, _ := middleware.PrincipalFromContext(c)
	job := &warmupJob{ID: fmt.Sprintf("%x", jobIDBytes), Principal: principal.ID, Ecosystem: body.Ecosystem, Status: "queued", CreatedAt: now, UpdatedAt: now, Items: make([]warmupItem, len(packages)), cancel: func() {}}
	for i, pkg := range packages {
		job.Items[i] = warmupItem{Package: pkg, Status: "queued"}
	}
	jobCtx, cancelJob := context.WithCancel(context.Background())
	job.cancel = cancelJob
	h.jobsMu.Lock()
	h.pruneWarmupJobs(time.Now().UTC())
	if len(h.jobs) >= maxWarmupJobs {
		h.jobsMu.Unlock()
		cancelJob()
		release()
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "WARMUP_QUEUE_FULL", "message": "too many warmup jobs"})
		return
	}
	h.jobs[job.ID] = job
	h.jobsMu.Unlock()
	if err := h.tasks.Submit(func(ctx context.Context) {
		defer release()
		defer cancelJob()
		taskCtx, cancel := context.WithCancel(ctx)
		go func() { <-jobCtx.Done(); cancel() }()
		h.runWarmupJob(taskCtx, job, body.Ecosystem, packages, pool)
		cancel()
	}); err != nil {
		cancelJob()
		h.jobsMu.Lock()
		delete(h.jobs, job.ID)
		h.jobsMu.Unlock()
		release()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "SERVER_SHUTTING_DOWN", "message": "cache warmup is unavailable"})
		return
	}

	zap.L().Info("cache warmup started",
		zap.String("ecosystem", body.Ecosystem),
		zap.Int("packages", total),
	)

	c.JSON(http.StatusAccepted, gin.H{
		"message":  "warmup started",
		"packages": total,
		"job_id":   job.ID,
		"status":   "queued",
	})
}

func (h *WarmupHandler) pruneWarmupJobs(now time.Time) {
	for id, existing := range h.jobs {
		existing.mu.RLock()
		terminal, updated := warmupTerminal(existing.Status), existing.UpdatedAt
		existing.mu.RUnlock()
		if terminal && now.Sub(updated) >= warmupJobRetention {
			delete(h.jobs, id)
		}
	}
}

// Status returns a bounded, persisted-for-process-lifetime view of a warmup.
func (h *WarmupHandler) Status(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("id"))
	principal, principalOK := middleware.PrincipalFromContext(c)
	h.jobsMu.Lock()
	job, ok := h.jobs[jobID]
	h.jobsMu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": "WARMUP_NOT_FOUND", "message": "warmup job not found"})
		return
	}
	if !principalOK || job.Principal != principal.ID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "warmup job belongs to another operator"})
		return
	}
	job.mu.RLock()
	items := append([]warmupItem(nil), job.Items...)
	response := gin.H{"job_id": job.ID, "ecosystem": job.Ecosystem, "status": job.Status, "created_at": job.CreatedAt, "updated_at": job.UpdatedAt, "packages": len(items), "items": items}
	job.mu.RUnlock()
	c.JSON(http.StatusOK, response)
}

// Cancel requests cooperative cancellation; completed jobs remain unchanged.
func (h *WarmupHandler) Cancel(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("id"))
	principal, principalOK := middleware.PrincipalFromContext(c)
	h.jobsMu.Lock()
	job, ok := h.jobs[jobID]
	h.jobsMu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": "WARMUP_NOT_FOUND", "message": "warmup job not found"})
		return
	}
	if !principalOK || !principal.CanWrite || job.Principal != principal.ID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "write permission for this warmup job is required"})
		return
	}
	job.mu.Lock()
	if job.Status == "queued" || job.Status == "running" {
		job.Status = "cancelling"
		job.UpdatedAt = time.Now().UTC()
		job.cancel()
	}
	status := job.Status
	job.mu.Unlock()
	c.JSON(http.StatusAccepted, gin.H{"job_id": job.ID, "status": status})
}

// Retry re-submits only failed inputs from a completed job through the normal
// admission path; it cannot add new packages or bypass policy checks.
func (h *WarmupHandler) Retry(c *gin.Context) {
	jobID := strings.TrimSpace(c.Param("id"))
	principal, principalOK := middleware.PrincipalFromContext(c)
	h.jobsMu.Lock()
	job, ok := h.jobs[jobID]
	h.jobsMu.Unlock()
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": "WARMUP_NOT_FOUND", "message": "warmup job not found"})
		return
	}
	if !principalOK || !principal.CanWrite || job.Principal != principal.ID {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "write permission for this warmup job is required"})
		return
	}
	job.mu.RLock()
	if !warmupTerminal(job.Status) {
		status := job.Status
		job.mu.RUnlock()
		c.JSON(http.StatusConflict, gin.H{"code": "WARMUP_NOT_TERMINAL", "message": "warmup must finish before retrying", "status": status})
		return
	}
	failed := make([]string, 0, len(job.Items))
	for _, item := range job.Items {
		if item.Status == "failed" {
			failed = append(failed, item.Package)
		}
	}
	ecosystem := job.Ecosystem
	job.mu.RUnlock()
	if len(failed) == 0 {
		c.JSON(http.StatusConflict, gin.H{"code": "WARMUP_NO_FAILED_ITEMS", "message": "warmup has no failed items to retry"})
		return
	}
	body, err := json.Marshal(map[string]any{"ecosystem": ecosystem, "packages": failed})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "WARMUP_UNAVAILABLE", "message": "could not create retry request"})
		return
	}
	c.Request.Body = io.NopCloser(strings.NewReader(string(body)))
	c.Request.ContentLength = int64(len(body))
	h.Warmup(c)
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
		job.mu.Lock()
		if job.Status != "cancelling" {
			job.Status = "running"
		}
		job.UpdatedAt = time.Now().UTC()
		job.mu.Unlock()
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
			if job != nil {
				h.updateWarmupItem(job, pkg, "failed", "request failed")
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
			if job != nil {
				h.updateWarmupItem(job, pkg, "failed", "request failed")
			}
			zap.L().Warn("warmup fetch failed",
				zap.String("package", pkg),
				zap.String("ecosystem", ecosystem),
				zap.Error(err),
			)
		} else {
			if job != nil {
				h.updateWarmupItem(job, pkg, "succeeded", "metadata cached")
			}
			zap.L().Debug("warmup cached",
				zap.String("package", pkg),
				zap.String("ecosystem", ecosystem),
			)
		}
	}
	if job != nil {
		job.mu.RLock()
		status := job.Status
		job.mu.RUnlock()
		if warmupContext.Err() != nil || status == "cancelling" {
			terminalStatus := "interrupted"
			if status == "cancelling" {
				terminalStatus = "cancelled"
			}
			h.finishWarmupJob(job, terminalStatus)
		} else if status != "interrupted" {
			h.finishWarmupJob(job, "succeeded")
		}
	}

	zap.L().Info("cache warmup completed",
		zap.String("ecosystem", ecosystem),
		zap.Int("packages", len(packages)),
	)
}

func (h *WarmupHandler) updateWarmupItem(job *warmupJob, pkg, status, detail string) {
	job.mu.Lock()
	defer job.mu.Unlock()
	for i := range job.Items {
		if job.Items[i].Package == pkg {
			job.Items[i].Status, job.Items[i].Detail = status, detail
			break
		}
	}
	job.UpdatedAt = time.Now().UTC()
}

func (h *WarmupHandler) finishWarmupJob(job *warmupJob, status string) {
	job.mu.Lock()
	for i := range job.Items {
		if (status == "cancelled" || status == "interrupted") && (job.Items[i].Status == "queued" || job.Items[i].Status == "running") {
			job.Items[i].Status = status
			job.Items[i].Detail = "warmup stopped before this package completed"
		}
	}
	if status == "succeeded" {
		for _, item := range job.Items {
			if item.Status == "failed" {
				status = "partial"
				break
			}
		}
	}
	job.Status, job.UpdatedAt = status, time.Now().UTC()
	job.mu.Unlock()
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
