package upstream

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"depsilo/internal/config"
	"depsilo/internal/db"
)

// DefaultProbeInterval is the fallback periodic health-check interval used when
// an upstream does not specify its own probe_interval. Kept intentionally long
// so the proxy does not hammer mirrors with frequent probes — latency data also
// comes from real traffic and on-demand manual checks. Change this single
// constant to adjust the global default frequency.
const DefaultProbeInterval = 30 * time.Minute

var errCriticalFailureLatched = errors.New("upstream disabled after critical protocol failure")

// Upstream represents a single upstream source with its HTTP client.
type Upstream struct {
	ID            uint
	AdapterType   string
	Name          string
	URL           string
	Proxy         string
	Priority      int
	ProbeMode     string        // "active" or "passive"
	ProbeInterval time.Duration // parsed from config string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	client        *http.Client
	// directNetworkGuard means hostname targets are resolved and authorized by
	// the guarded dialer before the exact resolved IP is used for the socket.
	// Configured HTTP proxies own their DNS and egress boundary instead.
	directNetworkGuard bool

	mu       sync.RWMutex
	health   healthState
	recovery passiveRecoveryState
}

type passiveRecoveryState struct {
	reserved    bool
	inFlight    bool
	nextAttempt time.Time
}

type healthState struct {
	healthy                bool
	criticalFailureLatched bool
	avgLatency             time.Duration
	successRate            float64
	totalReqs              int64
	successReqs            int64
	lastCheckedAt          time.Time
}

// HealthSnapshot is one consistent sample of an upstream's mutable health.
type HealthSnapshot struct {
	Healthy       bool
	AvgLatency    time.Duration
	SuccessRate   float64
	LastCheckedAt time.Time
}

// ProbeResult captures one active health-check outcome.
type ProbeResult struct {
	Healthy   bool
	Latency   time.Duration
	CheckedAt time.Time
	Err       error
}

func (h *healthState) applyProbe(result ProbeResult) {
	h.totalReqs++
	if result.Healthy {
		h.successReqs++
	}
	h.successRate = float64(h.successReqs) / float64(h.totalReqs)
	h.healthy = result.Healthy && !h.criticalFailureLatched
	h.lastCheckedAt = result.CheckedAt
	if result.Latency > 0 {
		if h.avgLatency == 0 {
			h.avgLatency = result.Latency
		} else {
			h.avgLatency = (h.avgLatency*7 + result.Latency*3) / 10
		}
	}
}

// FetchResult holds the response from an upstream fetch.
type FetchResult struct {
	Body         io.ReadCloser
	ContentType  string
	Size         int64
	StatusCode   int
	ETag         string
	LastModified string
	// URL is the final response URL after redirects, with credentials and the
	// fragment removed. Adapters use it only as a base for resolving links
	// declared by upstream metadata.
	URL string
}

// Pool manages a set of upstreams for a given adapter type.
type poolSnapshot struct {
	upstreams []*Upstream
	byID      map[uint]*Upstream
}

type Pool struct {
	snapshot atomic.Pointer[poolSnapshot]
}

// NewPool creates an upstream pool from config.
func NewPool(cfgs []config.UpstreamConfig) (*Pool, error) {
	records := make([]db.UpstreamRecord, 0, len(cfgs))
	for _, cfg := range cfgs {
		records = append(records, db.UpstreamRecord{
			Name: cfg.Name, URL: cfg.URL, Proxy: cfg.Proxy, Priority: cfg.Priority,
			ProbeMode: cfg.ProbeMode, ProbeInterval: cfg.ProbeInterval,
			Healthy: true, SuccessRate: 1,
		})
	}
	pool, err := NewPoolFromRecords(records)
	if err != nil {
		return nil, err
	}
	for _, u := range pool.Snapshot() {
		zap.L().Info("registered upstream",
			zap.String("name", u.Name),
			zap.String("url", safeURLOrigin(u.URL)),
			zap.Int("priority", u.Priority),
			zap.String("proxy", safeURLOrigin(u.Proxy)),
			zap.String("probe_mode", u.ProbeMode),
			zap.Duration("probe_interval", u.ProbeInterval),
		)
	}
	return pool, nil
}

// NewPoolFromRecords creates a pool from persisted upstream records.
func NewPoolFromRecords(records []db.UpstreamRecord) (*Pool, error) {
	next, err := buildPoolSnapshot(records, nil)
	if err != nil {
		return nil, err
	}
	pool := &Pool{}
	pool.Replace(next)
	return pool, nil
}

func (p *Pool) load() *poolSnapshot {
	return p.snapshot.Load()
}

// Snapshot returns a caller-owned copy of the current upstream list.
func (p *Pool) Snapshot() []*Upstream {
	current := p.load()
	if current == nil {
		return nil
	}
	return append([]*Upstream(nil), current.upstreams...)
}

// Replace atomically publishes an immutable pool snapshot.
func (p *Pool) Replace(next *poolSnapshot) {
	p.snapshot.Store(next)
}

// Find returns an upstream from the currently published snapshot.
func (p *Pool) Find(id uint) (*Upstream, bool) {
	current := p.load()
	if current == nil {
		return nil, false
	}
	u, ok := current.byID[id]
	return u, ok
}

func buildPoolSnapshot(records []db.UpstreamRecord, previous *poolSnapshot) (*poolSnapshot, error) {
	next := &poolSnapshot{
		upstreams: make([]*Upstream, 0, len(records)),
		byID:      make(map[uint]*Upstream, len(records)),
	}
	for _, record := range records {
		if previous != nil {
			if existing := previous.byID[record.ID]; existing != nil && existing.sameConfig(record) {
				next.upstreams = append(next.upstreams, existing)
				next.byID[record.ID] = existing
				continue
			}
		}
		u, err := newUpstreamFromRecord(record)
		if err != nil {
			return nil, err
		}
		next.upstreams = append(next.upstreams, u)
		next.byID[u.ID] = u
	}
	return next, nil
}

func normalizeRecordProbe(record db.UpstreamRecord) (string, time.Duration, error) {
	mode := record.ProbeMode
	if mode == "" {
		mode = "active"
	}
	if mode != "active" && mode != "passive" {
		return "", 0, fmt.Errorf("invalid probe mode %q", mode)
	}
	intervalText := record.ProbeInterval
	if intervalText == "" {
		intervalText = DefaultProbeInterval.String()
	}
	interval, err := time.ParseDuration(intervalText)
	if err != nil || interval <= 0 {
		return "", 0, fmt.Errorf("invalid probe interval %q", intervalText)
	}
	return mode, interval, nil
}

func newUpstreamFromRecord(record db.UpstreamRecord) (*Upstream, error) {
	mode, interval, err := normalizeRecordProbe(record)
	if err != nil {
		return nil, err
	}
	client, err := buildClient(record.Proxy, record.URL)
	if err != nil {
		return nil, fmt.Errorf("build client for %s: %w", record.Name, err)
	}
	return &Upstream{
		ID: record.ID, AdapterType: record.AdapterType, Name: record.Name, URL: record.URL,
		Proxy: record.Proxy, Priority: record.Priority, ProbeMode: mode, ProbeInterval: interval,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, client: client,
		directNetworkGuard: record.Proxy == "",
		health: healthState{
			healthy: record.Healthy, avgLatency: time.Duration(record.AvgLatencyMs) * time.Millisecond,
			successRate: record.SuccessRate, lastCheckedAt: record.LastCheckedAt,
		},
	}, nil
}

func (u *Upstream) sameConfig(record db.UpstreamRecord) bool {
	mode, interval, err := normalizeRecordProbe(record)
	return err == nil && u.ID == record.ID && u.AdapterType == record.AdapterType &&
		u.Name == record.Name && u.URL == record.URL && u.Proxy == record.Proxy &&
		u.Priority == record.Priority && u.ProbeMode == mode && u.ProbeInterval == interval
}

// Fetch performs an HTTP GET to the upstream, joining the given path.
func (u *Upstream) Fetch(ctx context.Context, path string) (*FetchResult, error) {
	reqURL, err := originRequestURL(u.URL, path)
	if err != nil {
		return nil, err
	}
	return u.do(ctx, reqURL, true)
}

// FetchURL performs an HTTP GET against an absolute URL using this
// upstream's client (including its per-upstream proxy). Used by
// adapters whose artifact downloads live on a different host than
// the metadata endpoint — e.g. Composer dists, which the p2
// metadata points at GitHub or a mirror's storage host.
//
// Latency/health are deliberately NOT reported: the target is a
// third-party host, and charging its latency or failures to this
// upstream would poison the mirror's health accounting.
func (u *Upstream) FetchURL(ctx context.Context, reqURL string) (*FetchResult, error) {
	if !validHTTPURL(reqURL) {
		return nil, fmt.Errorf("create request for %s: invalid HTTP URL", safeURLOrigin(reqURL))
	}
	target, _ := url.Parse(reqURL)
	origin, _ := url.Parse(u.URL)
	if err := validateExternalTarget(origin, target, u.directNetworkGuard); err != nil {
		return nil, fmt.Errorf("fetch %s: external target rejected", safeURLOrigin(reqURL))
	}
	return u.do(ctx, reqURL, false)
}

func (u *Upstream) do(ctx context.Context, reqURL string, report bool) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: invalid URL", safeURLOrigin(reqURL))
	}
	req.Header.Set("User-Agent", "depsilo/0.1")

	if u.client == nil {
		return nil, fmt.Errorf("fetch %s: upstream client unavailable", safeURLOrigin(reqURL))
	}
	client := *u.client
	client.CheckRedirect = u.secureRedirectCheck(client.CheckRedirect)
	recovery, err := u.admitExchange()
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", safeURLOrigin(reqURL), err)
	}
	if recovery {
		defer u.finishPassiveRecovery()
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		if report {
			u.Report(latency, false)
		}
		return nil, fmt.Errorf("fetch %s: %w", safeURLOrigin(reqURL), redactedTransportError{cause: err})
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		if report {
			u.Report(latency, resp.StatusCode < 500)
		}
		return nil, fmt.Errorf("upstream %s returned %d for %s", u.Name, resp.StatusCode, safeURLOrigin(reqURL))
	}

	if report {
		u.Report(latency, true)
	}

	return &FetchResult{
		Body:         resp.Body,
		ContentType:  resp.Header.Get("Content-Type"),
		Size:         resp.ContentLength,
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		URL:          responseURL(resp, reqURL),
	}, nil
}

// FetchWithHeaders performs an HTTP GET with additional request headers.
func (u *Upstream) FetchWithHeaders(ctx context.Context, path string, headers map[string]string) (*FetchResult, error) {
	reqURL, joinErr := originRequestURL(u.URL, path)
	if joinErr != nil {
		return nil, joinErr
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request for %s: invalid URL", safeURLOrigin(reqURL))
	}
	req.Header.Set("User-Agent", "depsilo/0.1")

	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	if u.client == nil {
		return nil, fmt.Errorf("fetch %s: upstream client unavailable", safeURLOrigin(reqURL))
	}
	client := *u.client
	client.CheckRedirect = u.secureRedirectCheck(client.CheckRedirect)
	recovery, err := u.admitExchange()
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", safeURLOrigin(reqURL), err)
	}
	if recovery {
		defer u.finishPassiveRecovery()
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		u.Report(latency, false)
		return nil, fmt.Errorf("fetch %s: %w", safeURLOrigin(reqURL), redactedTransportError{cause: err})
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		u.Report(latency, resp.StatusCode < 500)
		return nil, fmt.Errorf("upstream %s returned %d for %s", u.Name, resp.StatusCode, safeURLOrigin(reqURL))
	}

	u.Report(latency, true)

	return &FetchResult{
		Body:         resp.Body,
		ContentType:  resp.Header.Get("Content-Type"),
		Size:         resp.ContentLength,
		StatusCode:   resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
		URL:          responseURL(resp, reqURL),
	}, nil
}

func responseURL(response *http.Response, fallback string) string {
	resolved := fallback
	if response != nil && response.Request != nil && response.Request.URL != nil {
		resolved = response.Request.URL.String()
	}
	parsed, err := url.Parse(resolved)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed.String()
}

// Report records a request result for latency/health tracking.
func (u *Upstream) Report(latency time.Duration, success bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.reportLocked(latency, success)
}

func (u *Upstream) reportLocked(latency time.Duration, success bool) {
	u.health.totalReqs++
	if success {
		u.health.successReqs++
	}
	u.health.successRate = float64(u.health.successReqs) / float64(u.health.totalReqs)

	// Exponential moving average for latency
	if u.health.avgLatency == 0 {
		u.health.avgLatency = latency
	} else {
		u.health.avgLatency = (u.health.avgLatency*7 + latency*3) / 10
	}

	// Mark unhealthy if success rate drops too low. A protocol-integrity
	// failure is intentionally sticky for this runtime object: routine traffic
	// cannot re-admit it to selection.
	u.health.healthy = !u.health.criticalFailureLatched && u.health.successRate > 0.3
}

// ReportCriticalFailure records a failed exchange and immediately removes the
// upstream from selection, regardless of historical success volume. It is for
// protocol-integrity violations where retrying the same origin could serve
// bytes from the wrong immutable object. The failure stays latched for this
// runtime object; publishing a replacement configuration or restarting creates
// a fresh object and clears the latch.
func (u *Upstream) ReportCriticalFailure(latency time.Duration) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.health.criticalFailureLatched = true
	u.reportLocked(latency, false)
}

// admitExchange is the linearization point between a caller holding an older
// Upstream pointer and a concurrent protocol-integrity failure. Exchanges
// admitted before the latch are already in flight; every later exchange is
// rejected even when it bypasses Selector and reuses that pointer directly.
func (u *Upstream) admitExchange() (bool, error) {
	if u == nil {
		return false, errors.New("nil upstream")
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.health.criticalFailureLatched {
		return false, errCriticalFailureLatched
	}
	recovery := u.recovery.reserved
	if recovery {
		u.recovery.reserved = false
		u.recovery.inFlight = true
	}
	return recovery, nil
}

func (u *Upstream) finishPassiveRecovery() {
	u.mu.Lock()
	u.recovery.inFlight = false
	u.mu.Unlock()
}

func (u *Upstream) applyProbe(result ProbeResult) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.health.applyProbe(result)
}

// HealthSnapshot returns one locked sample of mutable health fields.
func (u *Upstream) HealthSnapshot() HealthSnapshot {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return HealthSnapshot{
		Healthy:       u.health.healthy,
		AvgLatency:    u.health.avgLatency,
		SuccessRate:   u.health.successRate,
		LastCheckedAt: u.health.lastCheckedAt,
	}
}

// AvgLatency returns the current average latency.
func (u *Upstream) AvgLatency() time.Duration {
	return u.HealthSnapshot().AvgLatency
}

// SuccessRate returns the current success rate.
func (u *Upstream) SuccessRate() float64 {
	return u.HealthSnapshot().SuccessRate
}

// IsHealthy returns the current health status (thread-safe).
func (u *Upstream) IsHealthy() bool {
	return u.HealthSnapshot().Healthy
}

func buildClient(proxy, origin string) (*http.Client, error) {
	originURL, err := url.Parse(origin)
	if err != nil || !validOriginURL(origin) {
		return nil, fmt.Errorf("parse origin URL %s: invalid URL", safeURLOrigin(origin))
	}
	transport := &http.Transport{
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second, // timeout for headers only, not body
		TLSHandshakeTimeout:   15 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil || !validHTTPURL(proxy) {
			return nil, fmt.Errorf("parse proxy URL %s: invalid URL", safeURLOrigin(proxy))
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		zap.L().Info("using proxy", zap.String("proxy", safeURLOrigin(proxy)))
	} else {
		dialer := &net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		guard := &guardedDialer{
			origin:      originURL,
			resolver:    net.DefaultResolver,
			dialContext: dialer.DialContext,
		}
		transport.DialContext = guard.DialContext
	}

	return &http.Client{
		Transport: transport,
		// No client-level Timeout — large files (torch ~2GB) need unlimited
		// body read time. Connection/header timeouts are handled by transport.
	}, nil
}
