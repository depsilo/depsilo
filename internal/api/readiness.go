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
func readinessHandler(database *gorm.DB, storage cache.Storage) gin.HandlerFunc {
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
		c.Header("Cache-Control", "no-store")
		c.JSON(status, gin.H{
			"status":  state,
			"version": version.Version,
			"checks":  checks,
		})
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
