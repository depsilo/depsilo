package server

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/accesslog"
	"depsilo/internal/adapter/packagekey"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func configureTrustedProxies(engine *gin.Engine, trustedProxies []string) error {
	if len(trustedProxies) == 0 {
		return engine.SetTrustedProxies(nil)
	}
	return engine.SetTrustedProxies(trustedProxies)
}

func prepareFiveMinuteHistory(ctx context.Context, database *gorm.DB, cfg config.AccessLogConfig) error {
	if !cfg.RollupEnabled {
		if err := accesslog.InvalidateFiveMinuteBackfill(ctx, database); err != nil {
			zap.L().Error("failed to invalidate access log five-minute backfill marker", zap.Error(err))
			return fmt.Errorf("invalidate five-minute backfill marker: %w", err)
		}
		return nil
	}
	if !cfg.BackfillOnStart {
		return nil
	}

	backfillStarted := time.Now()
	zap.L().Info("starting access log five-minute backfill",
		zap.Duration("window", 7*24*time.Hour))
	if err := accesslog.BackfillFiveMinutely(ctx, database, backfillStarted.UTC()); err != nil {
		zap.L().Warn("access log five-minute backfill failed",
			zap.Error(err),
			zap.Duration("took", time.Since(backfillStarted)))
	} else {
		zap.L().Info("access log five-minute backfill complete",
			zap.Duration("took", time.Since(backfillStarted)))
	}
	return nil
}

// backfillPackageNames updates existing cache entries that have an empty PackageName.
func backfillPackageNames(database *gorm.DB) {
	var entries []db.CacheEntry
	database.Where("package_name = '' OR package_name IS NULL").Find(&entries)
	if len(entries) == 0 {
		return
	}
	zap.L().Info("backfilling package names", zap.Int("count", len(entries)))
	for _, entry := range entries {
		if !canBackfillPackageName(entry) {
			continue
		}
		name := packagekey.ExtractName(entry.AdapterType, entry.Key)
		if name != "" {
			database.Model(&entry).Update("package_name", name)
		}
	}
	zap.L().Info("package name backfill complete")
}

// canBackfillPackageName is deliberately narrower than ExtractName. A few
// transports expose a package-looking token that is not the identity used by
// their vulnerability ecosystem (APT source-vs-binary names, NuGet casing,
// and RubyGems platform filenames). Schema v3 clears those legacy values;
// startup must not repopulate them before the next trusted migration or scan.
func canBackfillPackageName(entry db.CacheEntry) bool {
	ecosystem := strings.ToLower(strings.TrimSpace(entry.AdapterType))
	switch ecosystem {
	case "apt", "nuget", "rubygems":
		return false
	case "npm":
		// Legacy npm cache keys used a case-folded namespace and cannot be
		// recovered into the exact identity contract. New rows carry the
		// explicit namespace revision.
		return strings.HasPrefix(entry.Key, packagekey.NPMExactIdentityCachePrefix)
	default:
		return true
	}
}

// syncWebhookConfigs ensures configured webhooks from config.toml exist in the database.
func syncWebhookConfigs(database *gorm.DB, webhooks []config.WebhookConfig) {
	for _, webhook := range webhooks {
		var record db.WebhookConfig
		result := database.Where("url = ? AND platform = ?", webhook.URL, webhook.Platform).First(&record)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			database.Create(&db.WebhookConfig{
				Name:     webhook.Name,
				Platform: webhook.Platform,
				URL:      webhook.URL,
				Events:   webhook.Events,
				Enabled:  webhook.Enabled,
			})
			zap.L().Info("synced webhook config from config.toml", zap.String("name", webhook.Name))
		}
	}
}

// syncConfigOwnedUpstreams persists extra-index sources, which remain outside
// the dynamic standard-ecosystem Registry.
func syncConfigOwnedUpstreams(database *gorm.DB, adapterType string, upstreams []config.UpstreamConfig) {
	for _, item := range upstreams {
		mode := item.ProbeMode
		if mode == "" {
			mode = "active"
		}
		interval := item.ProbeInterval
		if interval == "" {
			interval = upstream.DefaultProbeInterval.String()
		}
		var record db.UpstreamRecord
		result := database.Where("name = ? AND adapter_type = ?", item.Name, adapterType).First(&record)
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			record = db.UpstreamRecord{
				AdapterType:   adapterType,
				Name:          item.Name,
				URL:           item.URL,
				Proxy:         item.Proxy,
				Priority:      item.Priority,
				ProbeMode:     mode,
				ProbeInterval: interval,
				Healthy:       true,
				SuccessRate:   1,
			}
			if err := database.Create(&record).Error; err != nil {
				zap.L().Warn("failed to sync config-owned upstream", zap.String("name", item.Name), zap.Error(err))
			}
			continue
		}
		if result.Error != nil {
			zap.L().Warn("failed to read config-owned upstream", zap.String("name", item.Name), zap.Error(result.Error))
			continue
		}
		if err := database.Model(&record).Updates(map[string]any{
			"url":            item.URL,
			"proxy":          item.Proxy,
			"priority":       item.Priority,
			"probe_mode":     mode,
			"probe_interval": interval,
		}).Error; err != nil {
			zap.L().Warn("failed to update config-owned upstream", zap.String("name", item.Name), zap.Error(err))
		}
	}
}
