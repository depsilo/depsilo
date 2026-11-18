package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/api/contract"
	"depsilo/internal/cache"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/traffic"
	"depsilo/internal/upstreamupdates"
)

type CacheHandler struct {
	transferReader func() []traffic.Transfer
	db             *gorm.DB
	retention      *cache.Retention
	maxSizeGB      int
	indexRefresher upstreamupdates.Refresher
	plansMu        sync.Mutex
	plans          map[string]cleanupPlan
}

type cleanupPlan struct {
	principalID         uint
	expiresAt           time.Time
	items               []cache.PreviewEntry
	totalCandidateCount int64
	plannedBytes        int64
}

// cacheIndexSummary is the scan target of the per-ecosystem index summary.
type cacheIndexSummary struct {
	AdapterType string
	Total       int64
	Fresh       int64
	Stale       int64
	LastUpdated *string
}

// cacheContractSlice converts each item to its contract type. A nil slice
// stays nil, so a body field that used to encode as null still does.
func cacheContractSlice[T, U any](items []T, convert func(T) U) []U {
	if items == nil {
		return nil
	}
	converted := make([]U, len(items))
	for i, item := range items {
		converted[i] = convert(item)
	}
	return converted
}

// ListIndexes returns mutable package-manager indexes separately from cached
// artifacts. cache_kind is persisted by cache.Manager and legacy rows are
// classified during migration, so this query does not depend on key guessing.
// Two kinds of metadata are no index and stay out: opaque Hugging Face query
// answers and the relayed Go checksum database. The checksum prefix is
// compared exactly, as the Go handler does; LIKE would fold its case.
func (h *CacheHandler) ListIndexes(c *gin.Context) {
	page := parseIntParam(c, "page", 1, 1, 100000)
	pageSize := parseIntParam(c, "page_size", 50, 1, 200)
	search := strings.TrimSpace(c.Query("search"))
	adapterTypes, err := parseEcosystemFilter(c, "adapter_type")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && status != "fresh" && status != "stale" {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "status must be fresh or stale"})
		return
	}

	now := time.Now().UTC()
	// Ecosystem counts reuse every filter except the ecosystem one.
	filtered := func(withEcosystems bool) *gorm.DB {
		query := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{}).
			Where("cache_kind = ?", db.CacheKindMetadata).
			Where("NOT (adapter_type = ? AND key LIKE ?)", "huggingface", "huggingface/__query__/%").
			Where("NOT (adapter_type = ? AND substr(key, 1, ?) = ?)", "go", len(db.GoSumDBKeyPrefix), db.GoSumDBKeyPrefix)
		if search != "" {
			like := "%" + search + "%"
			query = query.Where("package_name LIKE ? OR key LIKE ?", like, like)
		}
		if withEcosystems && len(adapterTypes) > 0 {
			query = query.Where("adapter_type IN ?", adapterTypes)
		}
		if status == "fresh" {
			query = query.Where("expires_at > ?", now)
		} else if status == "stale" {
			query = query.Where("expires_at <= ?", now)
		}
		return query
	}
	query := filtered(true)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	var rows []db.CacheEntry
	if err := query.Order("adapter_type ASC, package_name ASC, last_accessed DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	items := make([]contract.CacheIndexEntry, len(rows))
	for i, row := range rows {
		_, refreshErr := CacheIndexPublicPath(row)
		rowStatus := contract.CacheIndexStatusStale
		if row.ExpiresAt.After(now) {
			rowStatus = contract.CacheIndexStatusFresh
		}
		items[i] = contract.CacheIndexEntry{
			ID: row.ID, Key: row.Key, AdapterType: row.AdapterType,
			PackageName: row.PackageName, Size: row.Size, HitCount: row.HitCount,
			ETag: row.ETag, LastModified: row.LastModified,
			LastAccessed: row.LastAccessed, ExpiresAt: row.ExpiresAt,
			UpdatedAt: row.UpdatedAt, Status: rowStatus, RefreshSupported: refreshErr == nil && h.indexRefresher != nil,
		}
	}

	var summary []cacheIndexSummary
	if err := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{}).
		Select(`adapter_type, COUNT(*) AS total,
			SUM(CASE WHEN expires_at > ? THEN 1 ELSE 0 END) AS fresh,
			SUM(CASE WHEN expires_at <= ? THEN 1 ELSE 0 END) AS stale,
			MAX(updated_at) AS last_updated`, now, now).
		Where("cache_kind = ?", db.CacheKindMetadata).
		Where("NOT (adapter_type = ? AND key LIKE ?)", "huggingface", "huggingface/__query__/%").
		Where("NOT (adapter_type = ? AND substr(key, 1, ?) = ?)", "go", len(db.GoSumDBKeyPrefix), db.GoSumDBKeyPrefix).
		Group("adapter_type").Order("adapter_type ASC").Scan(&summary).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}

	ecosystems, err := countByEcosystem(filtered(false), "adapter_type")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, contract.CacheIndexListResponse{
		Items: items, Total: total, Page: page, PageSize: pageSize,
		Summary: cacheContractSlice(summary, func(row cacheIndexSummary) contract.CacheIndexSummary {
			return contract.CacheIndexSummary{
				AdapterType: row.AdapterType, Total: row.Total, Fresh: row.Fresh,
				Stale: row.Stale, LastUpdated: row.LastUpdated,
			}
		}),
		Ecosystems: ecosystems,
	})
}

func NewCacheHandler(database *gorm.DB, retention *cache.Retention, maxSizeGB int) *CacheHandler {
	return &CacheHandler{db: database, retention: retention, maxSizeGB: maxSizeGB, plans: make(map[string]cleanupPlan)}
}

// SetIndexRefresher configures the callback used by RefreshIndex. It is
// expected to be called once while routes are being assembled.
func (h *CacheHandler) SetIndexRefresher(refresher upstreamupdates.Refresher) {
	h.indexRefresher = refresher
}

// RefreshIndex refreshes a single mutable package index immediately.
func (h *CacheHandler) RefreshIndex(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var entry db.CacheEntry
	if err := h.db.WithContext(c.Request.Context()).First(&entry, uint(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "cache entry not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"code": "INTERNAL", "message": err.Error()})
		return
	}
	if entry.CacheKind != db.CacheKindMetadata || goChecksumDatabaseEntry(entry) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"code": "NOT_REFRESHABLE", "message": "only index metadata can be refreshed",
		})
		return
	}
	if h.indexRefresher == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"code": "INDEX_REFRESH_UNAVAILABLE", "message": "index refresh is not configured",
		})
		return
	}
	if _, err := h.indexRefresher(c.Request.Context(), entry); err != nil {
		zap.L().Warn("manual index refresh failed",
			zap.Uint("cache_entry_id", entry.ID),
			zap.String("adapter_type", entry.AdapterType),
			zap.String("error_type", fmt.Sprintf("%T", err)),
		)
		c.JSON(http.StatusBadGateway, gin.H{
			"code": "INDEX_REFRESH_FAILED", "message": "index refresh failed; inspect server logs",
		})
		return
	}

	c.JSON(http.StatusOK, contract.CacheIndexRefreshResponse{Message: "index refreshed", ID: entry.ID})
}

// CacheIndexPublicPath maps a persisted cache key back to the public GET route
// that originally populated it. Only standard mutable index shapes are
// accepted; artifacts, custom adapter IDs and unsafe paths are rejected.
func CacheIndexPublicPath(entry db.CacheEntry) (string, error) {
	if entry.CacheKind != db.CacheKindMetadata {
		return "", fmt.Errorf("cache entry %d is not index metadata", entry.ID)
	}

	var publicPath string

	switch entry.AdapterType {
	case "pypi":
		const prefix, suffix = "pypi/simple/", "/index.html"
		if !strings.HasPrefix(entry.Key, prefix) || !strings.HasSuffix(entry.Key, suffix) {
			return "", unsupportedIndexKey(entry)
		}
		pkg := strings.TrimSuffix(strings.TrimPrefix(entry.Key, prefix), suffix)
		if pkg == "" || strings.Contains(pkg, "/") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/pypi/simple/" + pkg + "/"

	case "npm":
		const suffix = "/metadata.json"
		prefix := "npm/"
		if strings.HasPrefix(entry.Key, packagekey.NPMExactIdentityCachePrefix) {
			prefix = packagekey.NPMExactIdentityCachePrefix
		}
		if !strings.HasPrefix(entry.Key, prefix) || !strings.HasSuffix(entry.Key, suffix) {
			return "", unsupportedIndexKey(entry)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(entry.Key, prefix), suffix)
		parts := strings.Split(name, "/")
		validScopedName := len(parts) == 2 && len(parts[0]) > 1 &&
			strings.HasPrefix(parts[0], "@") && parts[1] != ""
		if name == "" || (len(parts) != 1 && !validScopedName) {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/npm/" + name

	case "go":
		rest, ok := trimCachePrefix(entry.Key, "go/")
		if !ok || goChecksumDatabaseEntry(entry) {
			return "", unsupportedIndexKey(entry)
		}
		if !strings.HasSuffix(rest, "/@v/list") && !strings.HasSuffix(rest, "/@latest") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/go/" + rest

	case "cargo":
		switch {
		case entry.Key == "cargo/config.json":
			publicPath = "/crates/config.json"
		case strings.HasPrefix(entry.Key, "cargo/index/"):
			rest, ok := trimCachePrefix(entry.Key, "cargo/index/")
			if !ok {
				return "", unsupportedIndexKey(entry)
			}
			publicPath = "/crates/" + rest
		default:
			return "", unsupportedIndexKey(entry)
		}

	case "maven", "rubygems", "composer", "nuget", "conda", "cran", "alpine", "helm":
		prefix := entry.AdapterType + "/"
		rest, ok := trimCachePrefix(entry.Key, prefix)
		if !ok {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/" + entry.AdapterType + "/" + rest

	case "huggingface":
		rest, ok := trimCachePrefix(entry.Key, "huggingface/")
		if !ok ||
			(!strings.HasPrefix(rest, "api/models/") && !strings.HasPrefix(rest, "api/datasets/")) ||
			strings.HasPrefix(rest, "__query__/") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/huggingface/" + rest

	case "apt":
		rest, ok := trimCachePrefix(entry.Key, "apt/")
		if !ok {
			return "", unsupportedIndexKey(entry)
		}
		repo, storedPath, ok := strings.Cut(rest, "/")
		if !ok || repo == "" || storedPath == "" {
			return "", unsupportedIndexKey(entry)
		}
		// CacheKey(repo, upstreamPath) stores both repo and the full
		// upstream path, so normal keys contain repo twice.
		if !strings.HasPrefix(storedPath, repo+"/") {
			// Older normalized keys do not round-trip through the current APT
			// handler (which stores the repo twice). Reject them instead of
			// refreshing a different cache row and reporting false success.
			return "", fmt.Errorf("legacy APT index key %q must be re-cached before manual refresh", entry.Key)
		}
		storedPath = strings.TrimPrefix(storedPath, repo+"/")
		if !strings.HasPrefix(storedPath, "dists/") && !strings.HasPrefix(storedPath, "pool/") {
			return "", unsupportedIndexKey(entry)
		}
		publicPath = "/apt/" + repo + "/" + storedPath

	default:
		return "", fmt.Errorf("index refresh is not supported for adapter %q", entry.AdapterType)
	}

	if err := validateInternalProxyPath(publicPath); err != nil {
		return "", fmt.Errorf("unsafe cache index path for %q: %w", entry.Key, err)
	}
	return publicPath, nil
}

// goChecksumDatabaseEntry reports whether entry holds an answer of the Go
// checksum database relay. Such an entry is cached as metadata, but it is
// verification data the go command asks for by itself, not an index.
func goChecksumDatabaseEntry(entry db.CacheEntry) bool {
	return entry.AdapterType == "go" && strings.HasPrefix(entry.Key, db.GoSumDBKeyPrefix)
}

func trimCachePrefix(key, prefix string) (string, bool) {
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(key, prefix)
	return rest, rest != ""
}

func unsupportedIndexKey(entry db.CacheEntry) error {
	return fmt.Errorf("unsupported %s index cache key %q", entry.AdapterType, entry.Key)
}

func validateInternalProxyPath(path string) error {
	if path == "" || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return errors.New("invalid absolute path")
	}
	decoded := path
	for {
		if strings.ContainsAny(decoded, "\\?#\x00\r\n") {
			return errors.New("path contains a forbidden character")
		}
		unescaped, err := url.PathUnescape(decoded)
		if err != nil {
			return errors.New("path contains invalid escaping")
		}
		if unescaped == decoded {
			break
		}
		decoded = unescaped
	}
	if strings.Contains(decoded, "//") {
		return errors.New("path contains an empty segment")
	}
	for _, r := range decoded {
		if r < 0x20 || r == 0x7f {
			return errors.New("path contains a control character")
		}
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return errors.New("path traversal is not allowed")
		}
	}
	return nil
}

func (h *CacheHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	search := c.Query("search")
	adapterTypes, err := parseEcosystemFilter(c, "adapter_type", "type")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}
	kind := c.Query("cache_kind")
	if kind != "" && kind != string(db.CacheKindArtifact) && kind != string(db.CacheKindMetadata) {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid cache kind"})
		return
	}

	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize

	// Ecosystem counts reuse every filter except the ecosystem one.
	filtered := func(withEcosystems bool) *gorm.DB {
		query := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{})
		if kind != "" {
			query = query.Where("cache_kind = ?", kind)
		}
		if search != "" {
			query = query.Where("key LIKE ?", "%"+search+"%")
		}
		if withEcosystems && len(adapterTypes) > 0 {
			query = query.Where("adapter_type IN ?", adapterTypes)
		}
		return query
	}
	query := filtered(true)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		h.readError(c, err)
		return
	}

	var entries []db.CacheEntry
	if err := query.Order("last_accessed DESC").Offset(offset).Limit(pageSize).Find(&entries).Error; err != nil {
		h.readError(c, err)
		return
	}

	ecosystems, err := countByEcosystem(filtered(false), "adapter_type")
	if err != nil {
		h.readError(c, err)
		return
	}

	c.JSON(http.StatusOK, contract.CacheListResponse{
		Total: total, Page: page, PageSize: pageSize,
		Items: cacheContractSlice(entries, func(entry db.CacheEntry) contract.CacheEntry {
			return contract.CacheEntry{
				ID: entry.ID, Key: entry.Key, AdapterType: entry.AdapterType,
				CacheKind: contract.CacheKind(entry.CacheKind), PackageName: entry.PackageName,
				StoragePath: entry.StoragePath, Size: entry.Size, HitCount: entry.HitCount,
				ContentType: entry.ContentType, ETag: entry.ETag, LastModified: entry.LastModified,
				ExpiresAt: entry.ExpiresAt, LastAccessed: entry.LastAccessed,
				CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt,
			}
		}),
		Ecosystems: ecosystems,
	})
}

func (h *CacheHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	removal, err := h.retention.Remove(c.Request.Context(), uint(id))
	switch {
	case err == nil:
		c.JSON(http.StatusOK, contract.CacheDeleteResponse{
			Message: "deleted", Deleted: 1, ReclaimedBytes: removal.ReclaimedBytes,
		})
	case errors.Is(err, cache.ErrCacheEntryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "cache entry not found"})
	default:
		zap.L().Error("remove cache entry",
			zap.Uint64("cache_entry_id", id),
			zap.Bool("object_removed", removal.ObjectRemoved),
			zap.Bool("metadata_removed", removal.MetadataRemoved),
			zap.Int64("reclaimed_bytes", removal.ReclaimedBytes),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":             "CACHE_REMOVE_INCOMPLETE",
			"message":          "cache entry removal did not complete",
			"deleted":          0,
			"failed":           1,
			"reclaimed_bytes":  removal.ReclaimedBytes,
			"object_removed":   removal.ObjectRemoved,
			"metadata_removed": removal.MetadataRemoved,
		})
	}
}

func (h *CacheHandler) Cleanup(c *gin.Context) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || principal.ID == 0 || !principal.CanWrite {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "write permission is required"})
		return
	}
	planID, err := parseCleanupRequest(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "cleanup body must be empty or contain exactly one nonempty plan_id string"})
		return
	}
	if h.retention == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "CACHE_CLEANUP_UNAVAILABLE", "message": "cache cleanup is not configured"})
		return
	}
	if planID != "" {
		h.cleanupPlan(c, planID)
		return
	}
	report, err := h.retention.Reclaim(c.Request.Context(), cache.ReclaimModeManual)
	if err != nil {
		zap.L().Error("manual cache cleanup", zap.Error(err))
		code := "CACHE_CLEANUP_PARTIAL"
		message := "cache cleanup did not complete"
		if errors.Is(err, cache.ErrReclaimTargetNotReached) {
			code = "CACHE_RECLAIM_TARGET_NOT_REACHED"
			message = "cache cleanup incomplete because storage remains above target"
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":            code,
			"message":         message,
			"deleted":         report.Removed,
			"failed":          report.Failed,
			"reclaimed_bytes": report.ReclaimedBytes,
			"examined":        report.Examined,
			"expired_removed": report.ExpiredRemoved,
			"lru_removed":     report.LRURemoved,
			"usage_before":    report.UsageBefore,
			"usage_after":     report.UsageAfter,
		})
		return
	}

	zap.L().Info("cache cleanup completed",
		zap.Int("deleted", report.Removed),
		zap.Int64("reclaimed_bytes", report.ReclaimedBytes),
	)
	c.JSON(http.StatusOK, contract.CacheReclaimResponse{
		Message: "cleanup completed", Deleted: report.Removed, Failed: report.Failed,
		ReclaimedBytes: report.ReclaimedBytes, Examined: report.Examined,
		ExpiredRemoved: report.ExpiredRemoved, LRURemoved: report.LRURemoved,
		UsageBefore: report.UsageBefore, UsageAfter: report.UsageAfter,
	})
}

// parseCleanupRequest preserves only the explicitly supported legacy contract:
// a request with no body invokes the broad manual reclaim. Any body must be a
// strict plan request; malformed input must never fall through to reclaim.
func parseCleanupRequest(body io.Reader) (string, error) {
	if body == nil {
		return "", nil
	}
	const maxBodyBytes = 1024
	data, err := io.ReadAll(io.LimitReader(body, maxBodyBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	invalid := errors.New("invalid cleanup plan request")
	if len(data) > maxBodyBytes {
		return "", invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return "", invalid
	}
	if token, err := decoder.Token(); err != nil || token != "plan_id" {
		return "", invalid
	}
	var planID string
	if err := decoder.Decode(&planID); err != nil || strings.TrimSpace(planID) == "" {
		return "", invalid
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return "", invalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", invalid
	}
	return strings.TrimSpace(planID), nil
}

func (h *CacheHandler) cleanupPlan(c *gin.Context, planID string) {
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok || !principal.CanWrite {
		c.JSON(http.StatusForbidden, gin.H{"code": "FORBIDDEN", "message": "write permission is required"})
		return
	}
	h.plansMu.Lock()
	plan, found := h.plans[planID]
	if found && time.Now().After(plan.expiresAt) {
		delete(h.plans, planID)
		found = false
	}
	if found && plan.principalID != principal.ID {
		found = false
	}
	if found {
		delete(h.plans, planID)
	}
	h.plansMu.Unlock()
	if !found {
		c.JSON(http.StatusConflict, gin.H{"code": "CACHE_PLAN_EXPIRED", "message": "cleanup preview expired or belongs to another operator"})
		return
	}
	deleted, skipped, failed := 0, 0, 0
	notAttempted := 0
	interrupted := false
	var reclaimed int64
	results := make([]contract.CacheCleanupItemResult, 0, len(plan.items))
	for index, candidate := range plan.items {
		removal, attempted, err := h.retention.RemoveIfMatches(c.Request.Context(), candidate)
		if err != nil {
			failed++
			results = append(results, contract.CacheCleanupItemResult{ID: candidate.ID, Status: contract.CacheCleanupItemStatusFailed, ObjectRemoved: removal.ObjectRemoved, MetadataRemoved: removal.MetadataRemoved})
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				interrupted = true
				notAttempted = len(plan.items) - index - 1
				break
			}
			continue
		}
		if !attempted {
			skipped++
			results = append(results, contract.CacheCleanupItemResult{ID: candidate.ID, Status: contract.CacheCleanupItemStatusSkipped})
			continue
		}
		if !removal.MetadataRemoved {
			failed++
			results = append(results, contract.CacheCleanupItemResult{ID: candidate.ID, Status: contract.CacheCleanupItemStatusFailed, ObjectRemoved: removal.ObjectRemoved})
			continue
		}
		deleted++
		reclaimed += removal.ReclaimedBytes
		results = append(results, contract.CacheCleanupItemResult{ID: candidate.ID, Status: contract.CacheCleanupItemStatusDeleted, ObjectRemoved: removal.ObjectRemoved, MetadataRemoved: true})
	}
	outcome := contract.CacheCleanupOutcomeSucceeded
	if failed > 0 || skipped > 0 {
		outcome = contract.CacheCleanupOutcomePartial
		if deleted == 0 && len(plan.items) > 0 {
			outcome = contract.CacheCleanupOutcomeFailed
		}
	}
	if interrupted {
		outcome = contract.CacheCleanupOutcomePartial
		if deleted == 0 && len(plan.items) > 0 {
			outcome = contract.CacheCleanupOutcomeFailed
		}
	}
	message := "cleanup completed"
	if interrupted {
		message = "cleanup interrupted before the plan completed"
	} else if outcome == contract.CacheCleanupOutcomePartial {
		message = "cleanup partially completed"
	} else if outcome == contract.CacheCleanupOutcomeFailed {
		message = "cleanup failed"
	}
	c.JSON(http.StatusOK, contract.CacheCleanupResponse{
		Message: message, Outcome: outcome, Interrupted: interrupted,
		Deleted: deleted, Skipped: skipped, Failed: failed,
		ReclaimedBytes: reclaimed, Planned: len(plan.items), PlannedCount: len(plan.items),
		PlannedBytes: plan.plannedBytes, TotalCandidateCount: plan.totalCandidateCount,
		NotAttempted: notAttempted, Items: results,
	})
}

// PreviewCleanup reports the same manual retention candidates without
// mutating cache metadata or objects.
func (h *CacheHandler) PreviewCleanup(c *gin.Context) {
	page := parseIntParam(c, "page", 1, 1, 100000)
	pageSize := parseIntParam(c, "page_size", 20, 1, 100)
	if h.retention == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": "CACHE_PREVIEW_UNAVAILABLE", "message": "cache cleanup preview is not configured"})
		return
	}
	preview, err := h.retention.Preview(c.Request.Context(), page, pageSize)
	if err != nil {
		zap.L().Warn("cache cleanup preview failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CACHE_PREVIEW_FAILED", "message": "cache cleanup preview is unavailable"})
		return
	}
	principal, ok := middleware.PrincipalFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": "UNAUTHORIZED", "message": "principal unavailable"})
		return
	}
	planBytes := make([]byte, 18)
	if _, err := rand.Read(planBytes); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "CACHE_PREVIEW_FAILED", "message": "could not create cleanup plan"})
		return
	}
	planID := fmt.Sprintf("%x", planBytes)
	expiresAt := time.Now().Add(5 * time.Minute)
	h.plansMu.Lock()
	for key, existing := range h.plans {
		if time.Now().After(existing.expiresAt) {
			delete(h.plans, key)
		}
	}
	if len(h.plans) >= 32 {
		h.plansMu.Unlock()
		c.JSON(http.StatusTooManyRequests, gin.H{"code": "CACHE_PREVIEW_LIMIT", "message": "too many cleanup previews"})
		return
	}
	var plannedBytes int64
	for _, item := range preview.Items {
		plannedBytes += item.Size
	}
	h.plans[planID] = cleanupPlan{
		principalID: principal.ID, expiresAt: expiresAt,
		items:               append([]cache.PreviewEntry(nil), preview.Items...),
		totalCandidateCount: preview.CandidateCount, plannedBytes: plannedBytes,
	}
	h.plansMu.Unlock()
	c.JSON(http.StatusOK, contract.CacheCleanupPreviewResponse{
		GeneratedAt: preview.GeneratedAt, UsageBytes: preview.UsageBytes,
		ThresholdBytes: preview.ThresholdBytes, TargetBytes: preview.TargetBytes,
		LogicalBytes: preview.LogicalBytes, CandidateCount: preview.CandidateCount,
		PlannedCount: len(preview.Items), PlannedBytes: plannedBytes,
		PhysicalUsageKnown: preview.PhysicalUsageKnown, PhysicalUsageMessage: preview.PhysicalUsageMessage,
		Items: cacheContractSlice(preview.Items, func(item cache.PreviewEntry) contract.CacheCleanupPreviewItem {
			return contract.CacheCleanupPreviewItem{
				ID: item.ID, Key: item.Key, AdapterType: item.AdapterType, PackageName: item.PackageName,
				Size: item.Size, HitCount: item.HitCount, LastAccessed: item.LastAccessed,
				ExpiresAt: item.ExpiresAt, Reason: contract.CacheCleanupReason(item.Reason),
			}
		}),
		Page: page, PageSize: pageSize, PlanID: planID, PlanExpiresAt: expiresAt,
	})
}

func (h *CacheHandler) GetDistribution(c *gin.Context) {
	type TypeBreakdown struct {
		Type      string
		Size      int64
		FileCount int64
	}

	type PackageSize struct {
		Name     string
		Type     string
		Size     int64
		HitCount int64
	}

	var byType []TypeBreakdown
	if err := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{}).
		Select("adapter_type as type, COALESCE(SUM(size), 0) as size, COUNT(*) as file_count").
		Group("adapter_type").
		Find(&byType).Error; err != nil {
		h.readError(c, err)
		return
	}

	var topPackages []PackageSize
	if err := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{}).
		Select("package_name as name, adapter_type as type, COALESCE(SUM(size), 0) as size, COALESCE(SUM(hit_count), 0) as hit_count").
		Where("package_name != ''").
		Group("package_name, adapter_type").
		Order("size DESC").
		Limit(30).
		Find(&topPackages).Error; err != nil {
		h.readError(c, err)
		return
	}

	var totalSize int64
	if err := h.db.WithContext(c.Request.Context()).Model(&db.CacheEntry{}).Select("COALESCE(SUM(size), 0)").Scan(&totalSize).Error; err != nil {
		h.readError(c, err)
		return
	}

	maxSize := int64(h.maxSizeGB) * 1024 * 1024 * 1024
	usagePercent := float64(0)
	if maxSize > 0 {
		usagePercent = float64(totalSize) / float64(maxSize) * 100
	}

	c.JSON(http.StatusOK, contract.CacheDistributionResponse{
		TotalSize: totalSize, MaxSize: maxSize, UsagePercent: usagePercent,
		ByType: cacheContractSlice(byType, func(row TypeBreakdown) contract.CacheTypeBreakdown {
			return contract.CacheTypeBreakdown{Type: row.Type, Size: row.Size, FileCount: row.FileCount}
		}),
		TopPackages: cacheContractSlice(topPackages, func(row PackageSize) contract.CachePackageSize {
			return contract.CachePackageSize{Name: row.Name, Type: row.Type, Size: row.Size, HitCount: row.HitCount}
		}),
	})
}

func (h *CacheHandler) SetTransferReader(read func() []traffic.Transfer) { h.transferReader = read }

// ListTransfers exposes runtime progress only on the authenticated Admin surface.
func (h *CacheHandler) ListTransfers(c *gin.Context) {
	items := make([]traffic.Transfer, 0)
	if h.transferReader != nil {
		items = h.transferReader()
	}
	c.JSON(http.StatusOK, contract.CacheTransfersResponse{
		Items: cacheContractSlice(items, func(transfer traffic.Transfer) contract.CacheTransfer {
			return contract.CacheTransfer{
				ID: transfer.ID, Name: transfer.Name, Ecosystem: transfer.Ecosystem, Upstream: transfer.Upstream,
				Status: contract.CacheTransferStatus(transfer.Status), DownloadedBytes: transfer.DownloadedBytes,
				TotalBytes: transfer.TotalBytes, ResumeAttempts: transfer.ResumeAttempts, ResumeOffset: transfer.ResumeOffset,
				BytesPerSecond: transfer.BytesPerSecond, StartedAt: transfer.StartedAt, UpdatedAt: transfer.UpdatedAt,
				FinishedAt: transfer.FinishedAt,
			}
		}),
		Limit: traffic.TransferLimit,
	})
}

func (h *CacheHandler) readError(c *gin.Context, err error) {
	zap.L().Error("read cache inventory", zap.Error(err))
	c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": "cache data is temporarily unavailable"})
}
