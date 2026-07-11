package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

// ── API response types ──────────────────────────────────────────

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Uptime  string `json:"uptime"`
}

type statsResponse struct {
	Service struct {
		Version       string `json:"version"`
		UptimeSeconds int64  `json:"uptime_seconds"`
		Status        string `json:"status"`
	} `json:"service"`
	Today struct {
		TotalRequests int64   `json:"total_requests"`
		HitCount      int64   `json:"hit_count"`
		MissCount     int64   `json:"miss_count"`
		HitRate       float64 `json:"hit_rate"`
		BytesServed   int64   `json:"bytes_served"`
		BytesSaved    int64   `json:"bytes_saved"`
	} `json:"today"`
	Cache struct {
		TotalFiles     int64 `json:"total_files"`
		TotalSizeBytes int64 `json:"total_size_bytes"`
	} `json:"cache"`
	Upstreams []struct {
		Name         string  `json:"name"`
		Adapter      string  `json:"adapter"`
		Healthy      bool    `json:"healthy"`
		AvgLatencyMs int64   `json:"avg_latency_ms"`
		SuccessRate  float64 `json:"success_rate"`
	} `json:"upstreams"`
}

type cacheDistResponse struct {
	TotalSize    int64   `json:"total_size"`
	MaxSize      int64   `json:"max_size"`
	UsagePercent float64 `json:"usage_percent"`
	ByType       []struct {
		Type      string `json:"type"`
		Size      int64  `json:"size"`
		FileCount int64  `json:"file_count"`
	} `json:"by_type"`
}

// ── runStatus ───────────────────────────────────────────────────

func runStatus(args []string) int {
	jsonMode, _ := stripJSONFlag(args)
	baseURL := getServerURL()

	// 1. Health check
	var health healthResponse
	status, err := getJSON(baseURL+"/health", &health)
	if err != nil {
		if jsonMode {
			printJSON(map[string]any{
				"ok":    false,
				"error": "unreachable",
				"url":   baseURL,
				"hint":  err.Error(),
			})
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: cannot connect to Depsilo at %s\n", baseURL)
		fmt.Fprintf(os.Stderr, "  %v\n", err)
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Make sure Depsilo is running. Use one of:")
		fmt.Fprintln(os.Stderr, "  depsilo start --daemon    (start in background)")
		fmt.Fprintln(os.Stderr, "  depsilo serve             (start in foreground)")
		fmt.Fprintln(os.Stderr, "  docker run -d -p 23333:23333 depsilo/depsilo")
		return 1
	}
	if status != 200 || health.Status != "healthy" {
		if jsonMode {
			printJSON(map[string]any{
				"ok":          false,
				"error":       "unhealthy",
				"http_status": status,
				"status":      health.Status,
			})
			return 1
		}
		fmt.Fprintf(os.Stderr, "Error: service not healthy (HTTP %d, status: %s)\n", status, health.Status)
		return 1
	}

	// In JSON mode, collect every response into one object and emit at the end.
	var stats statsResponse
	statsStatus, statsErr := getJSON(baseURL+"/api/v1/stats", &stats)
	var dist cacheDistResponse
	distStatus, distErr := getJSON(baseURL+"/api/v1/admin/cache/distribution", &dist)

	if jsonMode {
		out := map[string]any{
			"ok":      true,
			"url":     baseURL,
			"version": health.Version,
			"status":  health.Status,
			"uptime":  health.Uptime,
		}
		if statsErr == nil && statsStatus == 200 {
			out["stats"] = stats
		}
		if distErr == nil && distStatus == 200 {
			out["cache_distribution"] = dist
		}
		printJSON(out)
		return 0
	}

	fmt.Printf("Depsilo %s\n", health.Version)
	fmt.Printf("Status:  %s\n", health.Status)
	fmt.Printf("Uptime:  %s\n\n", health.Uptime)

	if statsErr == nil && statsStatus == 200 {
		hitPct := stats.Today.HitRate * 100
		fmt.Printf("Today's Activity:\n")
		fmt.Printf("  Requests:    %s total (%s hits, %s misses)\n",
			formatCount(stats.Today.TotalRequests),
			formatCount(stats.Today.HitCount),
			formatCount(stats.Today.MissCount))
		fmt.Printf("  Hit Rate:    %.1f%%\n", hitPct)
		fmt.Printf("  Data Served: %s\n", formatBytes(stats.Today.BytesServed))
		fmt.Printf("  Data Saved:  %s (from cache)\n", formatBytes(stats.Today.BytesSaved))
		fmt.Println()
	}

	if distErr == nil && distStatus == 200 {
		fmt.Printf("Cache: %s / %s (%.1f%%)\n",
			formatBytes(dist.TotalSize),
			formatBytes(dist.MaxSize),
			dist.UsagePercent)

		totalFiles := int64(0)
		for _, bt := range dist.ByType {
			totalFiles += bt.FileCount
		}
		fmt.Printf("  Files: %s entries across %d ecosystems\n",
			formatCount(totalFiles), len(dist.ByType))
		for _, bt := range dist.ByType {
			fmt.Printf("  %-12s %s  (%s files)\n",
				bt.Type+":", formatBytes(bt.Size), formatCount(bt.FileCount))
		}
		fmt.Println()
	}

	// 4. Upstreams
	if len(stats.Upstreams) > 0 {
		fmt.Println("Upstreams:")
		for _, u := range stats.Upstreams {
			icon := "✓"
			if !u.Healthy {
				icon = "✗"
			}
			latency := time.Duration(u.AvgLatencyMs) * time.Millisecond
			fmt.Printf("  %s %s/%-20s %8s  %.0f%% success\n",
				icon, u.Adapter, u.Name, latency.Round(time.Millisecond), u.SuccessRate*100)
		}
	}

	return 0
}

// ── runActivate ─────────────────────────────────────────────────

func runActivate(args []string) int {
	baseURL := getServerURL()

	// Parse flags
	shell := "bash"
	ecosystems := ""
	for i, a := range args {
		if a == "--shell" || a == "-s" {
			if i+1 < len(args) {
				shell = args[i+1]
			}
		} else if a == "--eco" || a == "-e" {
			if i+1 < len(args) {
				ecosystems = args[i+1]
			}
		}
	}

	isFish := shell == "fish"

	// Build environment map
	envVars := map[string]string{
		"DEPSILO_URL": baseURL,
	}

	if ecosystems == "" || contains(ecosystems, "pypi") {
		envVars["PIP_INDEX_URL"] = baseURL + "/pypi/simple/"
		if host := trustedHostForPlainHTTP(baseURL); host != "" {
			envVars["PIP_TRUSTED_HOST"] = host
		}
	}
	if ecosystems == "" || contains(ecosystems, "npm") {
		envVars["npm_config_registry"] = baseURL + "/npm/"
	}
	if ecosystems == "" || contains(ecosystems, "go") {
		envVars["GOPROXY"] = baseURL + "/go,direct"
	}
	if ecosystems == "" || contains(ecosystems, "cargo") {
		envVars["CARGO_REGISTRIES_DEPSILO_INDEX"] = "sparse+" + baseURL + "/crates/"
	}
	if ecosystems == "" || contains(ecosystems, "apt") {
		envVars["DEPSILO_APT_MIRROR"] = baseURL + "/apt"
	}
	if ecosystems == "" || contains(ecosystems, "maven") {
		envVars["MAVEN_MIRROR_URL"] = baseURL + "/maven/"
	}
	if ecosystems == "" || contains(ecosystems, "conda") {
		envVars["CONDA_CHANNEL_DEPSILO"] = baseURL + "/conda"
	}

	// Output
	for k, v := range envVars {
		if isFish {
			fmt.Printf("set -x %s %s\n", k, shellQuote(v))
		} else {
			fmt.Printf("export %s=%s\n", k, shellQuote(v))
		}
	}

	fmt.Println()
	if isFish {
		fmt.Printf("# Usage: source (depsilo activate | psub)\n")
	} else {
		fmt.Printf("# Usage: eval \"$(depsilo activate)\"\n")
		fmt.Printf("#        source <(depsilo activate)\n")
	}

	return 0
}

func trustedHostForPlainHTTP(baseURL string) string {
	parsed, err := url.Parse(baseURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") {
		return ""
	}
	return parsed.Host
}

func contains(list, item string) bool {
	for _, s := range strings.Split(list, ",") {
		if strings.TrimSpace(s) == item {
			return true
		}
	}
	return false
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// ── JSON pretty-print helper (for future --json flag) ──────────

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(v)
}
