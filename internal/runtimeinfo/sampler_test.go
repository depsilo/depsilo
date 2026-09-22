package runtimeinfo

import (
	"errors"
	"testing"
	"time"
)

func TestSamplerDistinguishesInitialSamplingAndReadyCPU(t *testing.T) {
	at := time.Unix(100, 0)
	reads := []processSample{{CPUTime: 100 * time.Millisecond, RSS: 10}, {CPUTime: 300 * time.Millisecond, RSS: 20}}
	s := &Sampler{read: func() (processSample, error) { sample := reads[0]; reads = reads[1:]; return sample, nil }}
	first := s.Snapshot(at)
	if first.CPUState != "sampling" || first.CPUPercent != nil || first.MemoryState != "ready" {
		t.Fatalf("first snapshot = %#v", first)
	}
	second := s.Snapshot(at.Add(5 * time.Second))
	if second.CPUState != "ready" || second.CPUPercent == nil || *second.CPUPercent != 4 {
		t.Fatalf("second snapshot = %#v, want 4%%", second)
	}
	if second.RSSBytes == nil || *second.RSSBytes != 20 {
		t.Fatalf("rss = %#v", second.RSSBytes)
	}
}

func TestSamplerThrottlesReadsAndReportsErrors(t *testing.T) {
	calls := 0
	s := &Sampler{read: func() (processSample, error) { calls++; return processSample{}, errors.New("no access") }}
	at := time.Unix(100, 0)
	if got := s.Snapshot(at); got.CPUState != "error" || got.MemoryState != "error" {
		t.Fatalf("error snapshot = %#v", got)
	}
	_ = s.Snapshot(at.Add(time.Second))
	if calls != 1 {
		t.Fatalf("calls = %d, want one throttled read", calls)
	}
}

func TestSamplerKeepsLastValuesWhenRefreshFails(t *testing.T) {
	reads := []struct {
		sample processSample
		err    error
	}{{sample: processSample{CPUTime: time.Second, RSS: 42}}, {err: errors.New("temporary")}}
	s := &Sampler{read: func() (processSample, error) {
		read := reads[0]
		reads = reads[1:]
		return read.sample, read.err
	}}
	first := s.Snapshot(time.Unix(100, 0))
	if first.RSSBytes == nil || *first.RSSBytes != 42 {
		t.Fatalf("first = %#v", first)
	}
	failed := s.Snapshot(time.Unix(106, 0))
	if failed.CPUState != "error" || failed.MemoryState != "error" || failed.RSSBytes == nil || *failed.RSSBytes != 42 {
		t.Fatalf("failed = %#v, want stale value", failed)
	}
}
