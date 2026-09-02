package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/version"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const readinessTimeout = 2 * time.Second

// readinessHandler verifies only dependencies required to serve cached proxy
// traffic. Upstream health is intentionally excluded: Depsilo can still serve
// cache hits while every remote source is degraded.
func readinessHandler(database *gorm.DB, storage cache.Storage, policyProviders ...PolicyStatusProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readinessTimeout)
		defer cancel()

		checks := gin.H{
			"database": "ready",
			"storage":  "ready",
		}
		ready := true
		if err := checkDatabaseReady(ctx, database); err != nil {
			checks["database"] = "unavailable"
			ready = false
		}
		if err := checkStorageReady(ctx, storage); err != nil {
			checks["storage"] = "unavailable"
			ready = false
		}

		status := http.StatusOK
		state := "ready"
		if !ready {
			status = http.StatusServiceUnavailable
			state = "not_ready"
		}
		response := gin.H{
			"status":  state,
			"version": version.Version,
			"checks":  checks,
		}
		// Policy freshness is informative rather than a readiness dependency:
		// a stale snapshot remains a valid serving state, and must not turn a
		// healthy database/storage pair into an HTTP 503. Keep the provider
		// optional so lightweight API tests and setup-mode callers retain the
		// original handler contract.
		if len(policyProviders) > 0 && policyProviders[0] != nil {
			statusValue := readPolicyStatus(policyProviders[0])
			policy := policyStatusView(statusValue, false)
			if loadedTime := policySnapshotLoadedAt(statusValue); !loadedTime.IsZero() {
				// These two fields are safe, useful freshness context for operators
				// polling /ready; fallback mode and internal errors stay on the
				// authenticated Admin status route.
				loadedAt := loadedTime.UTC().Format(time.RFC3339Nano)
				// Keep both names during the 0.9.x contract transition. The
				// canonical operator-facing name is snapshot_loaded_at; the
				// last_successful_refresh alias is retained for clients that
				// consumed the initial status endpoint draft.
				policy["snapshot_loaded_at"] = loadedAt
				policy["last_successful_refresh"] = loadedAt
				policy["snapshot_age_seconds"] = policySnapshotAge(statusValue, time.Now())
			}
			response["policy"] = policy
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(status, response)
	}
}

func checkDatabaseReady(ctx context.Context, database *gorm.DB) error {
	if database == nil {
		return gorm.ErrInvalidDB
	}
	var value int
	return database.WithContext(ctx).Raw("SELECT 1").Scan(&value).Error
}

func checkStorageReady(ctx context.Context, storage cache.Storage) error {
	if storage == nil {
		return gorm.ErrInvalidDB
	}
	probe, ok := storage.(cache.ReadinessProber)
	if !ok {
		return errors.New("storage does not expose a readiness probe")
	}
	return probe.CheckReady(ctx)
}
