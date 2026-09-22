//go:build !windows

package runtimeinfo

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
)

func readProcessSample() (processSample, error) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return processSample{}, fmt.Errorf("read process usage: %w", err)
	}
	cpu := timevalDuration(usage.Utime) + timevalDuration(usage.Stime)
	rss := usage.Maxrss
	// Linux reports KiB while Darwin reports bytes.
	if runtime.GOOS == "linux" {
		rss *= 1024
	}
	if rss < 0 {
		return processSample{}, fmt.Errorf("read process rss: invalid value %d", rss)
	}
	return processSample{CPUTime: cpu, RSS: rss}, nil
}

func timevalDuration(value syscall.Timeval) time.Duration {
	return time.Duration(value.Sec)*time.Second + time.Duration(value.Usec)*time.Microsecond
}
