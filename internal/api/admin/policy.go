package admin

import (
	"math"
	"net/http"
	"time"

	"depsilo/internal/rules"
	"github.com/gin-gonic/gin"
)

// PolicyStatusProvider is the read-only seam consumed by the Admin status
// endpoint. Implementations must return an in-memory snapshot and must not
// perform a policy-store refresh while serving a status request.
type PolicyStatusProvider = rules.PolicyStatusProvider

type PolicyHandler struct {
	provider PolicyStatusProvider
}

func NewPolicyHandler(provider PolicyStatusProvider) *PolicyHandler {
	return &PolicyHandler{provider: provider}
}

// Status returns policy freshness and fallback state for operators. A missing
// provider is represented as an unknown, non-stale policy rather than causing
// the Admin shell to fail; this keeps isolated API fixtures and setup-mode
// servers compatible with the route contract.
func (h *PolicyHandler) Status(c *gin.Context) {
	status := rules.PolicyStatus{}
	if h != nil && h.provider != nil {
		status = h.provider.PolicyStatus()
	}
	loadedAt := status.LastSuccessfulRefresh
	if loadedAt.IsZero() {
		loadedAt = status.SnapshotLoadedAt
	}
	state := status.Status
	if state == "" {
		if h == nil || h.provider == nil {
			state = "unknown"
		} else if status.Degraded {
			state = "degraded"
		} else if loadedAt.IsZero() {
			// A live provider with no successful snapshot is unavailable, not
			// healthy. Keep the distinction visible to operators even when a
			// custom provider omits the derived Status field.
			state = "unavailable"
		} else {
			// Policy status uses the same health vocabulary as the Engine and
			// readiness payload. "ready" is reserved for the service-level
			// readiness envelope, not a policy snapshot state.
			state = "healthy"
		}
	}
	age := status.SnapshotAgeSeconds
	if !loadedAt.IsZero() {
		age = time.Since(loadedAt).Seconds()
		if age < 0 || math.IsNaN(age) || math.IsInf(age, 0) {
			age = 0
		}
	} else {
		age = 0
	}
	var last any
	if !loadedAt.IsZero() {
		last = loadedAt.UTC().Format(time.RFC3339Nano)
	}
	mode := status.OnLoadError
	if mode == "" {
		mode = rules.DefaultOnLoadErrorPolicy
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"status":                  state,
		"degraded":                status.Degraded,
		"using_stale_snapshot":    status.UsingStaleSnapshot,
		"last_successful_refresh": last,
		"snapshot_loaded_at":      last,
		"snapshot_age_seconds":    age,
		"refresh_failures":        status.RefreshFailures,
		"on_load_error":           mode,
	})
}
