// Package upstreamupdates owns proactive refreshes of cached package metadata
// and the durable Operator-facing event trail produced by those refreshes.
package upstreamupdates

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

const (
	ResultUpdated   = "updated"
	ResultUnchanged = "unchanged"
	ResultError     = "error"

	candidateBatchSize = 100
)

// RefreshOutcome is the small seam between the producer and the proxy-aware
// refresh adapter. The adapter hides route mapping, conditional requests and
// durable cache commits; the producer owns scheduling and outcome translation.
type RefreshOutcome struct {
	Upstream string
	Changed  bool
	Detail   string
}

// Refresher synchronously refreshes one mutable metadata entry. A successful
// return guarantees that any cache mutation is durable before the producer
// records its event.
type Refresher func(context.Context, db.CacheEntry) (RefreshOutcome, error)

// Producer is a single, context-bound metadata refresh loop. Run performs no
// detached work: server cancellation therefore stops admission and joins the
// current refresh through the existing asynchronous runtime.
type Producer struct {
	database *gorm.DB
	history  *History
	interval time.Duration
	refresh  Refresher
}

// New constructs a proactive metadata producer from its real database,
// schedule and proxy-aware refresh seam.
func New(database *gorm.DB, interval time.Duration, refresh Refresher) (*Producer, error) {
	if database == nil {
		return nil, errors.New("upstream updates: database is required")
	}
	if interval <= 0 {
		return nil, errors.New("upstream updates: interval must be positive")
	}
	if refresh == nil {
		return nil, errors.New("upstream updates: refresher is required")
	}
	history, err := NewHistory(database)
	if err != nil {
		return nil, err
	}
	return &Producer{database: database, history: history, interval: interval, refresh: refresh}, nil
}

// Run checks cached metadata after each configured interval until ctx is
// cancelled. Sweeps never overlap; a slow Upstream naturally applies
// backpressure instead of creating an unbounded queue of refresh goroutines.
func (p *Producer) Run(ctx context.Context) {
	if ctx == nil {
		return
	}
	timer := time.NewTimer(p.interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := p.check(ctx); err != nil && ctx.Err() == nil {
				zap.L().Warn("upstream metadata sweep incomplete", zap.Error(err))
			}
			timer.Reset(p.interval)
		}
	}
}

func (p *Producer) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	var maxID uint
	if err := p.candidates(ctx).
		Select("COALESCE(MAX(id), 0)").
		Scan(&maxID).Error; err != nil {
		return fmt.Errorf("discover metadata refresh upper bound: %w", err)
	}

	var sweepErrors []error
	var lastID uint
	for lastID < maxID {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(sweepErrors, err)...)
		}
		var entries []db.CacheEntry
		if err := p.candidates(ctx).
			Where("id > ? AND id <= ?", lastID, maxID).
			Order("id ASC").
			Limit(candidateBatchSize).
			Find(&entries).Error; err != nil {
			return errors.Join(append(sweepErrors, fmt.Errorf("list metadata refresh candidates: %w", err))...)
		}
		if len(entries) == 0 {
			break
		}

		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return errors.Join(append(sweepErrors, err)...)
			}
			started := time.Now()
			outcome, refreshErr := p.refresh(ctx, entry)
			result := ResultUnchanged
			detail := strings.TrimSpace(outcome.Detail)
			if outcome.Changed {
				result = ResultUpdated
				if detail == "" {
					detail = "cached metadata refreshed"
				}
			} else if detail == "" {
				detail = "upstream metadata not modified"
			}
			if refreshErr != nil {
				result = ResultError
				// Refresh errors may contain credential-bearing URLs. Persist a
				// stable safe message; detailed diagnostics stay in typed logs.
				detail = "metadata refresh failed"
				zap.L().Warn("proactive metadata refresh failed",
					zap.Uint("cache_entry_id", entry.ID),
					zap.String("ecosystem", entry.AdapterType),
					zap.String("error_type", fmt.Sprintf("%T", refreshErr)),
				)
			}

			packageName := strings.TrimSpace(entry.PackageName)
			if packageName == "" {
				packageName = entry.Key
			}
			observation := Observation{
				CacheEntryID: entry.ID,
				Ecosystem:    entry.AdapterType,
				Upstream:     outcome.Upstream,
				Package:      packageName,
				Result:       result,
				Detail:       detail,
				Latency:      time.Since(started),
				ObservedAt:   time.Now().UTC(),
			}
			if _, err := p.history.Record(ctx, observation); err != nil {
				sweepErrors = append(sweepErrors, fmt.Errorf("persist metadata refresh event for cache entry %d: %w", entry.ID, err))
			}
		}
		lastID = entries[len(entries)-1].ID
	}
	return errors.Join(sweepErrors...)
}

// candidates deliberately describes a narrower capability than
// CacheKindMetadata. That cache kind also includes mutable binary objects such
// as Maven SNAPSHOT artifacts, and most adapters currently do not issue
// conditional requests. Only PyPI-compatible handlers persist and replay HTTP
// validators today, so only their validated entries can produce an honest
// updated/unchanged result without downloading artifacts or reporting every
// successful HTTP 200 as a change.
func (p *Producer) candidates(ctx context.Context) *gorm.DB {
	return p.database.WithContext(ctx).
		Model(&db.CacheEntry{}).
		Where("cache_kind = ?", db.CacheKindMetadata).
		Where("etag <> '' OR last_modified <> ''").
		Where("adapter_type = ? OR adapter_type LIKE ?", "pypi", "extra:%")
}
