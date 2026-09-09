package admin

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/blocklist"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/ecosystem"
	"depsilo/internal/quarantine"
	"depsilo/internal/rules"
	"depsilo/internal/security"
	"depsilo/internal/version"
)

// CapabilityHandler exposes a bounded, read-only view of runtime capability
// facts. It deliberately composes existing stores and providers; it does not
// refresh policies, sync datasets, probe upstreams, or invent a second
// capability registry.
type CapabilityHandler struct {
	db             *gorm.DB
	config         *config.Config
	configStore    *config.Store
	ecosystems     []string
	policy         rules.PolicyStatusProvider
	blocklistStore *blocklist.Store
	blocklistMode  string
}

func NewCapabilityHandler(database *gorm.DB, cfg *config.Config, store *config.Store, ecosystems []string, policy rules.PolicyStatusProvider, blocklistStore *blocklist.Store, blocklistMode string) *CapabilityHandler {
	return &CapabilityHandler{db: database, config: cfg, configStore: store, ecosystems: append([]string(nil), ecosystems...), policy: policy, blocklistStore: blocklistStore, blocklistMode: blocklistMode}
}

type capabilityFact struct {
	Name          string  `json:"name"`
	Ecosystem     string  `json:"ecosystem"`
	Support       string  `json:"support"`
	Mode          string  `json:"mode"`
	DataStatus    string  `json:"data_status"`
	LastSuccessAt *string `json:"last_success_at,omitempty"`
	RecentFailure string  `json:"recent_failure,omitempty"`
}

type capabilitySummary struct {
	Version      string                `json:"version"`
	Commit       string                `json:"commit"`
	BuildDate    string                `json:"build_date"`
	Capabilities []capabilityFact      `json:"capabilities"`
	Settings     *config.SettingsState `json:"settings,omitempty"`
}

func (h *CapabilityHandler) Summary(c *gin.Context) {
	ctx := c.Request.Context()
	settings := (*config.SettingsState)(nil)
	if h.configStore != nil {
		if state, err := h.configStore.Snapshot(ctx); err == nil {
			settings = &state
		}
	}

	active := h.ecosystems
	if len(active) == 0 {
		for _, d := range ecosystem.All() {
			active = append(active, d.Name)
		}
	}
	active = uniqueSorted(active)
	facts := make([]capabilityFact, 0, len(active)*6)
	for _, name := range active {
		definition, known := ecosystem.Lookup(name)
		if !known {
			continue
		}
		facts = append(facts,
			h.proxyFact(name, definition.StandardUpstreams),
			h.rulesFact(name, definition.RuleEnforcement),
			h.blocklistFact(ctx, name, definition.MaliciousDataset),
			h.securityFact(ctx, name),
			h.ageFact(name),
			h.tamperFact(name),
		)
	}
	if h.policy != nil {
		status := h.policy.PolicyStatus()
		facts = append(facts, capabilityFact{Name: "policy_snapshot", Support: "supported", Mode: "block", DataStatus: policyDataStatus(status), LastSuccessAt: timePointer(status.LastSuccessfulRefresh), RecentFailure: policyFailure(status)})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, capabilitySummary{Version: version.Version, Commit: version.Commit, BuildDate: version.BuildDate, Capabilities: facts, Settings: settings})
}

func (h *CapabilityHandler) proxyFact(name string, supported bool) capabilityFact {
	f := capabilityFact{Name: "proxy_cache", Ecosystem: name, Support: "unsupported", Mode: "off", DataStatus: "unknown"}
	if supported {
		f.Support, f.Mode, f.DataStatus = "supported", "on", "unknown"
	}
	return f
}

func (h *CapabilityHandler) rulesFact(name string, supported bool) capabilityFact {
	f := capabilityFact{Name: "manual_rules", Ecosystem: name, Support: "unsupported", Mode: "off", DataStatus: "unknown"}
	if supported {
		f.Support, f.Mode = "supported", "block"
	}
	if h.policy != nil {
		st := h.policy.PolicyStatus()
		f.DataStatus, f.LastSuccessAt, f.RecentFailure = policyDataStatus(st), timePointer(st.LastSuccessfulRefresh), policyFailure(st)
	}
	return f
}

func (h *CapabilityHandler) blocklistFact(ctx context.Context, name string, supported bool) capabilityFact {
	f := capabilityFact{Name: "malicious_blocklist", Ecosystem: name, Support: "unsupported", Mode: "off", DataStatus: "never_synced"}
	if !supported {
		return f
	}
	f.Support = "supported"
	if h.blocklistStore == nil {
		f.Mode = "off"
		return f
	}
	f.Mode = h.blocklistMode
	if f.Mode == "" {
		f.Mode = "block"
	}
	st, err := h.blocklistStore.SyncState(ctx)
	if err != nil {
		f.DataStatus, f.RecentFailure = "error", "sync state unavailable"
		return f
	}
	f.LastSuccessAt = timePointerPtr(st.LastSuccessAt)
	f.RecentFailure = st.LastError
	if st.LastSuccessAt == nil {
		if st.LastError != "" {
			f.DataStatus = "error"
		}
		return f
	}
	if st.LastError != "" {
		f.DataStatus = "stale"
	} else {
		f.DataStatus = "fresh"
	}
	return f
}

func (h *CapabilityHandler) securityFact(ctx context.Context, name string) capabilityFact {
	supported := security.SupportsAutomaticVulnerabilityScanning(name)
	f := capabilityFact{Name: "vulnerability_scanning", Ecosystem: name, Support: "unsupported", Mode: "off", DataStatus: "never_synced"}
	if !supported || h.db == nil {
		return f
	}
	f.Support = "supported"
	if h.config != nil && h.config.Security.Enabled {
		f.Mode = "alert_only"
	}
	var check db.VulnerabilityCheck
	err := h.db.WithContext(ctx).Where("ecosystem = ?", name).Order("last_fetched_at DESC").First(&check).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return f
		}
		f.DataStatus, f.RecentFailure = "error", "vulnerability state unavailable"
		return f
	}
	f.LastSuccessAt = timePointer(check.LastFetchedAt)
	if !check.NextFetchAt.IsZero() && time.Now().After(check.NextFetchAt) {
		f.DataStatus = "stale"
	} else {
		f.DataStatus = "fresh"
	}
	return f
}

func (h *CapabilityHandler) ageFact(name string) capabilityFact {
	f := capabilityFact{Name: "minimum_release_age", Ecosystem: name, Support: "safety_disabled", Mode: "off", DataStatus: "never_synced"}
	if quarantine.SupportsMinimumReleaseAge(name) {
		f.Support = "supported"
	}
	return f
}

func (h *CapabilityHandler) tamperFact(name string) capabilityFact {
	f := capabilityFact{Name: "tamper_alerts", Ecosystem: name, Support: "supported", Mode: "alert_only", DataStatus: "unknown"}
	if h.config != nil && !h.config.SupplyChain.TamperDetection.IsEnabled() {
		f.Mode = "off"
	}
	return f
}

func policyDataStatus(s rules.PolicyStatus) string {
	if s.LastSuccessfulRefresh.IsZero() {
		return "never_synced"
	}
	if s.Degraded || s.UsingStaleSnapshot {
		return "stale"
	}
	return "fresh"
}
func policyFailure(s rules.PolicyStatus) string {
	if s.RefreshFailures > 0 && s.Degraded {
		return "policy refresh failed"
	}
	return ""
}
func timePointer(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	v := t.UTC().Format(time.RFC3339Nano)
	return &v
}
func timePointerPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return timePointer(*t)
}
func uniqueSorted(in []string) []string {
	m := map[string]bool{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" {
			m[v] = true
		}
	}
	out := make([]string, 0, len(m))
	for v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
