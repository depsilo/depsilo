package api

import (
	"strings"
	"time"

	"depsilo/internal/api/contract"
	"depsilo/internal/rules"
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

// policyStatusView builds the public policy block of the readiness body. The
// snapshot timestamp and age are added only once a snapshot has loaded;
// fallback mode and failure counts stay on the authenticated Admin route.
func policyStatusView(status rules.PolicyStatus) contract.ReadinessPolicy {
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

	view := contract.ReadinessPolicy{
		Status:             contract.PolicyStatusKind(state),
		Degraded:           status.Degraded,
		UsingStaleSnapshot: status.UsingStaleSnapshot,
	}
	if !loadedAt.IsZero() {
		// Derive age from the durable event time at read time. A provider may
		// retain a cached age for telemetry transitions, but an operational
		// endpoint must not let that value stop advancing between refreshes.
		age := policySnapshotAge(status, time.Now())
		view.SnapshotLoadedAt = loadedAt.UTC().Format(time.RFC3339Nano)
		view.SnapshotAgeSeconds = &age
	}
	return view
}
