//go:build linux

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func daemonProcessIdentity(pid int) (string, error) {
	if pid <= 0 {
		return "", fmt.Errorf("invalid PID %d", pid)
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", fmt.Errorf("read process stat: %w", err)
	}
	// comm (field 2) is parenthesized and may itself contain spaces or ')'.
	// Splitting after the final ") " makes fields[19] the starttime (field 22).
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		return "", errorsForMalformedProcStat(pid)
	}
	fields := strings.Fields(string(stat)[end+2:])
	if len(fields) <= 19 {
		return "", errorsForMalformedProcStat(pid)
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read kernel boot identity: %w", err)
	}
	return strings.TrimSpace(string(bootID)) + ":" + fields[19], nil
}

func errorsForMalformedProcStat(pid int) error {
	return fmt.Errorf("malformed /proc/%d/stat", pid)
}
