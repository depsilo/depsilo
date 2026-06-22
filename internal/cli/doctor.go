package cli

import (
	"fmt"
	"os"
	"strings"

	"depsilo/internal/version"
)

// doctor runs a sequence of read-only checks against a live Depsilo
// instance: reachability, health, version skew, storage usage, upstream
// health, and recent cache hit rate. It surfaces actionable hints so a
// self-hosted operator can fix the most common problems without grepping
// server logs.

type checkLevel string

const (
	levelOK   checkLevel = "ok"
	levelWarn checkLevel = "warn"
	levelFail checkLevel = "fail"
	levelInfo checkLevel = "info"
)

type doctorCheck struct {
	Name    string     `json:"name"`
	Level   checkLevel `json:"level"`
	Message string     `json:"message"`
	Hint    string     `json:"hint,omitempty"`
}

func runDoctor(args []string) int {
	jsonMode, _ := stripJSONFlag(args)
	baseURL := getServerURL()

	checks := []doctorCheck{}
	hasFail := false

	// 1. Reachability + health
	var health healthResponse
	httpStatus, err := getJSON(baseURL+"/health", &health)
	if err != nil || httpStatus != 200 {
		msg := "unreachable at " + baseURL
		if err != nil {
			msg = err.Error()
		}
		checks = append(checks, doctorCheck{
			Name:    "Service reachable",
			Level:   levelFail,
			Message: msg,
			Hint:    "start the server: depsilo serve  (or: depsilo start --daemon)",
		})
		return finalizeDoctor(checks, jsonMode, baseURL, true)
	}
	checks = append(checks, doctorCheck{
		Name:    "Service reachable",
		Level:   levelOK,
		Message: baseURL,
	})

	switch health.Status {
	case "healthy":
		checks = append(checks, doctorCheck{
			Name:    "Service status",
			Level:   levelOK,
			Message: "healthy, uptime " + health.Uptime,
		})
	case "":
		checks = append(checks, doctorCheck{
			Name:    "Service status",
			Level:   levelWarn,
			Message: "unknown (no status field in /health response)",
		})
	default:
		level := levelWarn
		hint := "check server logs (journalctl -u depsilo or docker logs)"
		if health.Status == "failed" || health.Status == "unhealthy" {
			level = levelFail
			hasFail = true
		}
		checks = append(checks, doctorCheck{
			Name:    "Service status",
			Level:   level,
			Message: health.Status,
			Hint:    hint,
		})
	}

	// 2. Version skew (CLI vs server)
	cliVer := version.Version
	switch {
	case cliVer == health.Version:
		checks = append(checks, doctorCheck{
			Name:    "Version match",
			Level:   levelOK,
			Message: "CLI and server both at " + cliVer,
		})
	case cliVer == "dev" || health.Version == "":
		checks = append(checks, doctorCheck{
			Name:    "Version match",
			Level:   levelInfo,
			Message: fmt.Sprintf("CLI %s, server %s (dev build)", cliVer, health.Version),
		})
	default:
		checks = append(checks, doctorCheck{
			Name:    "Version match",
			Level:   levelWarn,
			Message: fmt.Sprintf("CLI %s vs server %s", cliVer, health.Version),
			Hint:    "rebuild or reinstall so both sides match",
		})
	}

	// 3. Storage backend (uses admin API; tolerates auth failure)
	var dist cacheDistResponse
	distStatus, distErr := getJSON(baseURL+"/api/v1/admin/cache/distribution", &dist)
	switch {
	case distErr != nil || distStatus == 401 || distStatus == 403:
		checks = append(checks, doctorCheck{
			Name:    "Storage backend",
			Level:   levelInfo,
			Message: "admin API requires auth (set DEPSILO_TOKEN to inspect)",
		})
	case distStatus != 200:
		checks = append(checks, doctorCheck{
			Name:    "Storage backend",
			Level:   levelWarn,
			Message: fmt.Sprintf("distribution endpoint returned HTTP %d", distStatus),
		})
	default:
		usage := dist.UsagePercent
		size := formatBytes(dist.TotalSize) + " / " + formatBytes(dist.MaxSize)
		switch {
		case usage >= 95:
			checks = append(checks, doctorCheck{
				Name:    "Storage backend",
				Level:   levelFail,
				Message: fmt.Sprintf("%s used (%.1f%%, critical)", size, usage),
				Hint:    "depsilo flush  — or raise cache.max_size_gb in config.toml",
			})
			hasFail = true
		case usage >= 80:
			checks = append(checks, doctorCheck{
				Name:    "Storage backend",
				Level:   levelWarn,
				Message: fmt.Sprintf("%s used (%.1f%%, near limit)", size, usage),
				Hint:    "consider running: depsilo flush",
			})
		default:
			checks = append(checks, doctorCheck{
				Name:    "Storage backend",
				Level:   levelOK,
				Message: fmt.Sprintf("%s used (%.1f%%)", size, usage),
			})
		}
	}

	// 4. Upstreams + 5. Hit rate (public /api/v1/stats, no auth needed)
	var stats statsResponse
	statsStatus, statsErr := getJSON(baseURL+"/api/v1/stats", &stats)
	if statsErr != nil || statsStatus != 200 {
		checks = append(checks, doctorCheck{
			Name:    "Upstream health",
			Level:   levelInfo,
			Message: "public stats endpoint unreachable",
		})
		return finalizeDoctor(checks, jsonMode, baseURL, hasFail)
	}

	if len(stats.Upstreams) == 0 {
		checks = append(checks, doctorCheck{
			Name:    "Upstream health",
			Level:   levelWarn,
			Message: "no upstreams configured",
			Hint:    "add upstreams in config.toml or via /admin/upstreams",
		})
	} else {
		healthy := 0
		degraded := 0
		failed := []string{}
		highLatency := []string{}
		for _, u := range stats.Upstreams {
			switch {
			case !u.Healthy:
				failed = append(failed, u.Adapter+"/"+u.Name)
			case u.AvgLatencyMs > 150:
				degraded++
				highLatency = append(highLatency, fmt.Sprintf("%s/%s (%dms)", u.Adapter, u.Name, u.AvgLatencyMs))
			default:
				healthy++
			}
		}
		total := len(stats.Upstreams)
		switch {
		case len(failed) > 0:
			checks = append(checks, doctorCheck{
				Name:    "Upstream health",
				Level:   levelFail,
				Message: fmt.Sprintf("%d/%d failed: %s", len(failed), total, strings.Join(failed, ", ")),
				Hint:    "open /admin/upstreams or verify network reachability",
			})
			hasFail = true
		case degraded > 0:
			checks = append(checks, doctorCheck{
				Name:    "Upstream health",
				Level:   levelWarn,
				Message: fmt.Sprintf("%d healthy, %d slow: %s", healthy, degraded, strings.Join(highLatency, ", ")),
			})
		default:
			checks = append(checks, doctorCheck{
				Name:    "Upstream health",
				Level:   levelOK,
				Message: fmt.Sprintf("all %d upstreams healthy", total),
			})
		}
	}

	// Cache hit rate (only meaningful after >= 1h uptime)
	uptimeHours := stats.Service.UptimeSeconds / 3600
	switch {
	case uptimeHours < 1:
		checks = append(checks, doctorCheck{
			Name:    "Cache hit rate",
			Level:   levelInfo,
			Message: "service uptime < 1h, not enough data yet",
		})
	case stats.Today.TotalRequests == 0:
		checks = append(checks, doctorCheck{
			Name:    "Cache hit rate",
			Level:   levelInfo,
			Message: "no traffic today",
			Hint:    "configure a client: depsilo activate",
		})
	default:
		hr := stats.Today.HitRate * 100
		switch {
		case hr < 30:
			checks = append(checks, doctorCheck{
				Name:    "Cache hit rate",
				Level:   levelWarn,
				Message: fmt.Sprintf("%.1f%% (below 30%%)", hr),
				Hint:    "pre-fetch popular packages: depsilo warmup <eco> <pkg>...",
			})
		case hr < 50:
			checks = append(checks, doctorCheck{
				Name:    "Cache hit rate",
				Level:   levelInfo,
				Message: fmt.Sprintf("%.1f%% (modest)", hr),
			})
		default:
			checks = append(checks, doctorCheck{
				Name:    "Cache hit rate",
				Level:   levelOK,
				Message: fmt.Sprintf("%.1f%% today", hr),
			})
		}
	}

	return finalizeDoctor(checks, jsonMode, baseURL, hasFail)
}

func finalizeDoctor(checks []doctorCheck, jsonMode bool, baseURL string, hasFail bool) int {
	if jsonMode {
		printJSON(map[string]any{
			"ok":      !hasFail,
			"url":     baseURL,
			"checks":  checks,
			"summary": summarize(checks),
		})
		if hasFail {
			return 1
		}
		return 0
	}

	color := newColorWriter()
	fmt.Println("Depsilo Doctor")
	fmt.Println()

	for _, c := range checks {
		var tag string
		switch c.Level {
		case levelOK:
			tag = color.green("[ OK ]")
		case levelWarn:
			tag = color.yellow("[WARN]")
		case levelFail:
			tag = color.red("[FAIL]")
		case levelInfo:
			tag = color.dim("[INFO]")
		}
		fmt.Printf("%s  %-22s  %s\n", tag, c.Name, c.Message)
	}

	fmt.Println()
	sum := summarize(checks)
	fmt.Printf("Summary: %d ok · %d warning · %d failed · %d info\n",
		sum["ok"], sum["warn"], sum["fail"], sum["info"])

	hints := []string{}
	for _, c := range checks {
		if c.Hint != "" {
			hints = append(hints, fmt.Sprintf("  · %s: %s", c.Name, c.Hint))
		}
	}
	if len(hints) > 0 {
		fmt.Println()
		fmt.Println("Suggestions:")
		for _, h := range hints {
			fmt.Println(h)
		}
	}

	if hasFail {
		return 1
	}
	return 0
}

func summarize(checks []doctorCheck) map[string]int {
	s := map[string]int{"ok": 0, "warn": 0, "fail": 0, "info": 0}
	for _, c := range checks {
		s[string(c.Level)]++
	}
	return s
}

// ── ANSI color writer ────────────────────────────────────────────────
//
// Honors NO_COLOR (https://no-color.org/) and only emits escapes when
// stdout is an interactive terminal; piping into a file or another
// process strips colors automatically.

type colorWriter struct {
	enabled bool
}

func newColorWriter() *colorWriter {
	if os.Getenv("NO_COLOR") != "" {
		return &colorWriter{enabled: false}
	}
	if fi, err := os.Stdout.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
		return &colorWriter{enabled: true}
	}
	return &colorWriter{enabled: false}
}

func (c *colorWriter) green(s string) string  { return c.wrap(s, "32") }
func (c *colorWriter) yellow(s string) string { return c.wrap(s, "33") }
func (c *colorWriter) red(s string) string    { return c.wrap(s, "31") }
func (c *colorWriter) dim(s string) string    { return c.wrap(s, "2") }

func (c *colorWriter) wrap(s, code string) string {
	if !c.enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}
