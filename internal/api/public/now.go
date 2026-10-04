package public

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

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

// nowResponse is the JSON shape consumed by web/src/admin/components/NowStrip.tsx.
// Fields are kept short to minimize payload bytes on the 5s poll.
type nowResponse struct {
	Status        string           `json:"status"` // healthy | degraded | down
	UptimeSeconds int64            `json:"uptime_seconds"`
	NowUnix       int64            `json:"now_unix"`
	Version       string           `json:"version"`
	LastActivity  *lastActivity    `json:"last_activity,omitempty"`
	Rate          rateBlock        `json:"rate"`
	Upstreams     upstreamRollup   `json:"upstreams"`
	Sparkline     []sparklinePoint `json:"sparkline"`
}

type lastActivity struct {
	SecondsAgo  int64  `json:"seconds_ago"`
	AdapterType string `json:"adapter_type"`
	Hit         bool   `json:"hit"`
	PackageName string `json:"package_name,omitempty"`
}

type rateBlock struct {
	// RequestsPerMin is the rolling 60-second client request count.
	RequestsPerMin int64 `json:"requests_per_min"`
	// EgressBps/IngressBps are legacy aliases. Egress is delivered-to-client
	// bytes per second; Ingress is measured upstream-read bytes per second.
	// They were previously a miss-path proxy for origin traffic; both now come
	// from the shared meter when it is available.
	EgressBps  float64 `json:"egress_bps"`
	IngressBps float64 `json:"ingress_bps"`
	// HasData is false when the window observed zero requests; the frontend
	// renders "—" instead of a misleading "0".
	HasData bool `json:"has_data"`
	// Measured is true when the process meter supplied the byte rates, letting
	// the client distinguish "idle" from "not collected".
	Measured bool `json:"measured"`

	WindowSeconds int `json:"window_seconds"`

	// Explicit measured rates. Service* is client→Depsilo, Origin* is
	// Depsilo→upstream. They are separate request paths and are never summed.
	ServiceRequestsPerSec float64 `json:"service_requests_per_sec"`
	ServiceBytesPerSec    float64 `json:"service_bytes_per_sec"`
	OriginRequestsPerSec  float64 `json:"origin_requests_per_sec"`
	OriginBytesPerSec     float64 `json:"origin_bytes_per_sec"`
}

type upstreamRollup struct {
	Total   int `json:"total"`
	Healthy int `json:"healthy"`
}

type sparklinePoint struct {
	T        int64 `json:"t"` // unix seconds at bucket start
	Requests int64 `json:"requests"`
	Hits     int64 `json:"hits"`
}

func (h *NowHandler) Get(c *gin.Context) {
	now := time.Now().UTC()
	resp := nowResponse{
		UptimeSeconds: int64(time.Since(h.startTime).Seconds()),
		NowUnix:       now.Unix(),
		Version:       version.Version,
		Upstreams:     h.upstreamRollup(),
		Sparkline:     h.sparkline(now),
	}
	resp.LastActivity = h.lastActivity(now)
	resp.Rate = h.rate(now)
	resp.Status = h.computeStatus(resp.Upstreams, resp.Rate)

	c.JSON(http.StatusOK, resp)
}

// upstreamRollup tallies healthy vs total across every wired pool. The pool
// objects already hold this state in memory — no DB hit.
func (h *NowHandler) upstreamRollup() upstreamRollup {
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
	return upstreamRollup{Total: total, Healthy: healthy}
}

// lastActivity returns the most recent access_log row as a relative timestamp
// + a tiny bit of context (which ecosystem, was it a hit). Returns nil when
// the table is empty — the frontend renders the onboarding hint in that case.
func (h *NowHandler) lastActivity(now time.Time) *lastActivity {
	var row struct {
		CreatedAt   time.Time
		AdapterType string
		Hit         bool
		PackageName string
	}
	err := h.db.Model(&db.AccessLog{}).
		Select("created_at, adapter_type, hit, package_name").
		Order("id DESC").
		Limit(1).
		Scan(&row).Error
	if err != nil || row.CreatedAt.IsZero() {
		return nil
	}
	secondsAgo := int64(now.Sub(row.CreatedAt.UTC()).Seconds())
	if secondsAgo < 0 {
		secondsAgo = 0
	}
	return &lastActivity{
		SecondsAgo:  secondsAgo,
		AdapterType: row.AdapterType,
		Hit:         row.Hit,
		PackageName: row.PackageName,
	}
}

// rate computes the rolling 60-second rates. The request count comes from a
// bounded scan of raw access_logs; the byte rates come from the shared meter,
// which counts delivered-to-client bytes and measured upstream-read bytes
// independently. Without a meter (isolated tests) the legacy DB proxy is kept.
func (h *NowHandler) rate(now time.Time) rateBlock {
	since := now.Add(-60 * time.Second)
	var agg struct {
		Requests int64
		Egress   int64
		Ingress  int64
	}
	h.db.Model(&db.AccessLog{}).
		Select(`COUNT(*) AS requests,
			COALESCE(SUM(bytes_sent), 0) AS egress,
			COALESCE(SUM(CASE WHEN hit = 0 THEN bytes_sent ELSE 0 END), 0) AS ingress`).
		Where("created_at >= ?", since).
		Scan(&agg)

	out := rateBlock{
		RequestsPerMin: agg.Requests,
		HasData:        agg.Requests > 0,
		WindowSeconds:  60,
	}
	if h.meter != nil {
		measured := h.meter.Snapshot(60, now)
		out.Measured = true
		out.ServiceRequestsPerSec = measured.ServiceRequestsPerSec()
		out.ServiceBytesPerSec = measured.ServiceBytesPerSec()
		out.OriginRequestsPerSec = measured.OriginRequestsPerSec()
		out.OriginBytesPerSec = measured.OriginBytesPerSec()
		out.EgressBps = out.ServiceBytesPerSec
		out.IngressBps = out.OriginBytesPerSec
		return out
	}
	// Compatibility fallback for callers constructing the handler without a
	// meter. These are client-side totals, not measured origin traffic.
	out.EgressBps = float64(agg.Egress) / 60.0
	out.IngressBps = float64(agg.Ingress) / 60.0
	return out
}

// sparkline returns 30 one-minute buckets ending at the current minute.
// Empty buckets are present in the response with zero counters so the
// frontend can render a continuous timeline without filling holes itself.
func (h *NowHandler) sparkline(now time.Time) []sparklinePoint {
	const buckets = 30
	const intervalSec = 60

	type bucketRow struct {
		Bucket   int64
		Requests int64
		Hits     int64
	}
	var rows []bucketRow

	since := now.Add(-buckets * time.Minute)
	h.db.Model(&db.AccessLog{}).
		Select(`(CAST(strftime('%s', created_at) AS INTEGER) / ?) * ? AS bucket,
			COUNT(*) AS requests,
			COALESCE(SUM(CASE WHEN hit = 1 THEN 1 ELSE 0 END), 0) AS hits`,
			intervalSec, intervalSec).
		Where("created_at >= ?", since).
		Group("bucket").
		Order("bucket ASC").
		Scan(&rows)

	lookup := make(map[int64]bucketRow, len(rows))
	for _, r := range rows {
		lookup[r.Bucket] = r
	}

	startBucket := (since.Unix() / int64(intervalSec)) * int64(intervalSec)
	out := make([]sparklinePoint, 0, buckets)
	for i := 0; i < buckets; i++ {
		ts := startBucket + int64(i*intervalSec)
		p := sparklinePoint{T: ts}
		if r, ok := lookup[ts]; ok {
			p.Requests = r.Requests
			p.Hits = r.Hits
		}
		out = append(out, p)
	}
	return out
}

// computeStatus is a deliberately coarse signal — the strip only cares about
// "is anything obviously broken right now". Refinements (per-upstream
// severity, error rate thresholds) belong on the dedicated dashboard cards,
// not here.
func (h *NowHandler) computeStatus(u upstreamRollup, _ rateBlock) string {
	if u.Total == 0 {
		return "healthy" // no upstreams wired yet (fresh install) — not a failure
	}
	if u.Healthy == 0 {
		return "down"
	}
	if u.Healthy < u.Total {
		return "degraded"
	}
	return "healthy"
}
