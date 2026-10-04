package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/sysmetrics"
)

// RuntimeHandler exposes the shared process/storage sample to Admin. It never
// triggers a new sample, and it never walks the cache directory: logical cache
// size comes from the metadata table, disk capacity from statfs.
type RuntimeHandler struct {
	db          *gorm.DB
	sampler     *sysmetrics.Sampler
	storageType string
	storagePath string
	maxSizeGB   int
}

func NewRuntimeHandler(database *gorm.DB, sampler *sysmetrics.Sampler, storageType, storagePath string, maxSizeGB int) *RuntimeHandler {
	return &RuntimeHandler{
		db:          database,
		sampler:     sampler,
		storageType: storageType,
		storagePath: storagePath,
		maxSizeGB:   maxSizeGB,
	}
}

// Get returns the most recent resource sample plus cache storage facts.
//
// Cache fields are kept distinct on purpose:
//   - logical_bytes: SUM(cache_entries.size), the metadata view of cached bytes
//   - physical_bytes: reserved for a real on-disk occupancy figure (currently
//     not collected; the client must not substitute logical size for it)
//   - disk: the filesystem holding the local storage path (statfs)
func (h *RuntimeHandler) Get(c *gin.Context) {
	sample := sysmetrics.Snapshot{}
	if h.sampler != nil {
		sample = h.sampler.Snapshot()
	}

	cache := gin.H{
		"storage_type":   h.storageType,
		"storage_path":   h.storagePath,
		"logical_bytes":  int64(0),
		"quota_bytes":    nil,
		"physical_bytes": nil,
		"physical_known": false,
	}
	var logical int64
	if err := h.db.WithContext(c.Request.Context()).
		Model(&db.CacheEntry{}).
		Select("COALESCE(SUM(size), 0)").
		Scan(&logical).Error; err == nil {
		cache["logical_bytes"] = logical
	}
	if h.maxSizeGB > 0 {
		cache["quota_bytes"] = int64(h.maxSizeGB) * 1024 * 1024 * 1024
	}

	heapAlloc, runtimeSys := sysmetrics.GoRuntimeMemory()

	c.JSON(http.StatusOK, gin.H{
		"sampled_at":       sample.SampledAt,
		"interval_seconds": sample.IntervalSeconds,
		"process":          sample.Process,
		"go_runtime": gin.H{
			"heap_alloc_bytes": heapAlloc,
			"sys_bytes":        runtimeSys,
		},
		"cache": cache,
		"disk":  sample.Disk,
		"host":  gin.H{"hostname": sysmetrics.Hostname()},
	})
}
