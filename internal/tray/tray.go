// Package tray renders the Depsilo macOS / Linux / Windows menu-bar (tray)
// application. The tray polls the locally running Depsilo HTTP server and
// exposes the minimal "control surface" a self-hosted user expects:
//
//	● Healthy · 87.3% hit · v0.6.0      (live status, refreshes every 5s)
//	──────────────────────────────────
//	Open Admin Panel
//	Open Portal
//	Run Doctor
//	──────────────────────────────────
//	Quit Depsilo
//
// The tray talks to the server purely via HTTP, so a remote-server mode
// (DEPSILO_URL pointing at a non-local instance) works too — handy when
// you have Depsilo running in a homelab container and want a tray on
// your laptop.
package tray

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/systray"
	"go.uber.org/zap"
)

// ShutdownFunc is invoked when the user picks "Quit Depsilo". It should
// stop the in-process server (if any) and return promptly; the tray will
// call systray.Quit() afterwards.
type ShutdownFunc func()

// Options configures the tray.
type Options struct {
	BaseURL  string        // e.g. http://localhost:23333
	PollEvery time.Duration // status refresh interval (default 5s)
	Shutdown ShutdownFunc  // called when user picks Quit (may be nil for remote-only trays)
}

// Setup wires the systray menu items and starts the background status
// poller. It must be called from the systray onReady callback.
func Setup(opts Options) {
	if opts.PollEvery == 0 {
		opts.PollEvery = 5 * time.Second
	}

	systray.SetTitle("Depsilo")
	systray.SetTooltip("Depsilo · dependency cache proxy")

	mStatus := systray.AddMenuItem("Loading…", "Live service status")
	mStatus.Disable()

	systray.AddSeparator()
	mAdmin := systray.AddMenuItem("Open Admin Panel", "Open the admin UI in your browser")
	mPortal := systray.AddMenuItem("Open Portal", "Open the user portal in your browser")
	mDoctor := systray.AddMenuItem("Run Doctor", "Run end-to-end health diagnosis")

	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit Depsilo", "Stop the service and quit")

	// Status poller in its own goroutine so the menu stays responsive.
	go pollStatus(opts.BaseURL, opts.PollEvery, mStatus)

	// Click router. One goroutine drains all menu channels via select; that
	// keeps memory low and prevents goroutine sprawl as items are added.
	go func() {
		for {
			select {
			case <-mAdmin.ClickedCh:
				openURL(opts.BaseURL + "/admin")
			case <-mPortal.ClickedCh:
				openURL(opts.BaseURL + "/")
			case <-mDoctor.ClickedCh:
				go runDoctor(opts.BaseURL) // off the click goroutine; doctor can take a few hundred ms
			case <-mQuit.ClickedCh:
				zap.L().Info("tray: quit requested by user")
				if opts.Shutdown != nil {
					opts.Shutdown()
				}
				systray.Quit()
				return
			}
		}
	}()
}

// pollStatus polls /health + /api/v1/stats and rewrites the status menu
// item title every interval.
func pollStatus(baseURL string, every time.Duration, mStatus *systray.MenuItem) {
	client := &http.Client{Timeout: 3 * time.Second}

	render := func() {
		title := fetchStatusTitle(client, baseURL)
		mStatus.SetTitle(title)
	}

	render()
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for range ticker.C {
		render()
	}
}

func fetchStatusTitle(client *http.Client, baseURL string) string {
	// /health is cheap and reveals service + version even before the stats
	// pipeline has data.
	var health struct {
		Status  string `json:"status"`
		Version string `json:"version"`
	}
	if err := getJSON(client, baseURL+"/health", &health); err != nil {
		return "● Unreachable"
	}

	// Public stats is fine too; auth is not required.
	var stats struct {
		Today struct {
			HitRate float64 `json:"hit_rate"`
		} `json:"today"`
		Upstreams []struct {
			Healthy bool `json:"healthy"`
		} `json:"upstreams"`
	}
	hitTxt := ""
	statsOK := false
	anyUpstreamDown := false
	if err := getJSON(client, baseURL+"/api/v1/stats", &stats); err == nil {
		statsOK = true
		hitTxt = fmt.Sprintf(" · %.1f%% hit", stats.Today.HitRate*100)
		for _, u := range stats.Upstreams {
			if !u.Healthy {
				anyUpstreamDown = true
				break
			}
		}
	}

	icon := statusIcon(health.Status, statsOK, anyUpstreamDown)
	verTxt := ""
	if health.Version != "" {
		verTxt = " · " + health.Version
	}
	statusTxt := health.Status
	if statusTxt == "" {
		statusTxt = "unknown"
	}
	return fmt.Sprintf("%s %s%s%s", icon, statusTxt, hitTxt, verTxt)
}

// statusIcon returns a single unicode glyph reflecting overall health.
// macOS renders this in template-icon style (auto light/dark inverted).
func statusIcon(health string, statsOK, anyUpstreamDown bool) string {
	if health != "healthy" {
		return "○"
	}
	if statsOK && anyUpstreamDown {
		return "◐" // partial — service up but some upstream down
	}
	return "●"
}

// runDoctor surfaces a one-line summary as a macOS notification (osascript
// is available on every macOS install; on Linux/Windows we fall back to
// stdout). It reuses the public /api/v1/stats endpoint so the tray needs
// no admin token.
func runDoctor(baseURL string) {
	client := &http.Client{Timeout: 5 * time.Second}

	var health struct {
		Status  string `json:"status"`
		Uptime  string `json:"uptime"`
		Version string `json:"version"`
	}
	if err := getJSON(client, baseURL+"/health", &health); err != nil {
		notify("Depsilo", "Server unreachable. Is it running?")
		return
	}

	var stats struct {
		Today struct {
			TotalRequests int64   `json:"total_requests"`
			HitRate       float64 `json:"hit_rate"`
		} `json:"today"`
		Upstreams []struct {
			Name    string `json:"name"`
			Adapter string `json:"adapter"`
			Healthy bool   `json:"healthy"`
		} `json:"upstreams"`
	}
	_ = getJSON(client, baseURL+"/api/v1/stats", &stats)

	healthy, failed := 0, 0
	for _, u := range stats.Upstreams {
		if u.Healthy {
			healthy++
		} else {
			failed++
		}
	}

	subtitle := fmt.Sprintf("%s, uptime %s", health.Status, health.Uptime)
	msg := fmt.Sprintf("%d/%d upstreams healthy", healthy, healthy+failed)
	if stats.Today.TotalRequests > 0 {
		msg += fmt.Sprintf(" · %.1f%% hit today", stats.Today.HitRate*100)
	}
	notify(subtitle, msg)
}

// notify shows a desktop notification. macOS uses osascript; other
// platforms log to zap (a future revision can use notify-send / toast).
func notify(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification "%s" with title "Depsilo" subtitle "%s"`,
			escapeAppleScript(body), escapeAppleScript(title))
		_ = exec.Command("osascript", "-e", script).Run()
	default:
		zap.L().Info("tray notification", zap.String("title", title), zap.String("body", body))
	}
}

func escapeAppleScript(s string) string {
	// AppleScript needs " and \ escaped. Strings are short so a stdlib
	// Replacer is overkill.
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '"' || r == '\\' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out)
}

// openURL launches the user's default browser at url.
func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		zap.L().Warn("tray: openURL failed", zap.String("url", url), zap.Error(err))
	}
	// Let the launched process detach.
	go func() { _ = cmd.Wait() }()
}

// ── HTTP helpers (tiny inline client, no shared dep on internal/cli) ───

func getJSON(client *http.Client, url string, target any) error {
	ctx, cancel := context.WithTimeout(context.Background(), client.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if target == nil {
		return nil
	}
	return json.Unmarshal(body, target)
}
