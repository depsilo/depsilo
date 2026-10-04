// Package sysmetrics samples bounded process and storage resource facts for
// the Admin Overview. It is capability-aware on purpose: on a platform where a
// fact cannot be read honestly (for example process RSS on darwin without
// cgroup/procfs), it reports "unsupported" with a reason instead of
// substituting a different measurement under the same label.
//
// Sampling is process-wide and shared: every Admin request reads the most
// recent sample rather than triggering a new read.
package sysmetrics

import (
	"context"
	"os"
	"runtime"
	"sync"
	"time"
)

// DefaultInterval is the process sampling cadence. It is long enough to be
// cheap and short enough for the Overview to feel live.
const DefaultInterval = 5 * time.Second

// Capability describes whether one measurement is actually collected.
type Capability struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason,omitempty"`
}

// Process is the sampled process (or container) resource block.
type Process struct {
	CPU Capability `json:"cpu"`
	// CPUPercent is on a single-core basis: 100% means one full core.
	CPUPercent *float64 `json:"cpu_percent,omitempty"`
	CPUCores   int      `json:"cpu_cores,omitempty"`

	Memory   Capability `json:"memory"`
	RSSBytes *int64     `json:"rss_bytes,omitempty"`
	// RSSBasis is "process" for current RSS or "peak" for peak RSS.
	RSSBasis string `json:"rss_basis,omitempty"`

	MemoryBasis      string `json:"memory_basis,omitempty"`
	MemoryUsedBytes  *int64 `json:"memory_used_bytes,omitempty"`
	MemoryLimitBytes *int64 `json:"memory_limit_bytes,omitempty"`
}

// Disk is the storage-path filesystem sample.
type Disk struct {
	Capability
	Path       string `json:"path,omitempty"`
	TotalBytes int64  `json:"total_bytes,omitempty"`
	FreeBytes  int64  `json:"free_bytes,omitempty"`
}

// Snapshot is one consistent sample.
type Snapshot struct {
	SampledAt       time.Time `json:"sampled_at"`
	IntervalSeconds int       `json:"interval_seconds"`
	Process         Process   `json:"process"`
	Disk            Disk      `json:"disk"`
}

type rawSample struct {
	cpuSeconds float64
	cpuOK      bool
	cpuReason  string

	rssBytes  int64
	rssBasis  string
	rssOK     bool
	rssReason string

	memUsed   int64
	memLimit  int64
	memBasis  string
	memOK     bool
	memReason string
}

// readRaw is a package var so tests can substitute a deterministic sample.
var readRaw = platformReadRaw

// Sampler owns the shared sampling loop.
type Sampler struct {
	interval time.Duration
	now      func() time.Time

	diskPath    string
	diskEnabled bool

	mu       sync.RWMutex
	latest   Snapshot
	prevCPU  float64
	prevAt   time.Time
	havePrev bool
}

// NewSampler builds a sampler for the given storage path. When diskEnabled is
// false (for example S3-backed storage with no local cache directory) disk
// capacity is reported as unsupported rather than guessed.
func NewSampler(diskPath string, diskEnabled bool) *Sampler {
	return &Sampler{
		interval:    DefaultInterval,
		now:         func() time.Time { return time.Now().UTC() },
		diskPath:    diskPath,
		diskEnabled: diskEnabled,
	}
}

// SetInterval overrides the sampling cadence. Test-only helper.
func (s *Sampler) SetInterval(d time.Duration) {
	if s == nil || d <= 0 {
		return
	}
	s.interval = d
}

// Start samples immediately and then on the configured interval until ctx is
// cancelled. It is safe to call once per process.
func (s *Sampler) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.SampleNow()
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.SampleNow()
		}
	}
}

// SampleNow takes one sample and publishes it.
func (s *Sampler) SampleNow() {
	if s == nil {
		return
	}
	now := s.now()
	raw := readRaw()

	s.mu.Lock()
	defer s.mu.Unlock()

	process := s.processLocked(raw, now)
	s.latest = Snapshot{
		SampledAt:       now,
		IntervalSeconds: int(s.interval / time.Second),
		Process:         process,
		Disk:            s.diskLocked(),
	}
}

func (s *Sampler) processLocked(raw rawSample, now time.Time) Process {
	out := Process{CPUCores: runtime.NumCPU()}
	if raw.cpuOK {
		out.CPU = Capability{Supported: true}
		if s.havePrev {
			elapsed := now.Sub(s.prevAt).Seconds()
			delta := raw.cpuSeconds - s.prevCPU
			if elapsed > 0 && delta >= 0 {
				percent := delta / elapsed * 100
				if percent < 0 {
					percent = 0
				}
				out.CPUPercent = &percent
			}
		}
		s.prevCPU = raw.cpuSeconds
		s.prevAt = now
		s.havePrev = true
	} else {
		out.CPU = Capability{Supported: false, Reason: raw.cpuReason}
		s.havePrev = false
	}

	if raw.rssOK {
		out.Memory = Capability{Supported: true}
		rss := raw.rssBytes
		out.RSSBytes = &rss
		out.RSSBasis = raw.rssBasis
	} else {
		out.Memory = Capability{Supported: false, Reason: raw.rssReason}
	}
	if raw.memOK {
		used, limit := raw.memUsed, raw.memLimit
		out.MemoryUsedBytes = &used
		out.MemoryLimitBytes = &limit
		out.MemoryBasis = raw.memBasis
	}
	return out
}

func (s *Sampler) diskLocked() Disk {
	if !s.diskEnabled || s.diskPath == "" {
		return Disk{Capability: Capability{Supported: false, Reason: "storage path is not a local filesystem"}}
	}
	total, free, err := platformDisk(s.diskPath)
	if err != nil {
		return Disk{Capability: Capability{Supported: false, Reason: err.Error()}, Path: s.diskPath}
	}
	return Disk{Capability: Capability{Supported: true}, Path: s.diskPath, TotalBytes: total, FreeBytes: free}
}

// Snapshot returns the most recent sample.
func (s *Sampler) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// GoRuntimeMemory exposes the Go runtime's own memory accounting. It is always
// available and is deliberately separate from process RSS: callers must label
// it as runtime memory, never as the program's total footprint.
func GoRuntimeMemory() (heapAllocBytes int64, sysBytes int64) {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return int64(stats.HeapAlloc), int64(stats.Sys)
}

// Hostname is a small helper the runtime endpoint includes for context.
func Hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}
