//go:build !linux && !darwin

package sysmetrics

// platformReadRaw reports every process resource as unsupported. This is the
// honest result on platforms where Depsilo cannot read process CPU or RSS
// without a platform-specific dependency.
func platformReadRaw() rawSample {
	return rawSample{
		cpuReason: "process CPU sampling is not implemented on this platform",
		rssReason: "process memory sampling is not implemented on this platform",
		memReason: "container memory limits are not available on this platform",
	}
}
