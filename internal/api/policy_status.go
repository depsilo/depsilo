package api

import (
	"math"
	"strings"
	"time"

	"depsilo/internal/rules"
	"github.com/gin-gonic/gin"
)

// PolicyStatusProvider is the small read-only seam used by operational HTTP
// surfaces and telemetry. The provider must return a coherent snapshot without
// performing a policy-store refresh; request handlers must never make a
// readiness or status probe trigger rule evaluation.
type PolicyStatusProvider = rules.PolicyStatusProvider

func readPolicyStatus(provider PolicyStatusProvider) rules.PolicyStatus {
	if provider == nil {
		return rules.PolicyStatus{}
	}
	return provider.PolicyStatus()
}

// policyStatusView builds the additive JSON object shared by readiness and
// status callers. Details are intentionally optional: the public readiness
// endpoint exposes only safe freshness fields, while the authenticated Admin
// endpoint also reports the configured fallback mode.
func policyStatusView(status rules.PolicyStatus, includeDetails bool) gin.H {
	loadedAt := policySnapshotLoadedAt(status)
	state := strings.TrimSpace(status.Status)
	if state == "" {
		if status.Degraded {
			state = "degraded"
		} else if !loadedAt.IsZero() {
			state = "healthy"
		} else {
			// A provider that has not supplied a state or successful timestamp
			// must never be rendered as healthy by an operational endpoint.
			state = "unavailable"
		}
	}

	age := status.SnapshotAgeSeconds
	if !loadedAt.IsZero() {
		// Derive age from the durable event time at read time. A provider may
		// retain a cached age for telemetry transitions, but an operational
		// endpoint must not let that value stop advancing between refreshes.
		age = policySnapshotAge(status, time.Now())
	}
	if age < 0 || math.IsNaN(age) || math.IsInf(age, 0) {
		age = 0
	}

	view := gin.H{
		"status":               state,
		"degraded":             status.Degraded,
		"using_stale_snapshot": status.UsingStaleSnapshot,
	}
	if includeDetails {
		var last any
		if !loadedAt.IsZero() {
			last = loadedAt.UTC().Format(time.RFC3339Nano)
		}
		view["last_successful_refresh"] = last
		view["snapshot_loaded_at"] = last
		view["snapshot_age_seconds"] = age
		view["refresh_failures"] = status.RefreshFailures
		mode := status.OnLoadError
		if mode == "" {
			mode = rules.DefaultOnLoadErrorPolicy
		}
		view["on_load_error"] = mode
	}
	return view
}
