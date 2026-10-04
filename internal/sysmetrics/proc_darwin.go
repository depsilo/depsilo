//go:build darwin

package sysmetrics

import (
	"os"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

// macOS exposes the current resident size through libproc (proc_pid_rusage)
// or Mach task_info; neither is a POSIX syscall, so the libproc symbol is bound
// with purego while CGO stays disabled. RUSAGE_INFO_V2 carries ri_resident_size
// (current RSS) and ri_phys_footprint (Activity Monitor's "Memory").
const rusageInfoV2 = 2

type rusageInfoV2Data struct {
	UUID                [16]byte
	UserTime            uint64
	SystemTime          uint64
	PkgIdleWkups        uint64
	InterruptWkups      uint64
	Pageins             uint64
	WiredSize           uint64
	ResidentSize        uint64
	PhysFootprint       uint64
	ProcStartAbstime    uint64
	ProcExitAbstime     uint64
	ChildUserTime       uint64
	ChildSystemTime     uint64
	ChildPkgIdleWkups   uint64
	ChildInterruptWkups uint64
	ChildPageins        uint64
	ChildElapsedAbstime uint64
	DiskioBytesRead     uint64
	DiskioBytesWritten  uint64
}

var (
	procAvailable bool
	procPidRusage func(pid int32, flavor int32, buffer unsafe.Pointer) int32
)

func init() { bindProcPidRusage() }

// bindProcPidRusage resolves proc_pid_rusage from libSystem. Any failure leaves
// procAvailable false so the sampler falls back to peak RSS.
func bindProcPidRusage() {
	defer func() { _ = recover() }() // RegisterFunc panics on an unexpected signature
	library, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY)
	if err != nil {
		return
	}
	symbol, err := purego.Dlsym(library, "proc_pid_rusage")
	if err != nil {
		return
	}
	purego.RegisterFunc(&procPidRusage, symbol)
	procAvailable = procPidRusage != nil
}

// currentResidentBytes returns the current resident set size in bytes.
func currentResidentBytes() (int64, bool) {
	if !procAvailable {
		return 0, false
	}
	info := rusageInfoV2Data{}
	if procPidRusage(int32(os.Getpid()), rusageInfoV2, unsafe.Pointer(&info)) != 0 {
		return 0, false
	}
	if info.ResidentSize == 0 {
		return 0, false
	}
	return int64(info.ResidentSize), true
}

// platformReadRaw reports real process CPU time, current RSS (libproc) and no
// container memory limit. If libproc is unavailable it falls back to the
// getrusage peak, labelled as a peak rather than current usage.
func platformReadRaw() rawSample {
	out := rawSample{
		memReason: "darwin has no container memory limit",
	}
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		out.cpuReason = "getrusage is unavailable"
		out.rssReason = "process memory is unavailable"
		return out
	}
	out.cpuSeconds = float64(usage.Utime.Sec) + float64(usage.Utime.Usec)/1e6 +
		float64(usage.Stime.Sec) + float64(usage.Stime.Usec)/1e6
	out.cpuOK = true

	if resident, ok := currentResidentBytes(); ok {
		out.rssBytes = resident
		out.rssBasis = "process"
		out.rssOK = true
		return out
	}
	// ru_maxrss is in bytes on darwin; it is a high-water mark.
	if usage.Maxrss > 0 {
		out.rssBytes = int64(usage.Maxrss)
		out.rssBasis = "peak"
		out.rssOK = true
		return out
	}
	out.rssReason = "process memory is unavailable"
	return out
}
