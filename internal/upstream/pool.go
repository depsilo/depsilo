package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"

	"depslio/internal/config"
)

// Upstream represents a single upstream source with its HTTP client.
type Upstream struct {
	Name     string
	URL      string
	Proxy    string
	Priority int
	Healthy  bool
	client   *http.Client

	mu          sync.RWMutex
	avgLatency  time.Duration
	successRate float64
	totalReqs   int64
	successReqs int64
}

// FetchResult holds the response from an upstream fetch.
type FetchResult struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
	StatusCode  int
}

// Pool manages a set of upstreams for a given adapter type.
type Pool struct {
	upstreams []*Upstream
}

// NewPool creates an upstream pool from config.
func NewPool(cfgs []config.UpstreamConfig) (*Pool, error) {
	upstreams := make([]*Upstream, 0, len(cfgs))
	for _, cfg := range cfgs {
		client, err := buildClient(cfg.Proxy)
		if err != nil {
			return nil, fmt.Errorf("build client for %s: %w", cfg.Name, err)
		}
		upstreams = append(upstreams, &Upstream{
			Name:        cfg.Name,
			URL:         cfg.URL,
			Proxy:       cfg.Proxy,
			Priority:    cfg.Priority,
			Healthy:     true,
			client:      client,
			successRate: 1.0,
		})
		zap.L().Info("registered upstream",
			zap.String("name", cfg.Name),
			zap.String("url", cfg.URL),
			zap.Int("priority", cfg.Priority),
			zap.String("proxy", cfg.Proxy),
		)
	}
	return &Pool{upstreams: upstreams}, nil
}

// Upstreams returns all upstreams.
func (p *Pool) Upstreams() []*Upstream {
	return p.upstreams
}

// Fetch performs an HTTP GET to the upstream, joining the given path.
func (u *Upstream) Fetch(ctx context.Context, path string) (*FetchResult, error) {
	reqURL := u.URL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "depslio/0.1")

	start := time.Now()
	resp, err := u.client.Do(req)
	latency := time.Since(start)

	if err != nil {
		u.Report(latency, false)
		return nil, fmt.Errorf("fetch %s: %w", reqURL, err)
	}

	if resp.StatusCode >= 500 {
		resp.Body.Close()
		u.Report(latency, false)
		return nil, fmt.Errorf("upstream %s returned %d", u.Name, resp.StatusCode)
	}

	u.Report(latency, true)

	return &FetchResult{
		Body:        resp.Body,
		ContentType: resp.Header.Get("Content-Type"),
		Size:        resp.ContentLength,
		StatusCode:  resp.StatusCode,
	}, nil
}

// Report records a request result for latency/health tracking.
func (u *Upstream) Report(latency time.Duration, success bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.totalReqs++
	if success {
		u.successReqs++
	}
	u.successRate = float64(u.successReqs) / float64(u.totalReqs)

	// Exponential moving average for latency
	if u.avgLatency == 0 {
		u.avgLatency = latency
	} else {
		u.avgLatency = (u.avgLatency*7 + latency*3) / 10
	}

	// Mark unhealthy if success rate drops too low
	u.Healthy = u.successRate > 0.3
}

// AvgLatency returns the current average latency.
func (u *Upstream) AvgLatency() time.Duration {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.avgLatency
}

// SuccessRate returns the current success rate.
func (u *Upstream) SuccessRate() float64 {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.successRate
}

func buildClient(proxy string) (*http.Client, error) {
	transport := &http.Transport{
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("parse proxy url: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		zap.L().Info("using proxy", zap.String("proxy", proxy))
	}

	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}, nil
}
