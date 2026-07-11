package upstream

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// RestoreFromDB loads the most recent latency data from the database to
// warm up in-memory metrics after a server restart. Without this, all
// upstreams show 0ms / "--" until the first health check completes.
func RestoreFromDB(pool *Pool, database *gorm.DB) {
	if database == nil {
		return
	}
	for _, u := range pool.Snapshot() {
		var logs []db.UpstreamLatencyLog
		query := database.Where("upstream_id = ? AND healthy = ?", u.ID, true)
		if u.ID == 0 {
			query = query.Where("name = ?", u.Name)
		}
		query.
			Order("datetime(created_at) DESC").
			Limit(10).
			Find(&logs)

		if len(logs) == 0 {
			continue
		}

		var total int64
		for _, l := range logs {
			total += l.LatencyMs
		}
		avgMs := total / int64(len(logs))

		u.mu.Lock()
		u.health.avgLatency = time.Duration(avgMs) * time.Millisecond
		u.health.healthy = logs[0].Healthy
		u.health.lastCheckedAt = logs[0].CreatedAt
		u.mu.Unlock()

		zap.L().Debug("restored upstream metrics from db",
			zap.String("upstream", u.Name),
			zap.Int64("avg_latency_ms", avgMs),
			zap.Int("data_points", len(logs)),
		)
	}
}

// StartHealthCheck launches one goroutine per active-mode upstream in the pool.
// Passive-mode upstreams are skipped — they rely on request-time Report() calls.
func StartHealthCheck(ctx context.Context, pool *Pool, database *gorm.DB, defaultInterval time.Duration) {
	for _, u := range pool.Snapshot() {
		if u.ProbeMode != "active" {
			zap.L().Info("upstream in passive probe mode, skipping health check",
				zap.String("upstream", u.Name))
			continue
		}
		interval := u.ProbeInterval
		if interval <= 0 {
			interval = defaultInterval
		}
		go runUpstreamHealthCheck(ctx, u, database, interval)
	}
}

func runUpstreamHealthCheck(ctx context.Context, u *Upstream, database *gorm.DB, interval time.Duration) {
	zap.L().Info("starting health check goroutine",
		zap.String("upstream", u.Name),
		zap.Duration("interval", interval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result := probe(ctx, u)
			u.applyProbe(result)
			if err := persistProbe(database, u, result); err != nil {
				zap.L().Warn("persist upstream probe", zap.Uint("upstream_id", u.ID), zap.Error(err))
			}
		}
	}
}

func probe(ctx context.Context, u *Upstream) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.URL, nil)
	if err != nil {
		return ProbeResult{CheckedAt: time.Now().UTC(), Err: err}
	}
	req.Header.Set("User-Agent", "depsilo/0.1")

	start := time.Now()
	resp, err := u.client.Do(req)
	result := ProbeResult{Latency: time.Since(start), CheckedAt: time.Now().UTC(), Err: err}
	if err == nil {
		resp.Body.Close()
		result.Healthy = resp.StatusCode < 500
		if !result.Healthy {
			result.Err = fmt.Errorf("upstream returned status %d", resp.StatusCode)
		}
	}
	return result
}

func persistProbe(database *gorm.DB, u *Upstream, result ProbeResult) error {
	if database == nil {
		return nil
	}
	health := u.HealthSnapshot()
	return database.Transaction(func(tx *gorm.DB) error {
		if u.ID != 0 {
			updated := tx.Model(&db.UpstreamRecord{}).Where("id = ?", u.ID).Updates(map[string]any{
				"healthy":         health.Healthy,
				"avg_latency_ms":  health.AvgLatency.Milliseconds(),
				"success_rate":    health.SuccessRate,
				"last_checked_at": health.LastCheckedAt,
			})
			if updated.Error != nil {
				return updated.Error
			}
			if updated.RowsAffected != 1 {
				return fmt.Errorf("persist upstream %d: expected one row, updated %d", u.ID, updated.RowsAffected)
			}
		}
		return tx.Create(&db.UpstreamLatencyLog{
			UpstreamID: u.ID,
			Name:       u.Name,
			LatencyMs:  result.Latency.Milliseconds(),
			Healthy:    result.Healthy,
			CreatedAt:  result.CheckedAt,
		}).Error
	})
}

// StartLatencyLogCleanup periodically removes latency logs older than 7 days.
func StartLatencyLogCleanup(ctx context.Context, database *gorm.DB) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().AddDate(0, 0, -7).UTC()
			database.Where("datetime(created_at) < datetime(?)", cutoff).Delete(&db.UpstreamLatencyLog{})
			zap.L().Info("cleaned up old latency logs", zap.Time("cutoff", cutoff))
		}
	}
}
