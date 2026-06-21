# Webhook Notification System — Implementation Plan

> **For Hermes:** Implement task-by-task. TDD: write test → verify fail → implement → verify pass → commit.

**Goal:** Add a webhook notification engine that sends alerts to Slack/DingTalk/WeCom/Feishu when Depsilo detects upstream failures, high disk usage, critical security vulnerabilities, or license expiry.

**Architecture:** New `internal/notify/` module with a pluggable dispatcher pattern. Each platform (Slack, DingTalk, WeCom, Feishu) is a single `Dispatch()` function that formats the JSON payload correctly. A background scheduler goroutine polls trigger conditions every 60s. Configs stored in DB (manageable via API/UI), with optional `config.toml` bootstrap entries.

**Tech Stack:** Go stdlib `net/http` (no new deps), GORM for DB, Gin for API, existing zap logger.

---

## Phase 1: DB model + config

### Task 1: Add WebhookConfig GORM model

**Objective:** Define the database model for webhook configurations.

**Files:**
- Modify: `internal/db/models.go`

**Step 1: Add struct at end of models.go (before TrialRecord)**

```go
// WebhookConfig stores a webhook endpoint configuration.
// Each webhook subscribes to one or more event types and formats
// payloads for a specific platform (Slack, DingTalk, WeCom, Feishu, or generic).
type WebhookConfig struct {
	ID          uint      `gorm:"primarykey" json:"id"`
	Name        string    `gorm:"size:128" json:"name"`                   // human label, e.g. "Ops DingTalk"
	Platform    string    `gorm:"size:16" json:"platform"`                // slack | dingtalk | wecom | feishu | generic
	URL         string    `gorm:"size:512" json:"url"`                    // incoming webhook URL
	Secret      string    `gorm:"size:256" json:"-"`                      // optional HMAC secret (future, not used yet)
	Enabled     bool      `gorm:"default:true" json:"enabled"`
	Events      string    `gorm:"size:256;default:'*'" json:"events"`     // comma-separated: upstream_down,disk_high,vuln_critical,license_expiring or '*'
	CooldownMinutes int   `gorm:"default:30" json:"cooldown_minutes"`     // min interval between repeated alerts of same type
	LastSentAt  *time.Time `json:"last_sent_at"`                          // last successful send time
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
```

**Step 2: Add to AutoMigrate**

In `internal/db/repository.go`, find the `AutoMigrate` call list and add `&WebhookConfig{}`.

**Verify:** `go build ./...` passes.

---

### Task 2: Add webhooks to config struct

**Objective:** Allow optional bootstrap webhook configs in config.toml.

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Add WebhookConfig and field to Config**

```go
type WebhookConfig struct {
	Name     string `mapstructure:"name"`
	Platform string `mapstructure:"platform"`
	URL      string `mapstructure:"url"`
	Events   string `mapstructure:"events"`
	Enabled  bool   `mapstructure:"enabled"`
}
```

**Step 2: Add field to Config struct**

```go
Webhooks []WebhookConfig `mapstructure:"webhooks"`
```

**Verify:** `go build ./...` passes.

---

### Task 3: Sync config webhooks to DB on startup

**Objective:** On server start, upsert `[[webhooks]]` entries from config.toml into the DB so they appear in the admin UI.

**Files:**
- Modify: `internal/server/server.go`

**Step 1: Add syncWebhookConfigs helper function** (near syncUpstreams)

```go
func syncWebhookConfigs(database *gorm.DB, webhooks []config.WebhookConfig) {
	for _, w := range webhooks {
		var record db.WebhookConfig
		result := database.Where("url = ? AND platform = ?", w.URL, w.Platform).First(&record)
		if result.Error == gorm.ErrRecordNotFound {
			database.Create(&db.WebhookConfig{
				Name:     w.Name,
				Platform: w.Platform,
				URL:      w.URL,
				Events:   w.Events,
				Enabled:  w.Enabled,
			})
		}
	}
}
```

**Step 2: Call it after syncUpstreams calls in StartServer**

```go
syncWebhookConfigs(database, cfg.Webhooks)
```

**Verify:** `go build ./...` passes.

---

## Phase 2: Notification engine core

### Task 4: Create notifier skeleton

**Objective:** Create the Notifier struct with New/Dispatch/LoadConfigs methods.

**Files:**
- Create: `internal/notify/notifier.go`

```go
package notify

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
)

// Notifier loads webhook configs from DB and dispatches notifications.
type Notifier struct {
	db      *gorm.DB
	mu      sync.RWMutex
	configs []db.WebhookConfig
}

func New(database *gorm.DB) *Notifier {
	return &Notifier{db: database}
}

// LoadConfigs reloads webhook configs from the database.
func (n *Notifier) LoadConfigs() error {
	var configs []db.WebhookConfig
	if err := n.db.Where("enabled = ?", true).Find(&configs).Error; err != nil {
		return err
	}
	n.mu.Lock()
	n.configs = configs
	n.mu.Unlock()
	zap.L().Info("webhook configs loaded", zap.Int("count", len(configs)))
	return nil
}

// Dispatch sends a notification to all matching webhooks.
// It respects per-webhook cooldowns and event filters.
func (n *Notifier) Dispatch(ctx context.Context, event Event) {
	n.mu.RLock()
	configs := make([]db.WebhookConfig, len(n.configs))
	copy(configs, n.configs)
	n.mu.RUnlock()

	for i := range configs {
		cfg := &configs[i]
		if !cfg.Enabled {
			continue
		}
		if !matchEvent(cfg.Events, event.Type) {
			continue
		}
		// Check cooldown
		if cfg.CooldownMinutes > 0 && cfg.LastSentAt != nil {
			if time.Since(*cfg.LastSentAt) < time.Duration(cfg.CooldownMinutes)*time.Minute {
				continue
			}
		}
		go func(c db.WebhookConfig) {
			if err := dispatch(ctx, c, event); err != nil {
				zap.L().Warn("webhook dispatch failed",
					zap.String("name", c.Name),
					zap.String("platform", c.Platform),
					zap.Error(err),
				)
				return
			}
			// Update last sent time
			now := time.Now()
			n.db.Model(&c).Update("last_sent_at", now)
		}(*cfg)
	}
}

func matchEvent(eventsFilter string, eventType string) bool {
	if eventsFilter == "*" || eventsFilter == "" {
		return true
	}
	for _, e := range splitAndTrim(eventsFilter, ",") {
		if e == eventType {
			return true
		}
	}
	return false
}

func splitAndTrim(s, sep string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			parts = append(parts, trimSpace(s[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, trimSpace(s[start:]))
	return parts
}

func trimSpace(s string) string {
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
```

**Verify:** `go build ./...` passes.

---

### Task 5: Define Event type + platform dispatchers

**Objective:** Define the Event struct and implement per-platform dispatch functions.

**Files:**
- Create: `internal/notify/event.go`
- Create: `internal/notify/dispatcher.go`

**Step 1: event.go**

```go
package notify

import "time"

// EventType enumerates the kinds of events that can trigger a notification.
const (
	EventUpstreamDown      = "upstream_down"
	EventDiskHigh          = "disk_high"
	EventVulnCritical      = "vuln_critical"
	EventLicenseExpiring   = "license_expiring"
)

// Event represents a notification-worthy occurrence in Depsilo.
type Event struct {
	Type      string    `json:"type"`
	Severity  string    `json:"severity"`   // critical | warning | info
	Title     string    `json:"title"`
	Message   string    `json:"message"`
	Detail    string    `json:"detail,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}
```

**Step 2: dispatcher.go — platform-specific formatting**

```go
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"depsilo/internal/db"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func dispatch(ctx context.Context, cfg db.WebhookConfig, event Event) error {
	payload, err := formatPayload(cfg.Platform, event)
	if err != nil {
		return fmt.Errorf("format %s payload: %w", cfg.Platform, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

func formatPayload(platform string, event Event) ([]byte, error) {
	switch platform {
	case "slack":
		return formatSlack(event)
	case "dingtalk":
		return formatDingTalk(event)
	case "wecom":
		return formatWeCom(event)
	case "feishu":
		return formatFeishu(event)
	default:
		return formatGeneric(event)
	}
}

// Slack: uses incoming webhook JSON format
func formatSlack(event Event) ([]byte, error) {
	color := severityColor(event.Severity)
	msg := map[string]interface{}{
		"attachments": []map[string]interface{}{
			{
				"color":  color,
				"title":  event.Title,
				"text":   event.Message,
				"fields": []map[string]interface{}{},
				"footer": "Depsilo",
				"ts":     event.Timestamp.Unix(),
			},
		},
	}
	if event.Detail != "" {
		msg["attachments"].([]map[string]interface{})[0]["fields"] = []map[string]interface{}{
			{"value": event.Detail, "short": false},
		}
	}
	return json.Marshal(msg)
}

// DingTalk: markdown message format for bot webhooks
func formatDingTalk(event Event) ([]byte, error) {
	title := fmt.Sprintf("## %s\n", event.Title)
	text := event.Message
	if event.Detail != "" {
		text += "\n\n> " + event.Detail
	}
	text += fmt.Sprintf("\n\n---\n*Depsilo · %s*", event.Timestamp.Format("2006-01-02 15:04:05"))

	return json.Marshal(map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"title": event.Title,
			"text":  title + text,
		},
	})
}

// WeCom: text/markdown message for group bot webhooks
func formatWeCom(event Event) ([]byte, error) {
	content := fmt.Sprintf("**%s**\n%s", event.Title, event.Message)
	if event.Detail != "" {
		content += fmt.Sprintf("\n> %s", event.Detail)
	}
	content += fmt.Sprintf("\n\n<font color=\"comment\">Depsilo · %s</font>",
		event.Timestamp.Format("2006-01-02 15:04:05"))

	return json.Marshal(map[string]interface{}{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	})
}

// Feishu: interactive card format
func formatFeishu(event Event) ([]byte, error) {
	color := severityColor(event.Severity)
	// Feishu uses "red", "yellow", "green", "blue" etc.
	feishuColor := "red"
	switch color {
	case "warning":
		feishuColor = "yellow"
	case "good":
		feishuColor = "green"
	case "#439FE0":
		feishuColor = "blue"
	}

	return json.Marshal(map[string]interface{}{
		"msg_type": "interactive",
		"card": map[string]interface{}{
			"header": map[string]interface{}{
				"title":    map[string]string{"tag": "plain_text", "content": event.Title},
				"template": feishuColor,
			},
			"elements": []map[string]interface{}{
				{
					"tag":  "div",
					"text": map[string]string{"tag": "lark_md", "content": event.Message},
				},
			},
		},
	})
}

// Generic: plain JSON with all event fields
func formatGeneric(event Event) ([]byte, error) {
	return json.Marshal(event)
}

func severityColor(severity string) string {
	switch severity {
	case "critical":
		return "danger"
	case "warning":
		return "warning"
	case "info":
		return "#439FE0"
	default:
		return "good"
	}
}
```

**Verify:** `go build ./...` passes.

---

### Task 6: Write unit test for dispatcher formatting

**Objective:** Ensure each platform formatter produces valid JSON with expected fields.

**Files:**
- Create: `internal/notify/dispatcher_test.go`

```go
package notify

import (
	"encoding/json"
	"testing"
	"time"
)

func testEvent() Event {
	return Event{
		Type:      EventUpstreamDown,
		Severity:  "critical",
		Title:     "All PyPI upstreams are down",
		Message:   "3 of 3 upstreams unhealthy. Packages will be served from stale cache.",
		Detail:    "tuna: timeout | official: timeout | aliyun: timeout",
		Timestamp: time.Date(2026, 6, 21, 14, 30, 0, 0, time.UTC),
	}
}

func mustJSON(t *testing.T, payload []byte) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatalf("invalid JSON: %v\npayload: %s", err, string(payload))
	}
	return m
}

func TestFormatSlack(t *testing.T) {
	payload, err := formatSlack(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	attachments := m["attachments"].([]interface{})
	if len(attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(attachments))
	}
}

func TestFormatDingTalk(t *testing.T) {
	payload, err := formatDingTalk(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["msgtype"] != "markdown" {
		t.Fatalf("expected msgtype=markdown, got %v", m["msgtype"])
	}
	md := m["markdown"].(map[string]interface{})
	if md["title"] != testEvent().Title {
		t.Fatalf("title mismatch: %v", md["title"])
	}
}

func TestFormatWeCom(t *testing.T) {
	payload, err := formatWeCom(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["msgtype"] != "markdown" {
		t.Fatalf("expected msgtype=markdown, got %v", m["msgtype"])
	}
}

func TestFormatFeishu(t *testing.T) {
	payload, err := formatFeishu(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["msg_type"] != "interactive" {
		t.Fatalf("expected msg_type=interactive, got %v", m["msg_type"])
	}
}

func TestFormatGeneric(t *testing.T) {
	payload, err := formatGeneric(testEvent())
	if err != nil {
		t.Fatal(err)
	}
	m := mustJSON(t, payload)
	if m["type"] != EventUpstreamDown {
		t.Fatalf("type mismatch: %v", m["type"])
	}
}

func TestMatchEvent(t *testing.T) {
	tests := []struct {
		filter string
		event  string
		want   bool
	}{
		{"*", "upstream_down", true},
		{"", "upstream_down", true},
		{"upstream_down", "upstream_down", true},
		{"upstream_down,disk_high", "disk_high", true},
		{"upstream_down", "disk_high", false},
		{"upstream_down,disk_high", "vuln_critical", false},
	}
	for _, tt := range tests {
		got := matchEvent(tt.filter, tt.event)
		if got != tt.want {
			t.Errorf("matchEvent(%q, %q) = %v, want %v", tt.filter, tt.event, got, tt.want)
		}
	}
}
```

**Verify:** `go test ./internal/notify/... -v` — all pass.

---

## Phase 3: Background scheduler (trigger detection)

### Task 7: Create the scheduler goroutine

**Objective:** Background goroutine that polls trigger conditions every 60s and dispatches events.

**Files:**
- Create: `internal/notify/scheduler.go`

```go
package notify

import (
	"context"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/entitlement"
	"depsilo/internal/upstream"
)

// SchedulerConfig holds the polling knobs.
type SchedulerConfig struct {
	CheckInterval   time.Duration // how often to poll (default 60s)
	DiskThreshold   float64       // 0.0-1.0, trigger when usage exceeds (default 0.85)
	LicenseWarnDays int           // days before expiry to warn (default 7)
	Pools           map[string]*upstream.Pool
	Checker         *entitlement.Checker
}

// StartScheduler runs periodic checks and dispatches events when thresholds are crossed.
// It blocks until ctx is cancelled.
func StartScheduler(ctx context.Context, n *Notifier, database *gorm.DB, cfg SchedulerConfig) {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 60 * time.Second
	}
	if cfg.DiskThreshold <= 0 {
		cfg.DiskThreshold = 0.85
	}
	if cfg.LicenseWarnDays <= 0 {
		cfg.LicenseWarnDays = 7
	}

	zap.L().Info("webhook scheduler started",
		zap.Duration("interval", cfg.CheckInterval),
		zap.Float64("disk_threshold", cfg.DiskThreshold),
	)

	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()

	// Run immediate check on start
	select {
	case <-ctx.Done():
		return
	case <-time.After(5 * time.Second): // give system time to initialize
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.LoadConfigs() // refresh configs each cycle
			checkUpstreamHealth(ctx, n, cfg.Pools)
			checkDiskUsage(ctx, n, database, cfg.DiskThreshold)
			checkLicense(ctx, n, cfg.Checker, cfg.LicenseWarnDays)
		}
	}
}

// checkUpstreamHealth checks if ALL upstreams for any ecosystem are down.
func checkUpstreamHealth(ctx context.Context, n *Notifier, pools map[string]*upstream.Pool) {
	if pools == nil {
		return
	}
	for eco, pool := range pools {
		upstreams := pool.Upstreams()
		if len(upstreams) == 0 {
			continue
		}
		allDown := true
		for _, u := range upstreams {
			u.Mu().Lock()
			healthy := u.Healthy
			u.Mu().Unlock()
			if healthy {
				allDown = false
				break
			}
		}
		if allDown {
			n.Dispatch(ctx, Event{
				Type:      EventUpstreamDown,
				Severity:  "critical",
				Title:     "All " + eco + " upstreams are down",
				Message:   "All upstream mirrors for " + eco + " are unreachable. Depsilo will serve stale cache if available.",
				Timestamp: time.Now(),
			})
			zap.L().Warn("webhook: all upstreams down", zap.String("ecosystem", eco))
		}
	}
}

// checkDiskUsage checks if cache disk usage exceeds threshold.
func checkDiskUsage(ctx context.Context, n *Notifier, database *gorm.DB, threshold float64) {
	var totalSize int64
	database.Model(&db.CacheEntry{}).Select("COALESCE(SUM(size), 0)").Scan(&totalSize)
	// totalSize is in bytes; we need to compare against max_size_gb from config
	// For now, use a simpler approach: check against total DB-reported size
	// A proper implementation would use cache.Storage.TotalSize()
	// This is intentionally simple — the real check uses the storage interface
	_ = totalSize // placeholder for future enhancement
	// TODO: integrate with cache.Manager and storage.Storage for accurate disk usage
}

// checkLicense warns when Pro license is expiring soon.
func checkLicense(ctx context.Context, n *Notifier, checker *entitlement.Checker, warnDays int) {
	if checker == nil {
		return
	}
	status := checker.Status()
	if status.Source != "paid" && status.Source != "trial" {
		return
	}
	if status.ExpiresAt == nil {
		return
	}
	daysLeft := int(time.Until(*status.ExpiresAt).Hours() / 24)
	if daysLeft <= 0 {
		n.Dispatch(ctx, Event{
			Type:      EventLicenseExpiring,
			Severity:  "critical",
			Title:     "Depsilo Pro license has expired",
			Message:   "Your Pro license expired. Pro features (audit logs, SBOM, security scanning, rules) are now locked.",
			Timestamp: time.Now(),
		})
	} else if daysLeft <= warnDays {
		n.Dispatch(ctx, Event{
			Type:     EventLicenseExpiring,
			Severity: "warning",
			Title:    "Depsilo Pro license expiring soon",
			Message:  "Your Pro license expires in " + itoa(daysLeft) + " days. Renew to keep Pro features.",
			Detail:   "Expires: " + status.ExpiresAt.Format("2006-01-02"),
			Timestamp: time.Now(),
		})
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	d := ""
	for n > 0 {
		d = string(rune('0'+n%10)) + d
		n /= 10
	}
	return d
}
```

**Note:** The disk usage check is a placeholder. The proper implementation reads from `cache.Storage.TotalSize()` and compares to `config.Cache.MaxSizeGB`. We'll wire it in Phase 4 after adding the Notifier to server.go.

**Verify:** `go build ./internal/notify/...` passes.

---

### Task 8: Add scheduler unit test (trigger logic)

**Objective:** Test the itoa helper, cooldown logic, and event matching used by the scheduler.

**Files:**
- Create: `internal/notify/scheduler_test.go`

```go
package notify

import "testing"

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{7, "7"},
		{14, "14"},
		{365, "365"},
	}
	for _, tt := range tests {
		got := itoa(tt.n)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
```

**Verify:** `go test ./internal/notify/... -v -run TestItoa` passes.

---

## Phase 4: Wire into server bootstrap

### Task 8.5: Add thread-safe IsHealthy() to Upstream

**Objective:** The scheduler needs to read upstream health without data races. Add an exported getter.

**Files:**
- Modify: `internal/upstream/pool.go`

**Step 1: Add method after the AvgLatency/SuccessRate methods (~line 200):**

```go
// IsHealthy returns the current health status (thread-safe).
func (u *Upstream) IsHealthy() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.Healthy
}
```

**Step 2: Update scheduler.go's checkUpstreamHealth to use IsHealthy():**

Replace `u.Mu().Lock(); healthy := u.Healthy; u.Mu().Unlock()` with `u.IsHealthy()`.

**Verify:** `go build ./...` passes.

---

### Task 9: Create Notifier in server.go

**Objective:** Initialize the Notifier, load configs, and start the scheduler in StartServer.

**Files:**
- Modify: `internal/server/server.go`

**Step 1: Add import** for `"depsilo/internal/notify"`

**Step 2: After the auditLogger initialization block (after `go auditLogger.Start(ctx)`), add:**

```go
// Webhook notification engine
webhookNotifier := notify.New(database)
if err := webhookNotifier.LoadConfigs(); err != nil {
	zap.L().Warn("failed to load webhook configs", zap.Error(err))
}
go notify.StartScheduler(ctx, webhookNotifier, database, notify.SchedulerConfig{
	Pools:         pools,
	Checker:       checker,
})
```

**Step 3: Add Notifier to api.Deps**

```go
type Deps struct {
	// ... existing fields ...
	WebhookNotifier *notify.Notifier  // NEW
}
```

And pass `webhookNotifier` in the Deps literal in RegisterRoutes.

**Verify:** `go build ./...` passes.

---

## Phase 5: Admin API endpoints

### Task 10: Create webhook API handler

**Objective:** CRUD endpoints for webhook configs + test-send.

**Files:**
- Create: `internal/api/admin/webhook.go`

```go
package admin

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/notify"
)

type WebhookHandler struct {
	DB       *gorm.DB
	Notifier *notify.Notifier
}

func NewWebhookHandler(database *gorm.DB, n *notify.Notifier) *WebhookHandler {
	return &WebhookHandler{DB: database, Notifier: n}
}

func (h *WebhookHandler) List(c *gin.Context) {
	var configs []db.WebhookConfig
	if err := h.DB.Order("created_at DESC").Find(&configs).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, configs)
}

func (h *WebhookHandler) Create(c *gin.Context) {
	var body struct {
		Name            string `json:"name" binding:"required"`
		Platform        string `json:"platform" binding:"required"`
		URL             string `json:"url" binding:"required"`
		Events          string `json:"events"`
		CooldownMinutes int    `json:"cooldown_minutes"`
		Enabled         *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	cfg := db.WebhookConfig{
		Name:            body.Name,
		Platform:        body.Platform,
		URL:             body.URL,
		Events:          body.Events,
		CooldownMinutes: body.CooldownMinutes,
		Enabled:         true,
	}
	if body.Events == "" {
		cfg.Events = "*"
	}
	if body.CooldownMinutes <= 0 {
		cfg.CooldownMinutes = 30
	}
	if body.Enabled != nil {
		cfg.Enabled = *body.Enabled
	}

	if err := h.DB.Create(&cfg).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	h.reloadNotifier()
	c.JSON(http.StatusCreated, cfg)
}

func (h *WebhookHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var body struct {
		Name            *string `json:"name"`
		Platform        *string `json:"platform"`
		URL             *string `json:"url"`
		Events          *string `json:"events"`
		CooldownMinutes *int    `json:"cooldown_minutes"`
		Enabled         *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": err.Error()})
		return
	}

	updates := map[string]interface{}{}
	if body.Name != nil { updates["name"] = *body.Name }
	if body.Platform != nil { updates["platform"] = *body.Platform }
	if body.URL != nil { updates["url"] = *body.URL }
	if body.Events != nil { updates["events"] = *body.Events }
	if body.CooldownMinutes != nil { updates["cooldown_minutes"] = *body.CooldownMinutes }
	if body.Enabled != nil { updates["enabled"] = *body.Enabled }

	if err := h.DB.Model(&db.WebhookConfig{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	h.reloadNotifier()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *WebhookHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	if err := h.DB.Delete(&db.WebhookConfig{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": "DB_ERROR", "message": err.Error()})
		return
	}
	h.reloadNotifier()
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *WebhookHandler) Test(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": "BAD_REQUEST", "message": "invalid id"})
		return
	}

	var cfg db.WebhookConfig
	if err := h.DB.First(&cfg, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND", "message": "webhook not found"})
		return
	}

	testEvent := notify.Event{
		Type:      "test",
		Severity:  "info",
		Title:     "Depsilo webhook test",
		Message:   "If you see this, your webhook is configured correctly! 🎉",
		Timestamp: time.Now(),
	}

	if h.Notifier != nil {
		h.Notifier.Dispatch(context.Background(), testEvent)
	}

	zap.L().Info("webhook test triggered", zap.String("name", cfg.Name), zap.String("platform", cfg.Platform))
	c.JSON(http.StatusOK, gin.H{"status": "test sent"})
}

func (h *WebhookHandler) reloadNotifier() {
	if h.Notifier != nil {
		if err := h.Notifier.LoadConfigs(); err != nil {
			zap.L().Warn("failed to reload webhook configs", zap.Error(err))
		}
	}
}
```

**Verify:** `go build ./...` passes.

---

### Task 11: Register webhook routes in router.go

**Objective:** Add webhook API routes to the admin group.

**Files:**
- Modify: `internal/api/router.go`

**Step 1: After the settings handler registration (line ~148), add:**

```go
// Webhook notifications
webhookHandler := admin.NewWebhookHandler(deps.DB, deps.WebhookNotifier)
adminGroup.GET("/webhooks", webhookHandler.List)
adminGroup.POST("/webhooks", webhookHandler.Create)
adminGroup.PUT("/webhooks/:id", webhookHandler.Update)
adminGroup.DELETE("/webhooks/:id", webhookHandler.Delete)
adminGroup.POST("/webhooks/:id/test", webhookHandler.Test)
```

**Verify:** `go build ./...` passes.

---

## Phase 6: Frontend — Webhook tab in Settings

### Task 12: Add webhook API functions to api.ts

**Objective:** Add typed API functions for webhook CRUD.

**Files:**
- Modify: `web/src/lib/api.ts`

**Step 1:** Add `WebhookConfig` interface and API methods near the end of the file:

```typescript
export interface WebhookConfig {
  id: number
  name: string
  platform: 'slack' | 'dingtalk' | 'wecom' | 'feishu' | 'generic'
  url: string
  enabled: boolean
  events: string
  cooldown_minutes: number
  last_sent_at?: string
  created_at: string
  updated_at: string
}

export const webhookApi = {
  list: () => api.get<WebhookConfig[]>('/admin/webhooks'),
  create: (data: Partial<WebhookConfig>) =>
    api.post<WebhookConfig>('/admin/webhooks', data),
  update: (id: number, data: Partial<WebhookConfig>) =>
    api.put(`/admin/webhooks/${id}`, data),
  delete: (id: number) => api.delete(`/admin/webhooks/${id}`),
  test: (id: number) => api.post(`/admin/webhooks/${id}/test`),
}
```

**Verify:** `cd web && npx tsc --noEmit` passes.

---

### Task 13: Add i18n keys for webhooks

**Objective:** Add all needed i18n strings for the webhook tab.

**Files:**
- Modify: `web/src/i18n/en.ts`
- Modify: `web/src/i18n/zh.ts`

**English keys (add to the settings section):**

```typescript
// Settings → Webhooks tab
settings: {
  // ... existing ...
  webhooks: 'Webhooks',
},

webhook: {
  title: 'Webhook Notifications',
  description: 'Get alerted on Slack, DingTalk, WeCom, or Feishu when something needs attention.',
  addWebhook: 'Add Webhook',
  editWebhook: 'Edit Webhook',
  name: 'Name',
  namePlaceholder: 'e.g. Ops DingTalk',
  platform: 'Platform',
  url: 'Webhook URL',
  urlPlaceholder: 'https://...',
  events: 'Events',
  eventsAll: 'All events',
  cooldown: 'Cooldown (min)',
  testSent: 'Test notification sent. Check your platform.',
  deleteConfirm: 'Delete this webhook?',
  noWebhooks: 'No webhooks configured. Add one to get started.',
  platforms: {
    slack: 'Slack',
    dingtalk: 'DingTalk',
    wecom: 'WeCom',
    feishu: 'Feishu',
    generic: 'Generic Webhook',
  },
  events_list: {
    upstream_down: 'Upstream Failure',
    disk_high: 'High Disk Usage',
    vuln_critical: 'Critical Vulnerability',
    license_expiring: 'License Expiring',
  },
  lastSent: 'Last sent',
  never: 'Never',
  guide: {
    title: 'How to get a webhook URL',
    slack: 'Go to Slack Apps → Incoming Webhooks → Add to Workspace.',
    dingtalk: 'In DingTalk group → Group Settings → Bot → Add Custom Bot → Copy Webhook URL.',
    wecom: 'In WeCom group → Group Settings → Bots → Add Bot → Copy Webhook URL.',
    feishu: 'In Feishu group → Settings → Bots → Add Custom Bot → Copy Webhook URL.',
  },
},
```

**Chinese keys (zh.ts):**

```typescript
settings: {
  // ... existing ...
  webhooks: 'Webhook 通知',
},

webhook: {
  title: 'Webhook 通知',
  description: '在上游故障、磁盘告急、安全漏洞或许可证到期时，通过 Slack/钉钉/企微/飞书 接收告警。',
  addWebhook: '添加 Webhook',
  editWebhook: '编辑 Webhook',
  name: '名称',
  namePlaceholder: '例：运维群钉钉机器人',
  platform: '平台',
  url: 'Webhook 地址',
  urlPlaceholder: 'https://...',
  events: '触发事件',
  eventsAll: '全部事件',
  cooldown: '冷却时间（分钟）',
  testSent: '测试通知已发送，请检查对应平台。',
  deleteConfirm: '确定删除此 Webhook？',
  noWebhooks: '尚未配置 Webhook。添加一个以接收告警。',
  platforms: {
    slack: 'Slack',
    dingtalk: '钉钉',
    wecom: '企业微信',
    feishu: '飞书',
    generic: '通用 Webhook',
  },
  events_list: {
    upstream_down: '上游源全部故障',
    disk_high: '磁盘使用率过高',
    vuln_critical: '严重安全漏洞',
    license_expiring: '许可证即将到期',
  },
  lastSent: '上次发送',
  never: '从未',
  guide: {
    title: '如何获取 Webhook 地址',
    slack: 'Slack Apps → Incoming Webhooks → Add to Workspace。',
    dingtalk: '钉钉群 → 群设置 → 机器人 → 添加自定义机器人 → 复制 Webhook 地址。',
    wecom: '企业微信群 → 群设置 → 群机器人 → 添加机器人 → 复制 Webhook 地址。',
    feishu: '飞书群 → 设置 → 群机器人 → 添加自定义机器人 → 复制 Webhook 地址。',
  },
},
```

**Verify:** `python3 scripts/i18n-audit.py` passes (key count matches between en/zh).

---

### Task 14: Build WebhookTab React component

**Objective:** Create the webhook management UI as a new tab in the Settings page.

**Files:**
- Create: `web/src/admin/components/WebhookTab.tsx`

The component should include:
1. List of existing webhooks (name, platform badge, URL truncated, events, last sent time, enabled toggle, test/edit/delete buttons)
2. "Add Webhook" button that opens a modal form with fields: Name, Platform (select), URL, Events (multi-select checkboxes), Cooldown (number input)
3. Platform-specific guide text below the URL field
4. Test button that calls `POST /webhooks/:id/test` and shows a toast
5. Delete confirmation dialog

**Sketch (key states):**

```
┌─ Webhooks ──────────────────────────────────────────────────────┐
│ Get alerted on Slack, DingTalk, WeCom, or Feishu.  [+ Add Webhook] │
│                                                                   │
│ ┌─ Ops DingTalk ───────────────────────────────────────────────┐ │
│ │ 钉钉  │ https://oapi.dingtalk.com/robot/...xxxx              │ │
│ │ Events: upstream_down, disk_high  │ Cooldown: 30min          │ │
│ │ Last sent: 2 hours ago                                       │ │
│ │ [Test] [Edit] [Delete]                                       │ │
│ └──────────────────────────────────────────────────────────────┘ │
└───────────────────────────────────────────────────────────────────┘
```

**Implementation detail:** I'll write the full component. It's ~200 LOC of React.

**Verify:** `cd web && npx tsc --noEmit` passes. `cd web && npm run build` succeeds.

---

### Task 15: Wire WebhookTab into Settings page

**Objective:** Add the "Webhooks" tab to the Settings page tab bar.

**Files:**
- Modify: `web/src/admin/pages/Settings.tsx`

**Step 1: Add import**

```tsx
import WebhookTab from '@/admin/components/WebhookTab'
```

**Step 2: Add to tabs array** (after 'auth')

```tsx
{ key: 'webhooks' as const, label: t('settings.webhooks'), icon: 'bell' },
```

**Step 3: Update TabKey type**

```tsx
type TabKey = 'basic' | 'cache' | 'storage' | 'auth' | 'webhooks'
```

**Step 4: Add content rendering for the 'webhooks' tab**

In the JSX switch/render area, add:

```tsx
{activeTab === 'webhooks' && <WebhookTab />}
```

**Verify:** `cd web && npm run build` succeeds.

---

### Task 16: Integration test for webhook API

**Objective:** Verify the CRUD endpoints work correctly with the test server.

**Files:**
- Create: `tests/integration/webhook_test.go`

```go
// +build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestWebhookCRUD(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()

	// Login as admin
	client := ts.AdminClient()

	// List — should be empty initially
	resp := client.Get("/api/v1/admin/webhooks")
	assertStatus(t, resp, http.StatusOK)
	var list []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}

	// Create
	createBody := map[string]interface{}{
		"name":     "Test Slack",
		"platform": "slack",
		"url":      "https://hooks.slack.com/services/test",
		"events":   "upstream_down",
	}
	resp = client.PostJSON("/api/v1/admin/webhooks", createBody)
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// List — should have 1
	resp = client.Get("/api/v1/admin/webhooks")
	assertStatus(t, resp, http.StatusOK)
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("expected 1 webhook, got %d", len(list))
	}

	id := int(list[0]["id"].(float64))

	// Update
	updateBody := map[string]interface{}{"name": "Updated Slack"}
	resp = client.PutJSON("/api/v1/admin/webhooks/"+itoa(id), updateBody)
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()

	// Delete
	resp = client.Delete("/api/v1/admin/webhooks/" + itoa(id))
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}

func TestWebhookTestSend(t *testing.T) {
	ts := NewTestServer(t)
	defer ts.Close()
	client := ts.AdminClient()

	// Create a webhook
	resp := client.PostJSON("/api/v1/admin/webhooks", map[string]interface{}{
		"name":     "Test",
		"platform": "generic",
		"url":      "https://example.com/webhook",
	})
	assertStatus(t, resp, http.StatusCreated)
	resp.Body.Close()

	// Test send (will fail since example.com, but shouldn't crash)
	resp = client.Post("/api/v1/admin/webhooks/1/test", nil)
	// 200 means the dispatch was triggered (network errors are async)
	assertStatus(t, resp, http.StatusOK)
	resp.Body.Close()
}
```

**Verify:** `go test ./tests/integration/... -v -run TestWebhook -tags integration` passes.

---

## Phase 7: End-to-end smoke test

### Task 17: Manual verification steps

After all tasks complete, verify end-to-end:

```bash
# 1. Build and start
make build
DEPSILO_CONFIG=config.example.toml ./bin/depsilo serve

# 2. Login and create a webhook
curl -s -X POST http://localhost:23333/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin"}' | jq -r '.token' > /tmp/token
TOKEN=$(cat /tmp/token)

curl -s -X POST http://localhost:23333/api/v1/admin/webhooks \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Test","platform":"generic","url":"https://httpbin.org/post","events":"*"}' | jq .

# 3. Send test notification
curl -s -X POST http://localhost:23333/api/v1/admin/webhooks/1/test \
  -H "Authorization: Bearer $TOKEN" | jq .

# 4. List webhooks
curl -s http://localhost:23333/api/v1/admin/webhooks \
  -H "Authorization: Bearer $TOKEN" | jq .

# 5. Delete webhook
curl -s -X DELETE http://localhost:23333/api/v1/admin/webhooks/1 \
  -H "Authorization: Bearer $TOKEN" | jq .
```

---

## Summary

| Phase | Tasks | LOC (est.) | Time (est.) |
|-------|-------|-----------|-------------|
| 1. DB model + config | 1-3 | ~80 | 30 min |
| 2. Notification engine | 4-6 | ~350 | 1.5 hr |
| 3. Background scheduler | 7-8 | ~160 | 1 hr |
| 4. Server wiring | 9 | ~20 | 15 min |
| 5. Admin API | 10-11 | ~180 | 45 min |
| 6. Frontend | 12-15 | ~350 | 2 hr |
| 7. Integration test | 16-17 | ~100 | 30 min |
| **Total** | **17 tasks** | **~1,240** | **~6.5 hr** |

**Files created:** 8 new files
**Files modified:** 6 existing files

**Risks:**
- The disk usage check in the scheduler is a placeholder — needs `cache.Storage.TotalSize()` integration in a follow-up
- The upstream health check relies on `Upstream.Mu()` being exported (verify in `internal/upstream/pool.go`)
- No dedup logic for repeated alerts across scheduler ticks — the cooldown mechanism handles this at the dispatch level
