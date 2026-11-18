package admin

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/api/contract"
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
	quarantine     *quarantine.Policy
}

func NewCapabilityHandler(database *gorm.DB, cfg *config.Config, store *config.Store, ecosystems []string, policy rules.PolicyStatusProvider, blocklistStore *blocklist.Store, blocklistMode string, quarantinePolicy *quarantine.Policy) *CapabilityHandler {
	return &CapabilityHandler{db: database, config: cfg, configStore: store, ecosystems: append([]string(nil), ecosystems...), policy: policy, blocklistStore: blocklistStore, blocklistMode: blocklistMode, quarantine: quarantinePolicy}
}

func (h *CapabilityHandler) Summary(c *gin.Context) {
	ctx := c.Request.Context()
	var settings *contract.AdminSettingsResponse
	if h.configStore != nil {
		if state, err := h.configStore.Snapshot(ctx); err == nil {
			response := adminSettingsResponse(state)
			settings = &response
		}
	}

	active := h.ecosystems
	if len(active) == 0 {
		for _, d := range ecosystem.All() {
			active = append(active, d.Name)
		}
	}
	active = uniqueSorted(active)
	facts := make([]contract.CapabilityFact, 0, len(active)*6)
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
		facts = append(facts, contract.CapabilityFact{Name: "policy_snapshot", Support: contract.CapabilitySupportSupported, Mode: "block", DataStatus: policyDataStatus(status), LastSuccessAt: timePointer(status.LastSuccessfulRefresh), RecentFailure: policyFailure(status)})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, contract.CapabilitySummary{
		Version: version.Version, Commit: version.Commit, BuildDate: version.BuildDate, Capabilities: facts,
		Settings: settings,
	})
}

func (h *CapabilityHandler) proxyFact(name string, supported bool) contract.CapabilityFact {
	f := contract.CapabilityFact{Name: "proxy_cache", Ecosystem: name, Support: contract.CapabilitySupportUnsupported, Mode: "off", DataStatus: contract.CapabilityDataStatusUnknown}
	if supported {
		f.Support, f.Mode, f.DataStatus = contract.CapabilitySupportSupported, "on", contract.CapabilityDataStatusUnknown
	}
	return f
}

func (h *CapabilityHandler) rulesFact(name string, supported bool) contract.CapabilityFact {
	f := contract.CapabilityFact{Name: "manual_rules", Ecosystem: name, Support: contract.CapabilitySupportUnsupported, Mode: "off", DataStatus: contract.CapabilityDataStatusUnknown}
	if supported {
		f.Support, f.Mode = contract.CapabilitySupportSupported, "block"
	}
	if h.policy != nil {
		st := h.policy.PolicyStatus()
		f.DataStatus, f.LastSuccessAt, f.RecentFailure = policyDataStatus(st), timePointer(st.LastSuccessfulRefresh), policyFailure(st)
	}
	return f
}

func (h *CapabilityHandler) blocklistFact(ctx context.Context, name string, supported bool) contract.CapabilityFact {
	f := contract.CapabilityFact{Name: "malicious_blocklist", Ecosystem: name, Support: contract.CapabilitySupportUnsupported, Mode: "off", DataStatus: contract.CapabilityDataStatusNeverSynced}
	if !supported {
		return f
	}
	f.Support = contract.CapabilitySupportSupported
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
		f.DataStatus, f.RecentFailure = contract.CapabilityDataStatusError, "sync state unavailable"
		return f
	}
	f.LastSuccessAt = timePointerPtr(st.LastSuccessAt)
	f.RecentFailure = st.LastError
	if st.LastSuccessAt == nil {
		if st.LastError != "" {
			f.DataStatus = contract.CapabilityDataStatusError
		}
		return f
	}
	if st.LastError != "" {
		f.DataStatus = contract.CapabilityDataStatusStale
	} else {
		f.DataStatus = contract.CapabilityDataStatusFresh
	}
	return f
}

func (h *CapabilityHandler) securityFact(ctx context.Context, name string) contract.CapabilityFact {
	supported := security.SupportsAutomaticVulnerabilityScanning(name)
	f := contract.CapabilityFact{Name: "vulnerability_scanning", Ecosystem: name, Support: contract.CapabilitySupportUnsupported, Mode: "off", DataStatus: contract.CapabilityDataStatusNeverSynced}
	if !supported || h.db == nil {
		return f
	}
	f.Support = contract.CapabilitySupportSupported
	if h.config != nil && h.config.Security.Enabled {
		f.Mode = "alert_only"
	}
	var check db.VulnerabilityCheck
	err := h.db.WithContext(ctx).Where("ecosystem = ?", name).Order("last_fetched_at DESC").First(&check).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return f
		}
		f.DataStatus, f.RecentFailure = contract.CapabilityDataStatusError, "vulnerability state unavailable"
		return f
	}
	f.LastSuccessAt = timePointer(check.LastFetchedAt)
	if !check.NextFetchAt.IsZero() && time.Now().After(check.NextFetchAt) {
		f.DataStatus = contract.CapabilityDataStatusStale
	} else {
		f.DataStatus = contract.CapabilityDataStatusFresh
	}
	return f
}

func (h *CapabilityHandler) ageFact(name string) contract.CapabilityFact {
	f := contract.CapabilityFact{Name: "minimum_release_age", Ecosystem: name, Support: contract.CapabilitySupportSafetyDisabled, Mode: "off", DataStatus: contract.CapabilityDataStatusNeverSynced}
	bound := quarantine.SupportsMinimumReleaseAge(name)
	if h.quarantine != nil {
		bound = bound || h.quarantine.SourceProvenanceBound(name)
	}
	if !bound {
		return f
	}
	f.Support = contract.CapabilitySupportSupported
	f.DataStatus = contract.CapabilityDataStatusSourceBound
	if h.quarantine.ApproximateProvenance(name) {
		f.DataStatus = contract.CapabilityDataStatusApproximate
	}
	if h.quarantine.ObservationProvenance(name) {
		f.DataStatus = contract.CapabilityDataStatusObserved
	}
	if h.quarantine.IsAgeGateEnabled() && h.quarantine.Threshold(name) > 0 {
		if h.quarantine.Mode == quarantine.ModeWarn {
			f.Mode = "warn"
		} else {
			f.Mode = "block"
		}
	}
	return f
}

func (h *CapabilityHandler) tamperFact(name string) contract.CapabilityFact {
	f := contract.CapabilityFact{Name: "tamper_alerts", Ecosystem: name, Support: contract.CapabilitySupportSupported, Mode: "alert_only", DataStatus: contract.CapabilityDataStatusUnknown}
	if h.config != nil && !h.config.SupplyChain.TamperDetection.IsEnabled() {
		f.Mode = "off"
	}
	return f
}

func policyDataStatus(s rules.PolicyStatus) contract.CapabilityDataStatus {
	if s.LastSuccessfulRefresh.IsZero() {
		return contract.CapabilityDataStatusNeverSynced
	}
	if s.Degraded || s.UsingStaleSnapshot {
		return contract.CapabilityDataStatusStale
	}
	return contract.CapabilityDataStatusFresh
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
