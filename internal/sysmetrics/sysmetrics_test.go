package sysmetrics

import (
	"runtime"
	"testing"
)

// TestSamplerReportsRealProcessMemory guards the platform contract: on the
// platforms where Depsilo reads process memory, the sample must be a real
// positive figure with an honest basis, never the platform-reason fallback.
func TestSamplerReportsRealProcessMemory(t *testing.T) {
	sampler := NewSampler("", false)
	sampler.SampleNow()
	sampler.SampleNow()
	snapshot := sampler.Snapshot()

	if snapshot.SampledAt.IsZero() {
		t.Fatal("sampler produced no sample")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return
	}
	if !snapshot.Process.Memory.Supported {
		t.Fatalf("memory reported unsupported on %s: %+v", runtime.GOOS, snapshot.Process.Memory)
	}
	if snapshot.Process.RSSBytes == nil || *snapshot.Process.RSSBytes <= 0 {
		t.Fatalf("rss bytes = %v, want a positive value", snapshot.Process.RSSBytes)
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	t.Logf("%s rss=%d bytes basis=%s (runtime sys=%d)", runtime.GOOS, *snapshot.Process.RSSBytes, snapshot.Process.RSSBasis, stats.Sys)
	switch snapshot.Process.RSSBasis {
	case "process", "peak":
	default:
		t.Fatalf("rss basis = %q, want process or peak", snapshot.Process.RSSBasis)
	}
}

func TestSamplerReportsLocalDiskCapacity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("disk sampling is not implemented on windows")
	}
	sampler := NewSampler(t.TempDir(), true)
	sampler.SampleNow()
	snapshot := sampler.Snapshot()
	if !snapshot.Disk.Supported {
		t.Fatalf("disk reported unsupported: %s", snapshot.Disk.Reason)
	}
	if snapshot.Disk.TotalBytes <= 0 || snapshot.Disk.FreeBytes < 0 {
		t.Fatalf("disk totals = total %d free %d", snapshot.Disk.TotalBytes, snapshot.Disk.FreeBytes)
	}
}
