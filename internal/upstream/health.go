package upstream

import (
	"context"
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
	for _, u := range pool.Upstreams() {
		var logs []db.UpstreamLatencyLog
		database.Where("name = ? AND healthy = ?", u.Name, true).
			Order("created_at DESC").
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
		u.avgLatency = time.Duration(avgMs) * time.Millisecond
		u.Healthy = logs[0].Healthy
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
	for _, u := range pool.Upstreams() {
		if u.ProbeMode == "passive" {
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
			checkUpstream(ctx, u, database)
		}
	}
}

func checkUpstream(ctx context.Context, u *Upstream, database *gorm.DB) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.URL, nil)
	if err != nil {
		zap.L().Warn("health check request error", zap.String("upstream", u.Name), zap.Error(err))
		return
	}

	start := time.Now()
	resp, err := u.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		u.mu.Lock()
		u.Healthy = false
		u.mu.Unlock()
		zap.L().Warn("upstream unhealthy", zap.String("upstream", u.Name), zap.Error(err))
		if database != nil {
			go func() {
				database.Create(&db.UpstreamLatencyLog{
					Name:      u.Name,
					LatencyMs: latency.Milliseconds(),
					Healthy:   false,
				})
			}()
		}
		return
	}
	resp.Body.Close()

	healthy := resp.StatusCode < 500
	u.mu.Lock()
	u.Healthy = healthy
	if u.avgLatency == 0 {
		u.avgLatency = latency
	} else {
		u.avgLatency = (u.avgLatency*7 + latency*3) / 10
	}
	u.mu.Unlock()

	if database != nil {
		go func() {
			database.Create(&db.UpstreamLatencyLog{
				Name:      u.Name,
				LatencyMs: latency.Milliseconds(),
				Healthy:   healthy,
			})
		}()
	}

	zap.L().Debug("health check done",
		zap.String("upstream", u.Name),
		zap.Bool("healthy", healthy),
		zap.Duration("latency", latency),
	)
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
			cutoff := time.Now().AddDate(0, 0, -7)
			database.Where("created_at < ?", cutoff).Delete(&db.UpstreamLatencyLog{})
			zap.L().Info("cleaned up old latency logs", zap.Time("cutoff", cutoff))
		}
	}
}
