//go:build linux

package sysmetrics

import (
	"os"
	"strconv"
	"strings"
)

// userHZ is the conventional Linux USER_HZ clock-tick frequency. Without cgo
// we cannot ask sysconf, and 100 has been the fixed value on every mainstream
// Linux architecture for years.
const userHZ = 100.0

func platformReadRaw() rawSample {
	out := rawSample{}

	// CPU: /proc/self/stat fields 14 (utime) and 15 (stime), in clock ticks.
	if data, err := os.ReadFile("/proc/self/stat"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 15 {
			utime, utimeErr := strconv.ParseFloat(fields[13], 64)
			stime, stimeErr := strconv.ParseFloat(fields[14], 64)
			if utimeErr == nil && stimeErr == nil {
				out.cpuSeconds = (utime + stime) / userHZ
				out.cpuOK = true
			}
		}
	}
	if !out.cpuOK {
		out.cpuReason = "process CPU is unavailable from /proc/self/stat"
	}

	// RSS: /proc/self/statm field 2 is resident pages.
	if data, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 2 {
			if pages, parseErr := strconv.ParseInt(fields[1], 10, 64); parseErr == nil && pages >= 0 {
				out.rssBytes = pages * int64(os.Getpagesize())
				out.rssBasis = "process"
				out.rssOK = true
			}
		}
	}
	if !out.rssOK {
		out.rssReason = "process RSS is unavailable from /proc/self/statm"
	}

	if used, limit, ok := readCgroupV2Memory(); ok {
		out.memUsed, out.memLimit, out.memBasis, out.memOK = used, limit, "cgroup_v2", true
	} else if used, limit, ok := readCgroupV1Memory(); ok {
		out.memUsed, out.memLimit, out.memBasis, out.memOK = used, limit, "cgroup_v1", true
	} else {
		out.memReason = "no cgroup memory limit is exposed on this host"
	}

	return out
}

func readCgroupV2Memory() (used int64, limit int64, ok bool) {
	limit, ok = readCgroupLimit("/sys/fs/cgroup/memory.max")
	if !ok {
		return 0, 0, false
	}
	used, err := readCgroupCounter("/sys/fs/cgroup/memory.current")
	if err != nil {
		return 0, 0, false
	}
	return used, limit, true
}

func readCgroupV1Memory() (used int64, limit int64, ok bool) {
	limit, ok = readCgroupLimit("/sys/fs/cgroup/memory/memory.limit_in_bytes")
	if !ok {
		return 0, 0, false
	}
	used, err := readCgroupCounter("/sys/fs/cgroup/memory/memory.usage_in_bytes")
	if err != nil {
		return 0, 0, false
	}
	return used, limit, true
}

func readCgroupLimit(path string) (int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "max" {
		return 0, false
	}
	limit, err := strconv.ParseInt(value, 10, 64)
	// cgroup v1 uses a near-INT64_MAX sentinel for "unlimited".
	if err != nil || limit <= 0 || limit > 1<<60 {
		return 0, false
	}
	return limit, true
}

func readCgroupCounter(path string) (int64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || value < 0 {
		return 0, strconv.ErrSyntax
	}
	return value, nil
}
