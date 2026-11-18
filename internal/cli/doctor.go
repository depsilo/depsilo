package cli

import (
	"fmt"
	"os"
	"sort"
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

type readinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// slowUpstreamLatencyMs is the average latency at which doctor calls a healthy
// upstream slow. Keep it equal to publicUpstreamDegradedLatency in
// internal/api/public/stats.go and DEGRADED_LATENCY_MS in
// web/src/lib/upstreamStatus.ts, so doctor, status and the Portal agree.
const slowUpstreamLatencyMs = 1000

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
		return finalizeDoctor(checks, jsonMode, baseURL, true, false)
	}
	checks = append(checks, doctorCheck{
		Name:    "Service reachable",
		Level:   levelOK,
		Message: baseURL,
	})

	// A healthy process that still waits for first-run setup has no
	// administrator, and a blank one serves no packages. Report it as a failure
	// so a caller does not read "healthy" as "usable". A reopened setup keeps
	// serving the configured ecosystems, so the message does not claim
	// otherwise. It names who must act and where, and deliberately carries no
	// way to obtain the bootstrap token.
	setupRequired := setupPending(baseURL)
	if setupRequired {
		checks = append(checks, doctorCheck{
			Name:    "Initial setup",
			Level:   levelFail,
			Message: "not finished; no administrator can sign in yet",
			Hint:    "an operator must open the Portal at " + baseURL + " and complete initial setup",
		})
		hasFail = true
	}

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

	var readiness readinessResponse
	readyStatus, readyErr := getJSONAnyStatus(baseURL+"/ready", &readiness)
	if readyErr == nil && readyStatus == 200 && readiness.Status == "ready" {
		checks = append(checks, doctorCheck{
			Name:    "Service readiness",
			Level:   levelOK,
			Message: formatReadiness(readiness),
		})
	} else {
		message := formatReadiness(readiness)
		if readyErr != nil {
			message = readyErr.Error()
		} else if message == "" {
			message = fmt.Sprintf("HTTP %d", readyStatus)
		}
		checks = append(checks, doctorCheck{
			Name:    "Service readiness",
			Level:   levelFail,
			Message: message,
			Hint:    "check database and storage availability in the server logs",
		})
		hasFail = true
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
		return finalizeDoctor(checks, jsonMode, baseURL, hasFail, setupRequired)
	}

	if len(stats.Upstreams) == 0 && setupRequired {
		// Upstreams cannot be managed before setup finishes, so advising the
		// caller to add them would point away from the real next step.
		checks = append(checks, doctorCheck{
			Name:    "Upstream health",
			Level:   levelInfo,
			Message: "no upstreams configured (initial setup is not finished)",
		})
	} else if len(stats.Upstreams) == 0 {
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
			case u.AvgLatencyMs >= slowUpstreamLatencyMs:
				degraded++
				highLatency = append(highLatency, fmt.Sprintf("%s/%s (%dms)", u.Adapter, u.Name, u.AvgLatencyMs))
			default:
				healthy++
			}
		}
		total := len(stats.Upstreams)
		switch {
		case len(failed) > 0:
			message := fmt.Sprintf("%d/%d failed: %s", len(failed), total, strings.Join(failed, ", "))
			codes, asked := upstreamFailureCodes(baseURL)
			if len(codes) > 0 {
				message += "; failure codes: " + formatFailureCodes(codes)
			}
			checks = append(checks, doctorCheck{
				Name:    "Upstream health",
				Level:   levelFail,
				Message: message,
				Hint:    upstreamFailureHint(codes, asked),
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
		hint := "configure a client: depsilo activate"
		if setupRequired {
			hint = "" // no client can be served before setup finishes
		}
		checks = append(checks, doctorCheck{
			Name:    "Cache hit rate",
			Level:   levelInfo,
			Message: "no traffic today",
			Hint:    hint,
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

	return finalizeDoctor(checks, jsonMode, baseURL, hasFail, setupRequired)
}

// setupPending reports whether the instance still waits for first-run setup.
// GET /api/v1/setup/status needs no credentials. A server without the
// endpoint, or one that does not answer it, counts as not pending so the
// diagnosis of an older or degraded server keeps working.
func setupPending(baseURL string) bool {
	var status struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	code, err := getJSON(baseURL+"/api/v1/setup/status", &status)
	return err == nil && code == 200 && status.NeedsSetup
}

const (
	upstreamGenericHint = "open /admin/upstreams or verify network reachability"
	egressGuideAnchor   = "docs/deployment.md#upstream-network-egress"
	// failureFakeIP mirrors upstream.FailureFakeIP, the stable Admin API
	// value for a DNS answer from a local proxy in fake-IP mode that Depsilo
	// could not use: nothing carried the connection, [egress] allow_fake_ip
	// is off, or the outbound guard always refuses the answer, such as one
	// that mixes fake-IP and other addresses.
	failureFakeIP = "egress_fake_ip"
)

// upstreamFailureCodes counts why unhealthy upstreams failed their last
// check, as GET /api/v1/admin/upstreams reports it. That endpoint needs a
// token: asked is false when none is configured or the request fails. An
// accepted request can still yield no codes, because an upstream may be
// unhealthy without a failed check on record (an open circuit breaker, or
// state restored after a restart).
// Only last_failure.code is decoded: the address and range the Admin API
// also returns never reach doctor's output.
func upstreamFailureCodes(baseURL string) (codes map[string]int, asked bool) {
	if getToken() == "" {
		return nil, false
	}
	var list struct {
		Items []struct {
			Healthy     bool `json:"healthy"`
			LastFailure *struct {
				Code string `json:"code"`
			} `json:"last_failure"`
		} `json:"items"`
	}
	status, err := getJSON(baseURL+"/api/v1/admin/upstreams", &list)
	if err != nil || status != 200 {
		return nil, false
	}
	codes = map[string]int{}
	for _, item := range list.Items {
		if item.Healthy || item.LastFailure == nil {
			continue
		}
		codes[failureCodeLabel(item.LastFailure.Code)]++
	}
	return codes, true
}

// failureCodeLabel keeps a code only when it has the shape of the documented
// ones (lower-case words joined by underscores), so nothing else a server
// puts in that field is echoed to the terminal.
func failureCodeLabel(code string) string {
	if code == "" || len(code) > 40 {
		return "other"
	}
	for _, r := range code {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return "other"
		}
	}
	return code
}

// formatFailureCodes lists codes with their counts, most frequent first.
func formatFailureCodes(codes map[string]int) string {
	names := make([]string, 0, len(codes))
	for code := range codes {
		names = append(names, code)
	}
	sort.Slice(names, func(i, j int) bool {
		if codes[names[i]] != codes[names[j]] {
			return codes[names[i]] > codes[names[j]]
		}
		return names[i] < names[j]
	})
	parts := make([]string, 0, len(names))
	for _, code := range names {
		parts = append(parts, fmt.Sprintf("%s x%d", code, codes[code]))
	}
	return strings.Join(parts, ", ")
}

// upstreamFailureHint turns the failure codes into the next step. Without
// codes it can only name the most common cause on a workstation or Homelab.
// asked says whether the Admin API answered: only when it did not is a token
// the thing to fix.
func upstreamFailureHint(codes map[string]int, asked bool) string {
	if !asked {
		return upstreamGenericHint + ". If proxy software in fake-IP mode answers this host's DNS while its TUN mode is off, " +
			"every upstream fails until the TUN or a proxy carries Depsilo's connections: see " +
			egressGuideAnchor + " (doctor names the failure codes when DEPSILO_TOKEN or ~/.config/depsilo/token holds a valid Admin token)"
	}
	if len(codes) == 0 {
		return upstreamGenericHint + " (the server has no failure reason on record for these upstreams yet)"
	}
	if codes[failureFakeIP] == 0 {
		return upstreamGenericHint
	}
	hint := failureFakeIP + " means DNS answered with fake-IP addresses, as proxy software in fake-IP mode does, " +
		"and Depsilo could not connect to them: nothing carried its connections, [egress] allow_fake_ip is off, " +
		"or the answer mixed fake-IP and other addresses or held a unique-local IPv6 fake-IP address, which Depsilo always refuses. " +
		"Turn on the proxy software's TUN mode, or set upstream_proxy under [egress] in config.toml, " +
		"or DEPSILO_EGRESS_UPSTREAM_PROXY in the service's environment, or check allow_fake_ip; " +
		"for unique-local IPv6 fake-IP addresses, set the proxy software's IPv6 fake-IP range to 2001:2::/48; see " + egressGuideAnchor
	if len(codes) > 1 {
		hint += ". For the other codes, " + upstreamGenericHint
	}
	return hint
}

func formatReadiness(readiness readinessResponse) string {
	parts := make([]string, 0, len(readiness.Checks))
	for name, status := range readiness.Checks {
		parts = append(parts, name+"="+status)
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return readiness.Status
	}
	if readiness.Status == "" {
		return strings.Join(parts, ", ")
	}
	return readiness.Status + " (" + strings.Join(parts, ", ") + ")"
}

func finalizeDoctor(checks []doctorCheck, jsonMode bool, baseURL string, hasFail bool, setupRequired bool) int {
	if jsonMode {
		printJSON(map[string]any{
			"ok":             !hasFail,
			"url":            baseURL,
			"setup_required": setupRequired,
			"checks":         checks,
			"summary":        summarize(checks),
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
