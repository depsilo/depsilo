package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"depsilo/internal/api/contract"
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

		checks := contract.ReadinessChecks{
			Database: contract.ReadinessCheckReady,
			Storage:  contract.ReadinessCheckReady,
		}
		ready := true
		if err := checkDatabaseReady(ctx, database); err != nil {
			checks.Database = contract.ReadinessCheckUnavailable
			ready = false
		}
		if err := checkStorageReady(ctx, storage); err != nil {
			checks.Storage = contract.ReadinessCheckUnavailable
			ready = false
		}

		status := http.StatusOK
		state := contract.ReadinessStateReady
		if !ready {
			status = http.StatusServiceUnavailable
			state = contract.ReadinessStateNotReady
		}
		response := contract.ReadinessResponse{
			Status:  state,
			Version: version.Version,
			Checks:  checks,
		}
		// Policy freshness is informative rather than a readiness dependency:
		// a stale snapshot remains a valid serving state, and must not turn a
		// healthy database/storage pair into an HTTP 503. Keep the provider
		// optional so lightweight API tests and setup-mode callers retain the
		// original handler contract.
		if len(policyProviders) > 0 && policyProviders[0] != nil {
			policy := policyStatusView(readPolicyStatus(policyProviders[0]))
			response.Policy = &policy
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
