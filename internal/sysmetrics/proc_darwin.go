//go:build darwin

package sysmetrics

import "syscall"

// platformReadRaw reports real process CPU time and peak RSS on darwin (via
// getrusage). macOS exposes no portable current-RSS reading (kern.proc's
// xrssize is unreliable, and task_info needs cgo), so the value is reported with
// rss_basis="peak" and the UI labels it as a peak rather than current usage.
// No container memory limit is available.
func platformReadRaw() rawSample {
	out := rawSample{
		memReason: "darwin has no container memory limit",
	}
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		out.cpuReason = "getrusage is unavailable"
		return out
	}
	out.cpuSeconds = float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 +
		float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
	out.cpuOK = true
	// ru_maxrss is in bytes on darwin.
	if usage.Maxrss > 0 {
		out.rssBytes = int64(usage.Maxrss)
		out.rssBasis = "peak"
		out.rssOK = true
	} else {
		out.rssReason = "peak RSS is unavailable from getrusage"
	}
	return out
}
