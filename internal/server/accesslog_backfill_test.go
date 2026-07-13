package server

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/config"
	"depsilo/internal/db"
)

func newAccessLogLifecycleDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "server-accesslog.db"))
	if err != nil {
		t.Fatalf("open lifecycle database: %v", err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("migrate lifecycle database: %v", err)
	}
	return database
}

func TestPrepareFiveMinuteHistory_RollupDisabledInvalidatesWithoutRebuilding(t *testing.T) {
	database := newAccessLogLifecycleDB(t)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if err := database.Create(&db.ControlPlaneState{
		Key:   accesslog.FiveMinuteBackfillMarker,
		Value: "true",
	}).Error; err != nil {
		t.Fatalf("create marker: %v", err)
	}
	if err := database.Create(&db.AccessLogFiveMinutely{
		BucketStart: now.Add(-time.Hour).Unix(),
		AdapterType: "pypi",
		Upstream:    "origin",
	}).Error; err != nil {
		t.Fatalf("create fine history: %v", err)
	}

	err := prepareFiveMinuteHistory(context.Background(), database, config.AccessLogConfig{
		RollupEnabled:   false,
		BackfillOnStart: true,
	})
	if err != nil {
		t.Fatalf("prepareFiveMinuteHistory: %v", err)
	}

	var markerCount int64
	if err := database.Model(&db.ControlPlaneState{}).
		Where("key = ?", accesslog.FiveMinuteBackfillMarker).
		Count(&markerCount).Error; err != nil {
		t.Fatalf("count markers: %v", err)
	}
	if markerCount != 0 {
		t.Fatalf("marker count = %d, want 0", markerCount)
	}
	var fineCount int64
	if err := database.Model(&db.AccessLogFiveMinutely{}).Count(&fineCount).Error; err != nil {
		t.Fatalf("count fine history: %v", err)
	}
	if fineCount != 1 {
		t.Fatalf("fine history count = %d, want existing row retained", fineCount)
	}
}

func TestPrepareFiveMinuteHistory_RollupEnabledWithoutStartupBackfillLeavesMarkerMissing(t *testing.T) {
	database := newAccessLogLifecycleDB(t)
	if err := database.Create(&db.AccessLog{
		AdapterType: "npm",
		CreatedAt:   time.Now().UTC().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("create raw history: %v", err)
	}

	err := prepareFiveMinuteHistory(context.Background(), database, config.AccessLogConfig{
		RollupEnabled:   true,
		BackfillOnStart: false,
	})
	if err != nil {
		t.Fatalf("prepareFiveMinuteHistory: %v", err)
	}

	var markerCount int64
	if err := database.Model(&db.ControlPlaneState{}).
		Where("key = ?", accesslog.FiveMinuteBackfillMarker).
		Count(&markerCount).Error; err != nil {
		t.Fatalf("count markers: %v", err)
	}
	if markerCount != 0 {
		t.Fatalf("marker count = %d, want marker to remain missing", markerCount)
	}
}

func TestPrepareFiveMinuteHistory_InvalidationFailureIsLoggedAndReturned(t *testing.T) {
	database := newAccessLogLifecycleDB(t)
	if err := database.Create(&db.ControlPlaneState{
		Key:   accesslog.FiveMinuteBackfillMarker,
		Value: "true",
	}).Error; err != nil {
		t.Fatalf("create marker: %v", err)
	}
	if err := database.Exec(`
CREATE TRIGGER reject_five_minute_marker_delete
BEFORE DELETE ON control_plane_states
WHEN OLD.key = '` + accesslog.FiveMinuteBackfillMarker + `'
BEGIN
    SELECT RAISE(ABORT, 'reject marker invalidation');
END`).Error; err != nil {
		t.Fatalf("create invalidation trigger: %v", err)
	}

	core, logs := observer.New(zap.ErrorLevel)
	restoreLogger := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(restoreLogger)

	err := prepareFiveMinuteHistory(context.Background(), database, config.AccessLogConfig{
		RollupEnabled: false,
	})
	if err == nil || !strings.Contains(err.Error(), "invalidate five-minute backfill marker") {
		t.Fatalf("prepareFiveMinuteHistory error = %v, want invalidation error", err)
	}
	if logs.FilterMessage("failed to invalidate access log five-minute backfill marker").Len() != 1 {
		t.Fatalf("error logs = %+v, want one invalidation failure", logs.All())
	}
}
