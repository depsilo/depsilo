package upstream

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// StartHealthCheck runs periodic health checks on all upstreams in the pool.
func StartHealthCheck(ctx context.Context, pool *Pool, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, u := range pool.Upstreams() {
				go checkUpstream(ctx, u)
			}
		}
	}
}

func checkUpstream(ctx context.Context, u *Upstream) {
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

	zap.L().Debug("health check done",
		zap.String("upstream", u.Name),
		zap.Bool("healthy", healthy),
		zap.Duration("latency", latency),
	)
}
