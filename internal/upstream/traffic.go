package upstream

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// Traffic measures dependency HTTP exchanges, not access-log misses. Health
// probes use the original client and are excluded. Retries, redirects and
// background fetches using the pool are included; object storage is not.
type Traffic struct {
	Requests int64 `json:"requests"`
	Bytes    int64 `json:"bytes"`
}
type trafficSlot struct {
	second int64
	Traffic
}
type trafficCounter struct {
	mu        sync.Mutex
	slots     [61]trafficSlot
	startedAt time.Time
}

func (c *trafficCounter) add(at time.Time, requests, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.startedAt.IsZero() {
		c.startedAt = at
	}
	second := at.Unix()
	slot := &c.slots[second%61]
	if slot.second != second {
		*slot = trafficSlot{second: second}
	}
	slot.Requests += requests
	slot.Bytes += bytes
}

// TrafficWindow is a bounded snapshot of actual dependency reads during the
// rolling window. CoverageSeconds is deliberately separate from the counters:
// a newly-created upstream with no requests is a sampling window, not a
// measured zero-rate window.
type TrafficWindow struct {
	Traffic
	CoverageSeconds int64
}

func (c *trafficCounter) window(now time.Time) TrafficWindow {
	c.mu.Lock()
	defer c.mu.Unlock()
	var result TrafficWindow
	if !c.startedAt.IsZero() {
		coverageStart := c.startedAt
		if candidate := now.Add(-60 * time.Second); candidate.After(coverageStart) {
			coverageStart = candidate
		}
		if seconds := int64(now.Sub(coverageStart) / time.Second); seconds > 0 {
			result.CoverageSeconds = seconds
			if result.CoverageSeconds > 60 {
				result.CoverageSeconds = 60
			}
		}
	}
	for _, slot := range c.slots {
		if slot.second >= now.Unix()-60 && slot.second < now.Unix() {
			result.Requests += slot.Requests
			result.Bytes += slot.Bytes
		}
	}
	return result
}

// RecentTrafficWindow returns both measured bytes/requests and the amount of
// time for which this in-memory collector has been able to observe traffic.
func (u *Upstream) RecentTrafficWindow(now time.Time) TrafficWindow {
	return u.traffic.window(now)
}

func (u *Upstream) RecentTraffic(now time.Time) Traffic {
	return u.RecentTrafficWindow(now).Traffic
}

type trafficTransport struct {
	base    http.RoundTripper
	counter *trafficCounter
}

func (t trafficTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.counter.add(time.Now(), 1, 0)
	response, err := t.base.RoundTrip(req)
	if response != nil && response.Body != nil {
		response.Body = &trafficBody{ReadCloser: response.Body, counter: t.counter}
	}
	return response, err
}

type trafficBody struct {
	io.ReadCloser
	counter *trafficCounter
}

func (b *trafficBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.counter.add(time.Now(), 0, int64(n))
	}
	return n, err
}
func (u *Upstream) measuredTransport() http.RoundTripper {
	base := u.client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	return trafficTransport{base: base, counter: &u.traffic}
}
