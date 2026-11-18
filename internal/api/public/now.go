package public

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
	"depsilo/internal/db"
	"depsilo/internal/traffic"
	"depsilo/internal/upstream"
	"depsilo/internal/version"
)

// NowHandler powers the dashboard's "Now" strip. The endpoint is intentionally
// small (~250 bytes) so the 5-second poll interval doesn't strain SQLite or
// the WebSocket pipe. It carries just enough state to make the strip feel
// alive: the rolling 60s rate, the rolling 5-minute hit rate + avg latency,
// an upstream-health roll-up, a last-activity relative timestamp, and a
// 30-point sparkline (1-minute buckets over the last 30 minutes).
//
// Distinct from /api/v1/stats — that endpoint serves the full Portal page
// at a 30-second cadence and carries cache stats / top packages / extra
// indexes. /now is a focused liveness signal.
type NowHandler struct {
	db        *gorm.DB
	pools     map[string]*upstream.Pool
	startTime time.Time
	meter     *traffic.Meter
}

func NewNowHandler(database *gorm.DB, pools map[string]*upstream.Pool, startTime time.Time, meter *traffic.Meter) *NowHandler {
	return &NowHandler{db: database, pools: pools, startTime: startTime, meter: meter}
}

func (h *NowHandler) Get(c *gin.Context) {
	now := time.Now().UTC()
	ctx := c.Request.Context()
	sparkline, err := h.sparkline(ctx, now)
	if err != nil {
		nowDatabaseError(c, err)
		return
	}
	lastActivity, err := h.lastActivity(ctx, now)
	if err != nil {
		nowDatabaseError(c, err)
		return
	}
	rate, err := h.rate(ctx, now)
	if err != nil {
		nowDatabaseError(c, err)
		return
	}
	resp := contract.NowResponse{
		UptimeSeconds: int64(time.Since(h.startTime).Seconds()),
		NowUnix:       now.Unix(),
		Version:       version.Version,
		Upstreams:     h.upstreamRollup(),
		Sparkline:     sparkline,
		LastActivity:  lastActivity,
		Rate:          rate,
	}
	resp.Status = h.computeStatus(resp.Upstreams, resp.Rate)

	c.JSON(http.StatusOK, resp)
}

func nowDatabaseError(c *gin.Context, err error) {
	zap.L().Error("load current request statistics", zap.Error(err))
	c.JSON(http.StatusInternalServerError, gin.H{
		"code":    "DB_ERROR",
		"message": "failed to load current request statistics",
	})
}

// upstreamRollup tallies healthy vs total across every wired pool. The pool
// objects already hold this state in memory — no DB hit.
func (h *NowHandler) upstreamRollup() contract.NowUpstreams {
	var total, healthy int
	for _, pool := range h.pools {
		if pool == nil {
			continue
		}
		for _, u := range pool.Snapshot() {
			health := u.HealthSnapshot()
			total++
			if health.Healthy {
				healthy++
			}
		}
	}
	return contract.NowUpstreams{Total: total, Healthy: healthy}
}

// lastActivity returns the most recent access_log row as a relative timestamp
// + a tiny bit of context (which ecosystem, was it a hit). Returns nil when
// the table is empty. Retained logs do not establish lifetime usage history.
func (h *NowHandler) lastActivity(ctx context.Context, now time.Time) (*contract.NowLastActivity, error) {
	var row struct {
		CreatedAt   time.Time
		AdapterType string
		Hit         bool
		PackageName string
	}
	err := h.db.WithContext(ctx).Model(&db.AccessLog{}).
		Select("created_at, adapter_type, hit, package_name").
		Order("id DESC").
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.CreatedAt.IsZero() {
		return nil, nil
	}
	secondsAgo := int64(now.Sub(row.CreatedAt.UTC()).Seconds())
	if secondsAgo < 0 {
		secondsAgo = 0
	}
	return &contract.NowLastActivity{
		SecondsAgo:  secondsAgo,
		AdapterType: row.AdapterType,
		Hit:         row.Hit,
		PackageName: row.PackageName,
	}, nil
}

// rate computes the rolling 60-second rates. The request count comes from a
// bounded scan of raw access_logs; the byte and per-leg request rates come
// from the shared meter, which counts delivered-to-client bytes and measured
// upstream-read bytes independently. Without a meter, Measured stays false.
func (h *NowHandler) rate(ctx context.Context, now time.Time) (contract.NowRate, error) {
	since := now.Add(-60 * time.Second)
	var requests int64
	err := h.db.WithContext(ctx).Model(&db.AccessLog{}).
		Select("COUNT(*) AS requests_per_min").
		Where("created_at >= ?", since).
		Scan(&requests).Error
	if err != nil {
		return contract.NowRate{}, err
	}

	out := contract.NowRate{
		RequestsPerMin: requests,
		HasData:        requests > 0,
		WindowSeconds:  60,
	}
	if h.meter != nil {
		measured := h.meter.Snapshot(60, now)
		out.Measured = true
		out.ServiceRequestsPerSec = measured.ServiceRequestsPerSec()
		out.ServiceBytesPerSec = measured.ServiceBytesPerSec()
		out.OriginRequestsPerSec = measured.OriginRequestsPerSec()
		out.OriginBytesPerSec = measured.OriginBytesPerSec()
	}
	return out, nil
}

// sparkline returns 30 one-minute buckets ending at the current minute.
// Empty buckets are present in the response with zero counters so the
// frontend can render a continuous timeline without filling holes itself.
func (h *NowHandler) sparkline(ctx context.Context, now time.Time) ([]contract.NowSparklinePoint, error) {
	const buckets = 30
	const intervalSec = 60

	type bucketRow struct {
		Bucket   int64
		Requests int64
		Hits     int64
	}
	var rows []bucketRow

	since := now.Add(-buckets * time.Minute)
	err := h.db.WithContext(ctx).Model(&db.AccessLog{}).
		Select(`(CAST(strftime('%s', created_at) AS INTEGER) / ?) * ? AS bucket,
			COUNT(*) AS requests,
			COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits`,
			intervalSec, intervalSec).
		Where("created_at >= ?", since).
		Group("bucket").
		Order("bucket ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	lookup := make(map[int64]bucketRow, len(rows))
	for _, r := range rows {
		lookup[r.Bucket] = r
	}

	startBucket := (since.Unix() / int64(intervalSec)) * int64(intervalSec)
	out := make([]contract.NowSparklinePoint, 0, buckets)
	for i := 0; i < buckets; i++ {
		ts := startBucket + int64(i*intervalSec)
		p := contract.NowSparklinePoint{T: ts}
		if r, ok := lookup[ts]; ok {
			p.Requests = r.Requests
			p.Hits = r.Hits
		}
		out = append(out, p)
	}
	return out, nil
}

// computeStatus is a deliberately coarse signal — the strip only cares about
// "is anything obviously broken right now". Refinements (per-upstream
// severity, error rate thresholds) belong on the dedicated dashboard cards,
// not here.
func (h *NowHandler) computeStatus(u contract.NowUpstreams, _ contract.NowRate) contract.NowStatus {
	if u.Total == 0 {
		return contract.NowStatusHealthy // no upstreams wired yet (fresh install) — not a failure
	}
	if u.Healthy == 0 {
		return contract.NowStatusDown
	}
	if u.Healthy < u.Total {
		return contract.NowStatusDegraded
	}
	return contract.NowStatusHealthy
}
