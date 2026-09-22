// Package runtimeinfo provides a small, shared process resource sampler for
// the admin dashboard. It is deliberately pull-based and throttled so browser
// refreshes cannot start an unbounded stream of OS probes.
package runtimeinfo

import (
	"sync"
	"time"
)

const minSampleInterval = 5 * time.Second

type processSample struct {
	CPUTime time.Duration
	RSS     int64
}

type Snapshot struct {
	CPUState        string
	CPUPercent      *float64
	CPUWindow       int
	CPUSampledAt    time.Time
	MemoryState     string
	RSSBytes        *int64
	MemorySampledAt time.Time
}

type Sampler struct {
	mu         sync.Mutex
	read       func() (processSample, error)
	lastRead   time.Time
	last       Snapshot
	previous   processSample
	previousAt time.Time
}

// New returns a process sampler shared by one DashboardHandler.
func New() *Sampler { return &Sampler{read: readProcessSample} }

// Snapshot returns the latest bounded snapshot. The first CPU read is marked
// sampling because a percentage needs two process/wall-clock samples.
func (s *Sampler) Snapshot(now time.Time) Snapshot {
	if s == nil {
		return Snapshot{CPUState: "unsupported", MemoryState: "unsupported"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.read == nil {
		s.read = readProcessSample
	}
	if !s.lastRead.IsZero() && now.Sub(s.lastRead) < minSampleInterval {
		return s.last
	}
	s.lastRead = now
	current, err := s.read()
	if err != nil {
		// Keep the last successful values so a transient probe failure does
		// not turn a known reading into a misleading empty value.
		out := s.last
		out.CPUState, out.MemoryState = "error", "error"
		s.last = out
		return out
	}
	out := Snapshot{
		CPUState: "sampling", MemoryState: "ready", RSSBytes: int64ptr(current.RSS), MemorySampledAt: now,
	}
	if !s.previousAt.IsZero() {
		wall := now.Sub(s.previousAt)
		cpu := current.CPUTime - s.previous.CPUTime
		if wall > 0 && cpu >= 0 {
			percent := float64(cpu) / float64(wall) * 100
			out.CPUState, out.CPUPercent, out.CPUWindow = "ready", &percent, int(wall.Round(time.Second)/time.Second)
			out.CPUSampledAt = now
		}
	}
	s.previous, s.previousAt, s.last = current, now, out
	return out
}

func int64ptr(value int64) *int64 { return &value }
