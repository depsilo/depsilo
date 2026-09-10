package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

// ErrCacheEntryNotFound means the requested cache entry is no longer present.
var ErrCacheEntryNotFound = errors.New("cache entry not found")

// ErrReclaimTargetNotReached means a capacity-triggered pass exhausted its
// tracked candidates while physical storage remained above the low-water
// mark. Pre-existing untracked objects or metadata size drift are common
// causes; callers should surface the incomplete pass and retry after repair.
var ErrReclaimTargetNotReached = errors.New("cache reclaim target not reached")

// RetentionPolicy controls capacity-triggered cache reclamation. Percentages
// are measured against MaxBytes; TargetPercent must be below ThresholdPercent.
type RetentionPolicy struct {
	MaxBytes         int64
	ThresholdPercent int
	TargetPercent    int
}

// DefaultRetentionPolicy derives a low-water mark from the configured high-
// water mark. The ten-point gap prevents cleanup from running again after only
// a small refill; the historical 80% target remains unchanged for the default
// 90% threshold and caps the target for higher thresholds.
func DefaultRetentionPolicy(maxBytes int64, thresholdPercent int) RetentionPolicy {
	targetPercent := thresholdPercent - 10
	if targetPercent > 80 {
		targetPercent = 80
	}
	if targetPercent < 0 {
		targetPercent = 0
	}
	return RetentionPolicy{
		MaxBytes:         maxBytes,
		ThresholdPercent: thresholdPercent,
		TargetPercent:    targetPercent,
	}
}

// ReclaimMode selects whether expiry cleanup is unconditional or capacity
// triggered. Both modes use the same guarded removal path.
type ReclaimMode string

const (
	// ReclaimModeManual always removes expired entries, then applies LRU when
	// the remaining usage is still at or above the configured threshold.
	ReclaimModeManual ReclaimMode = "manual"
	// ReclaimModeCapacity does no work below the initial threshold. Once
	// triggered, it removes expired entries first and then applies LRU until the
	// configured target is reached.
	ReclaimModeCapacity ReclaimMode = "capacity"
)

// ReclaimReport describes observable work performed by one reclaim pass.
type ReclaimReport struct {
	Examined        int   `json:"examined"`
	Removed         int   `json:"removed"`
	Failed          int   `json:"failed"`
	ReclaimedBytes  int64 `json:"reclaimed_bytes"`
	ExpiredRemoved  int   `json:"expired_removed"`
	LRURemoved      int   `json:"lru_removed"`
	StagingRemoved  int   `json:"staging_removed"`
	StagingFailures int   `json:"staging_failures"`
	UsageBefore     int64 `json:"usage_before"`
	UsageAfter      int64 `json:"usage_after"`
}

// ReclaimPreview is a read-only snapshot of the candidates selected by the
// manual reclaim policy. LogicalBytes is the sum of metadata sizes; it is an
// estimate of reclaimable bytes and may differ from physical storage usage.
type ReclaimPreview struct {
	GeneratedAt          time.Time      `json:"generated_at"`
	UsageBytes           int64          `json:"usage_bytes"`
	ThresholdBytes       int64          `json:"threshold_bytes"`
	TargetBytes          int64          `json:"target_bytes"`
	LogicalBytes         int64          `json:"logical_bytes"`
	CandidateCount       int64          `json:"candidate_count"`
	PhysicalUsageKnown   bool           `json:"physical_usage_known"`
	PhysicalUsageMessage string         `json:"physical_usage_message,omitempty"`
	Items                []PreviewEntry `json:"items"`
}

type PreviewEntry struct {
	ID           uint      `json:"id"`
	Key          string    `json:"key"`
	AdapterType  string    `json:"adapter_type"`
	PackageName  string    `json:"package_name"`
	Size         int64     `json:"size"`
	HitCount     int64     `json:"hit_count"`
	LastAccessed time.Time `json:"last_accessed"`
	ExpiresAt    time.Time `json:"expires_at"`
	Reason       string    `json:"reason"`
}

// RemoveIfMatches removes an entry only when the preview snapshot still
// describes the current row. It returns attempted=false for a disappeared or
// changed entry, allowing callers to report a safe skip.
func (retention *Retention) RemoveIfMatches(ctx context.Context, candidate PreviewEntry) (Removal, bool, error) {
	if retention == nil || retention.manager == nil {
		return Removal{}, false, errors.New("cache retention is unavailable")
	}
	return retention.removeCandidate(ctx, db.CacheEntry{
		ID: candidate.ID, Key: candidate.Key, Size: candidate.Size,
		AdapterType: candidate.AdapterType, PackageName: candidate.PackageName,
		ExpiresAt: candidate.ExpiresAt, LastAccessed: candidate.LastAccessed,
	}, func(current db.CacheEntry) bool {
		return current.Key == candidate.Key && current.Size == candidate.Size &&
			current.AdapterType == candidate.AdapterType && current.PackageName == candidate.PackageName &&
			current.ExpiresAt.Equal(candidate.ExpiresAt) && current.LastAccessed.Equal(candidate.LastAccessed)
	})
}

// Removal reports which irreversible stages completed. A caller can therefore
// distinguish an untouched entry from an object that was removed before a
// retryable metadata failure.
type Removal struct {
	ID              uint   `json:"id"`
	Key             string `json:"key"`
	ReclaimedBytes  int64  `json:"reclaimed_bytes"`
	ObjectRemoved   bool   `json:"object_removed"`
	MetadataRemoved bool   `json:"metadata_removed"`
}

// Retention owns every destructive cache mutation. It serializes deletion
// with Manager fills and refreshes through the same per-key mutation gate.
type Retention struct {
	manager   *Manager
	policy    RetentionPolicy
	threshold int64
	target    int64
}

// NewRetention validates policy and constructs a retention owner for manager.
func NewRetention(manager *Manager, policy RetentionPolicy) (*Retention, error) {
	if manager == nil {
		return nil, errors.New("cache retention: manager is required")
	}
	if manager.storage == nil {
		return nil, errors.New("cache retention: storage is required")
	}
	if manager.db == nil {
		return nil, errors.New("cache retention: database is required")
	}
	if manager.mutations == nil {
		return nil, errors.New("cache retention: mutation gate is required")
	}
	if policy.MaxBytes <= 0 {
		return nil, errors.New("cache retention: max bytes must be positive")
	}
	if policy.ThresholdPercent < 1 || policy.ThresholdPercent > 100 {
		return nil, errors.New("cache retention: threshold percent must be between 1 and 100")
	}
	if policy.TargetPercent < 0 || policy.TargetPercent > 100 {
		return nil, errors.New("cache retention: target percent must be between 0 and 100")
	}
	if policy.TargetPercent >= policy.ThresholdPercent {
		return nil, errors.New("cache retention: target percent must be below threshold percent")
	}

	return &Retention{
		manager:   manager,
		policy:    policy,
		threshold: percentOf(policy.MaxBytes, policy.ThresholdPercent),
		target:    percentOf(policy.MaxBytes, policy.TargetPercent),
	}, nil
}

func percentOf(total int64, percent int) int64 {
	// Divide before multiplying only when necessary, retaining useful precision
	// while avoiding overflow for unusually large configured capacities.
	quotient, remainder := total/100, total%100
	return quotient*int64(percent) + remainder*int64(percent)/100
}

// Remove deletes one entry's object before deleting its database row. A
// storage failure preserves the row. A database failure leaves the row
// retryable because Storage.Delete is required to be idempotent.
func (retention *Retention) Remove(ctx context.Context, id uint) (Removal, error) {
	if id == 0 {
		return Removal{}, fmt.Errorf("%w: id %d", ErrCacheEntryNotFound, id)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var candidate db.CacheEntry
	if err := retention.manager.db.WithContext(ctx).First(&candidate, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Removal{}, fmt.Errorf("%w: id %d", ErrCacheEntryNotFound, id)
		}
		return Removal{}, fmt.Errorf("read cache entry %d: %w", id, err)
	}

	removal, attempted, err := retention.removeCandidate(ctx, candidate, nil)
	if err != nil {
		return removal, err
	}
	if !attempted || !removal.MetadataRemoved {
		return removal, fmt.Errorf("%w: id %d", ErrCacheEntryNotFound, id)
	}
	return removal, nil
}

// Preview returns a bounded, read-only snapshot of the candidates that a
// manual reclaim pass would consider. It never acquires mutation locks,
// touches TTLs, or changes database/storage state.
func (retention *Retention) Preview(ctx context.Context, page, pageSize int) (ReclaimPreview, error) {
	if retention == nil || retention.manager == nil {
		return ReclaimPreview{}, errors.New("cache retention preview is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if page < 1 || pageSize < 1 {
		return ReclaimPreview{}, errors.New("cache retention preview: page and page size must be positive")
	}
	now := time.Now().UTC()
	preview := ReclaimPreview{
		GeneratedAt: now, ThresholdBytes: retention.threshold,
		TargetBytes: retention.target, Items: []PreviewEntry{},
	}
	usage, err := retention.manager.storage.TotalSize(ctx)
	if err != nil {
		return preview, fmt.Errorf("measure cache usage for preview: %w", err)
	}
	preview.UsageBytes = usage
	preview.PhysicalUsageKnown = true

	newQuery := func() *gorm.DB {
		return retention.manager.db.WithContext(ctx).Model(&db.CacheEntry{})
	}
	expired := newQuery().Where("datetime(expires_at) < datetime(?)", now)
	var expiredCount int64
	if err := newQuery().Where("datetime(expires_at) < datetime(?)", now).Count(&expiredCount).Error; err != nil {
		return preview, fmt.Errorf("count expired cache entries: %w", err)
	}
	var expiredBytes int64
	if err := newQuery().Where("datetime(expires_at) < datetime(?)", now).
		Select("COALESCE(SUM(size), 0)").Scan(&expiredBytes).Error; err != nil {
		return preview, fmt.Errorf("sum expired cache entries: %w", err)
	}
	if expiredBytes < 0 {
		expiredBytes = 0
	}
	postExpiryUsage := usage - expiredBytes
	if postExpiryUsage < 0 {
		postExpiryUsage = 0
	}

	lruEnabled := postExpiryUsage >= retention.threshold && postExpiryUsage > retention.target
	var lruCount, lruBytes int64
	if lruEnabled {
		need := postExpiryUsage - retention.target
		var cursorTime time.Time
		var cursorID uint
		for {
			batch := make([]db.CacheEntry, 0, 500)
			query := newQuery().Where("datetime(expires_at) >= datetime(?)", now).
				Select("id, size, last_accessed").Order("last_accessed ASC, id ASC").Limit(500)
			if cursorID != 0 {
				query = query.Where("(last_accessed > ? OR (last_accessed = ? AND id > ?))", cursorTime, cursorTime, cursorID)
			}
			if err := query.Find(&batch).Error; err != nil {
				return preview, fmt.Errorf("list LRU cache preview: %w", err)
			}
			for _, row := range batch {
				lruCount++
				lruBytes += maxInt64(row.Size, 0)
				if lruBytes >= need {
					break
				}
			}
			if lruBytes >= need || len(batch) < 500 {
				break
			}
			last := batch[len(batch)-1]
			cursorTime, cursorID = last.LastAccessed, last.ID
		}
	}
	preview.CandidateCount = expiredCount
	preview.LogicalBytes = expiredBytes
	if lruEnabled {
		preview.CandidateCount += lruCount
		preview.LogicalBytes += lruBytes
	}

	offset := (page - 1) * pageSize
	remaining := pageSize
	appendRows := func(rows []db.CacheEntry, reason string) {
		for _, row := range rows {
			if remaining == 0 {
				break
			}
			preview.Items = append(preview.Items, PreviewEntry{
				ID: row.ID, Key: row.Key, AdapterType: row.AdapterType,
				PackageName: row.PackageName, Size: maxInt64(row.Size, 0),
				HitCount: row.HitCount, LastAccessed: row.LastAccessed,
				ExpiresAt: row.ExpiresAt, Reason: reason,
			})
			remaining--
		}
	}
	if offset < int(expiredCount) {
		limit := pageSize
		var rows []db.CacheEntry
		if err := expired.Order("expires_at ASC, id ASC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
			return preview, fmt.Errorf("list expired cache preview: %w", err)
		}
		appendRows(rows, "expired")
	}
	if lruEnabled && remaining > 0 {
		lruOffset := offset - int(expiredCount)
		if lruOffset < 0 {
			lruOffset = 0
		}
		if lruOffset < int(lruCount) {
			var rows []db.CacheEntry
			limit := remaining
			if available := int(lruCount) - lruOffset; limit > available {
				limit = available
			}
			if err := newQuery().Where("datetime(expires_at) >= datetime(?)", now).
				Order("last_accessed ASC, id ASC").Offset(lruOffset).Limit(limit).Find(&rows).Error; err != nil {
				return preview, fmt.Errorf("list LRU cache preview: %w", err)
			}
			appendRows(rows, "lru")
		}
	}
	return preview, nil
}

func maxInt64(value, fallback int64) int64 {
	if value < fallback {
		return fallback
	}
	return value
}

type candidatePredicate func(db.CacheEntry) bool

// removeCandidate re-reads under the per-key gate so a candidate snapshot can
// never delete an entry concurrently refreshed by Manager. A false predicate
// is a benign skip, as is an entry another guarded mutation already removed.
func (retention *Retention) removeCandidate(
	ctx context.Context,
	candidate db.CacheEntry,
	predicate candidatePredicate,
) (removal Removal, attempted bool, err error) {
	removal = Removal{ID: candidate.ID, Key: candidate.Key}
	unlock, err := retention.manager.mutations.lock(ctx, candidate.Key)
	if err != nil {
		return removal, false, fmt.Errorf("lock cache entry %q: %w", candidate.Key, err)
	}
	defer unlock()

	var current db.CacheEntry
	if err := retention.manager.db.WithContext(ctx).First(&current, candidate.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return removal, false, nil
		}
		return removal, false, fmt.Errorf("re-read cache entry %d: %w", candidate.ID, err)
	}
	if current.Key != candidate.Key {
		return removal, false, fmt.Errorf("cache entry %d changed key while awaiting mutation lock", candidate.ID)
	}
	if predicate != nil && !predicate(current) {
		return removal, false, nil
	}
	removal.ID = current.ID
	removal.Key = current.Key
	attempted = true

	exists, err := retention.manager.storage.Exists(ctx, current.StoragePath)
	if err != nil {
		return removal, true, fmt.Errorf("check cache object %q: %w", current.Key, err)
	}
	if exists {
		if err := retention.manager.storage.Delete(ctx, current.StoragePath); err != nil {
			return removal, true, fmt.Errorf("delete cache object %q: %w", current.Key, err)
		}
		removal.ObjectRemoved = true
		removal.ReclaimedBytes = current.Size
		if removal.ReclaimedBytes < 0 {
			removal.ReclaimedBytes = 0
		}
	}
	deleted := retention.manager.db.WithContext(ctx).
		Where("id = ? AND key = ?", current.ID, current.Key).
		Delete(&db.CacheEntry{})
	if deleted.Error != nil {
		return removal, true, fmt.Errorf("delete cache entry %d: %w", current.ID, deleted.Error)
	}
	if deleted.RowsAffected != 1 {
		return removal, true, fmt.Errorf("delete cache entry %d: expected one row, deleted %d", current.ID, deleted.RowsAffected)
	}
	removal.MetadataRemoved = true
	return removal, true, nil
}

// Reclaim removes cache entries according to mode and continues after formal
// candidate-level failures. Staging reconciliation fails closed before any
// formal mutation because deleting healthy objects cannot compensate safely
// for unknown physical staging usage. The report always describes work that
// did complete.
func (retention *Retention) Reclaim(ctx context.Context, mode ReclaimMode) (ReclaimReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var report ReclaimReport
	if mode != ReclaimModeManual && mode != ReclaimModeCapacity {
		return report, fmt.Errorf("cache retention: unsupported reclaim mode %q", mode)
	}

	var reclaimErrors []error
	usage, err := retention.manager.storage.TotalSize(ctx)
	if err != nil {
		usageErr := fmt.Errorf("measure cache usage before reclaim: %w", err)
		if mode == ReclaimModeCapacity {
			return report, usageErr
		}
		reclaimErrors = append(reclaimErrors, usageErr)
	} else {
		report.UsageBefore = usage
		report.UsageAfter = usage
	}

	// A failed process can leave LocalStorage staging files without metadata.
	// Reconcile those candidates before deleting tracked cache entries;
	// otherwise LRU can remove every valid object and still miss its target.
	// Capacity passes defer the local walk until it is actually needed.
	if mode == ReclaimModeManual || usage >= retention.threshold {
		removed, reclaimed, stagingErrors := retention.reclaimStaging(ctx)
		report.StagingRemoved += removed
		report.StagingFailures += len(stagingErrors)
		report.ReclaimedBytes += reclaimed
		reclaimErrors = append(reclaimErrors, stagingErrors...)
		if removed > 0 {
			usage, err = retention.manager.storage.TotalSize(ctx)
			if err != nil {
				reclaimErrors = append(reclaimErrors, fmt.Errorf("measure cache usage after staging reclaim: %w", err))
				return report, errors.Join(reclaimErrors...)
			} else {
				report.UsageAfter = usage
			}
		}
		// Staging is part of the physical usage that triggered this pass. If it
		// cannot be reconciled reliably, deleting healthy formal objects cannot
		// prove the target will be reached and can cause avoidable data loss.
		// Both manual and capacity passes therefore fail closed before expiry or
		// LRU mutations.
		if len(stagingErrors) > 0 {
			return report, errors.Join(reclaimErrors...)
		}
	}

	if mode == ReclaimModeCapacity && usage < retention.threshold {
		return report, errors.Join(reclaimErrors...)
	}

	now := time.Now().UTC()
	var expired []db.CacheEntry
	if err := retention.manager.db.WithContext(ctx).
		Where("datetime(expires_at) < datetime(?)", now).
		Order("expires_at ASC, id ASC").
		Find(&expired).Error; err != nil {
		reclaimErrors = append(reclaimErrors, fmt.Errorf("list expired cache entries: %w", err))
		return report, errors.Join(reclaimErrors...)
	}
	for _, candidate := range expired {
		if err := ctx.Err(); err != nil {
			reclaimErrors = append(reclaimErrors, err)
			break
		}
		report.Examined++
		removal, attempted, removeErr := retention.removeCandidate(ctx, candidate, func(current db.CacheEntry) bool {
			return current.ExpiresAt.Before(now)
		})
		if attempted {
			report.ReclaimedBytes += removal.ReclaimedBytes
			usage -= removal.ReclaimedBytes
			if usage < 0 {
				usage = 0
			}
		}
		if removeErr != nil {
			report.Failed++
			reclaimErrors = append(reclaimErrors, fmt.Errorf("remove expired cache entry %d: %w", candidate.ID, removeErr))
			continue
		}
		if removal.MetadataRemoved {
			report.Removed++
			report.ExpiredRemoved++
		}
	}

	usage, err = retention.manager.storage.TotalSize(ctx)
	if err != nil {
		reclaimErrors = append(reclaimErrors, fmt.Errorf("measure cache usage after expiry reclaim: %w", err))
		return report, errors.Join(reclaimErrors...)
	}
	report.UsageAfter = usage

	applyLRU := mode == ReclaimModeCapacity || usage >= retention.threshold
	mustReachTarget := applyLRU && usage > retention.target
	if mustReachTarget {
		var candidates []db.CacheEntry
		if err := retention.manager.db.WithContext(ctx).
			Where("datetime(expires_at) >= datetime(?)", now).
			Order("last_accessed ASC, id ASC").
			Find(&candidates).Error; err != nil {
			reclaimErrors = append(reclaimErrors, fmt.Errorf("list LRU cache entries: %w", err))
		} else {
			for _, candidate := range candidates {
				if err := ctx.Err(); err != nil {
					reclaimErrors = append(reclaimErrors, err)
					break
				}
				if usage <= retention.target {
					break
				}
				report.Examined++
				removal, attempted, removeErr := retention.removeCandidate(ctx, candidate, nil)
				if attempted {
					report.ReclaimedBytes += removal.ReclaimedBytes
					usage -= removal.ReclaimedBytes
					if usage < 0 {
						usage = 0
					}
				}
				if removeErr != nil {
					report.Failed++
					reclaimErrors = append(reclaimErrors, fmt.Errorf("remove LRU cache entry %d: %w", candidate.ID, removeErr))
					continue
				}
				if removal.MetadataRemoved {
					report.Removed++
					report.LRURemoved++
				}
			}
		}
	}

	usageAfter, err := retention.manager.storage.TotalSize(ctx)
	if err != nil {
		reclaimErrors = append(reclaimErrors, fmt.Errorf("measure cache usage after reclaim: %w", err))
	} else {
		report.UsageAfter = usageAfter
		if mustReachTarget && usageAfter > retention.target {
			reclaimErrors = append(reclaimErrors, fmt.Errorf(
				"%w: usage %d bytes exceeds target %d bytes",
				ErrReclaimTargetNotReached,
				usageAfter,
				retention.target,
			))
		}
	}
	return report, errors.Join(reclaimErrors...)
}

// ReclaimRetired synchronously removes expired cache rows belonging to one
// retired adapter namespace. Startup uses this before adapters can refill a
// canonical key whose object path was retained only to give storage ownership
// a chance to delete it. It deliberately skips staging and LRU policy: this
// is a narrow migration recovery path, while removeCandidate still owns the
// object-before-metadata deletion and failure handling.
func (retention *Retention) ReclaimRetired(ctx context.Context, adapterType string) (ReclaimReport, error) {
	if retention == nil || retention.manager == nil {
		return ReclaimReport{}, errors.New("cache retired retention is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if adapterType == "" {
		return ReclaimReport{}, errors.New("cache retired retention: adapter type is required")
	}

	now := time.Now().UTC()
	var report ReclaimReport
	var reclaimErrors []error
	var candidates []db.CacheEntry
	query := retention.manager.db.WithContext(ctx).Where("adapter_type = ?", adapterType)
	// SQLite persists timestamps as text and can compare timezone suffixes
	// lexicographically. PostgreSQL and other typed timestamp dialects compare
	// the bound UTC value directly, so keeping the normalization here avoids
	// making server startup depend on SQLite's datetime function elsewhere.
	if retention.manager.db.Dialector.Name() == "sqlite" {
		query = query.Where("datetime(expires_at) < datetime(?)", now)
	} else {
		query = query.Where("expires_at < ?", now)
	}
	err := query.Order("id ASC").
		FindInBatches(&candidates, 500, func(_ *gorm.DB, _ int) error {
			for _, candidate := range candidates {
				if err := ctx.Err(); err != nil {
					return err
				}
				report.Examined++
				removal, attempted, removeErr := retention.removeCandidate(ctx, candidate, func(current db.CacheEntry) bool {
					return current.AdapterType == adapterType && current.ExpiresAt.Before(now)
				})
				if attempted {
					report.ReclaimedBytes += removal.ReclaimedBytes
				}
				if removeErr != nil {
					report.Failed++
					reclaimErrors = append(reclaimErrors, fmt.Errorf("remove retired cache entry %d: %w", candidate.ID, removeErr))
					continue
				}
				if removal.MetadataRemoved {
					report.Removed++
				}
			}
			return nil
		}).Error
	if err != nil {
		reclaimErrors = append(reclaimErrors, fmt.Errorf("list retired cache entries: %w", err))
	}
	return report, errors.Join(reclaimErrors...)
}

func (retention *Retention) reclaimStaging(ctx context.Context) (int, int64, []error) {
	stagingStore, ok := retention.manager.storage.(StagingObjectStore)
	if !ok {
		return 0, 0, nil
	}
	candidates, err := stagingStore.ListStaging(ctx)
	if err != nil {
		return 0, 0, []error{fmt.Errorf("list cache staging objects: %w", err)}
	}

	removed := 0
	var reclaimed int64
	var reclaimErrors []error
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			reclaimErrors = append(reclaimErrors, err)
			break
		}
		unlock, err := retention.manager.lockMutation(ctx, candidate.Key)
		if err != nil {
			reclaimErrors = append(reclaimErrors, fmt.Errorf("lock staging object %q: %w", candidate.Key, err))
			continue
		}

		removedCandidate, removeErr := stagingStore.RemoveStaging(ctx, candidate.Key)
		unlock()
		if removeErr != nil {
			reclaimErrors = append(reclaimErrors, fmt.Errorf("reclaim staging object %q: %w", candidate.Key, removeErr))
			continue
		}
		if removedCandidate {
			removed++
			if candidate.Size > 0 {
				reclaimed += candidate.Size
			}
		}
	}
	return removed, reclaimed, reclaimErrors
}
