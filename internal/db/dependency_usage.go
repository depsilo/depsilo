package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const dependencyUsageKey = "dependency_usage_v11"

// DependencyUsage is lifetime evidence, independent of access-log retention,
// cache eviction and process uptime. A timestamp is supplied only when the
// complete lifetime was observed; an upgraded instance may be used with no
// provable first request timestamp.
type DependencyUsage struct {
	State                    string     `json:"state"`
	FirstSuccessfulRequestAt *time.Time `json:"first_successful_request_at"`
}

const (
	DependencyNeverUsed = "never-used"
	DependencyUsed      = "used"
	DependencyUnknown   = "unknown"
)

type dependencyUsageRecord struct {
	DependencyUsage
	InProgress bool `json:"in_progress"`
}

func readDependencyUsageRecord(database *gorm.DB) (dependencyUsageRecord, error) {
	var row ControlPlaneState
	result := database.Where("key = ?", dependencyUsageKey).Limit(1).Find(&row)
	if result.Error != nil {
		return dependencyUsageRecord{}, result.Error
	}
	if result.RowsAffected == 0 {
		return dependencyUsageRecord{DependencyUsage: DependencyUsage{State: DependencyUnknown}}, nil
	}
	var record dependencyUsageRecord
	if err := json.Unmarshal([]byte(row.Value), &record); err != nil {
		return dependencyUsageRecord{}, fmt.Errorf("decode dependency usage: %w", err)
	}
	switch record.State {
	case DependencyNeverUsed, DependencyUsed, DependencyUnknown:
	default:
		return dependencyUsageRecord{}, fmt.Errorf("invalid dependency usage state %q", record.State)
	}
	return record, nil
}

func writeDependencyUsageRecord(database *gorm.DB, record dependencyUsageRecord) error {
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&ControlPlaneState{Key: dependencyUsageKey, Value: string(encoded), UpdatedAt: time.Now().UTC()}).Error
}

// ReadDependencyUsage is a conservative disk-only view. An active or
// interrupted recording session cannot prove absence; use the recorder's live
// reader while its continuous observation is active.
func ReadDependencyUsage(ctx context.Context, database *gorm.DB) (DependencyUsage, error) {
	record, err := readDependencyUsageRecord(database.WithContext(ctx))
	if err != nil {
		return DependencyUsage{State: DependencyUnknown}, err
	}
	if record.InProgress {
		if record.State == DependencyNeverUsed {
			record.State = DependencyUnknown
		}
		// An unflushed out-of-order observation may predate a saved success.
		// Positive use survives; its exact first timestamp needs clean coverage.
		record.FirstSuccessfulRequestAt = nil
	}
	return record.DependencyUsage, nil
}

// BeginDependencyUsage durably marks the recording session dirty before any
// requests can be observed. A crash can lose a pending positive observation,
// but the next start will then report unknown instead of never-used.
func BeginDependencyUsage(ctx context.Context, database *gorm.DB) (DependencyUsage, error) {
	usage := DependencyUsage{State: DependencyUnknown}
	err := withDurableUsageTransaction(ctx, database, func(tx *gorm.DB) error {
		record, err := readDependencyUsageRecord(tx)
		if err != nil {
			return err
		}
		if record.InProgress {
			if record.State == DependencyNeverUsed {
				record.State = DependencyUnknown
			}
			record.FirstSuccessfulRequestAt = nil
		}
		usage = record.DependencyUsage
		record.InProgress = true
		return writeDependencyUsageRecord(tx, record)
	})
	if err != nil {
		return DependencyUsage{State: DependencyUnknown}, err
	}
	return usage, nil
}

// SaveDependencyUsage retains positive evidence monotonically. clean is only
// true after the recorder has stopped accepting events and drained its work.
func SaveDependencyUsage(ctx context.Context, database *gorm.DB, usage DependencyUsage, clean bool) error {
	return withDurableUsageTransaction(ctx, database, func(tx *gorm.DB) error {
		stored, err := readDependencyUsageRecord(tx)
		if err != nil {
			return err
		}
		if stored.State == DependencyUsed {
			if usage.State == DependencyUsed && usage.FirstSuccessfulRequestAt == nil {
				stored.FirstSuccessfulRequestAt = nil
			}
			if usage.State == DependencyUsed && usage.FirstSuccessfulRequestAt != nil &&
				stored.FirstSuccessfulRequestAt != nil && usage.FirstSuccessfulRequestAt.Before(*stored.FirstSuccessfulRequestAt) {
				stored.FirstSuccessfulRequestAt = usage.FirstSuccessfulRequestAt
			}
			usage = stored.DependencyUsage
		} else if stored.State == DependencyUnknown && usage.State == DependencyNeverUsed {
			usage = stored.DependencyUsage
		}
		return writeDependencyUsageRecord(tx, dependencyUsageRecord{DependencyUsage: usage, InProgress: !clean})
	})
}

func withDurableUsageTransaction(ctx context.Context, database *gorm.DB, update func(*gorm.DB) error) (err error) {
	tx, release, err := BeginDurableTransaction(ctx, database)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	defer tx.Rollback()
	if err = update(tx); err != nil {
		return err
	}
	return tx.Commit().Error
}

func migrateDependencyUsage(database *gorm.DB) error {
	// Existing databases have incomplete history even when every retained log
	// and cached artifact has already been removed. Never backfill a first
	// timestamp from that truncated history.
	return writeDependencyUsageRecord(database, dependencyUsageRecord{DependencyUsage: DependencyUsage{State: DependencyUnknown}})
}
