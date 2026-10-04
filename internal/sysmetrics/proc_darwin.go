//go:build darwin

package sysmetrics

import "syscall"

// platformReadRaw reports real process CPU time on darwin (via getrusage) but
// deliberately does not report process memory: getrusage only exposes peak
// RSS, and presenting a peak as current memory usage would be dishonest. The
// Overview shows the Go runtime's own labeled memory figure instead.
func platformReadRaw() rawSample {
	out := rawSample{
		rssReason: "darwin exposes only peak RSS, not a current process RSS",
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
	return out
}
