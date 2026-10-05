package admin

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/db"
	"depsilo/internal/sysmetrics"
)

// cacheInventoryTTL bounds how often the metadata aggregate runs when several
// admin tabs poll the Overview every few seconds.
const cacheInventoryTTL = 15 * time.Second

// cacheInventory is the metadata view of the cache: logical bytes, the number
// of cache_entries rows, and the number of distinct packages behind them. A
// package is one (adapter_type, package_name) pair, so metadata and artifact
// rows for the same package version are counted once.
type cacheInventory struct {
	LogicalBytes int64
	Entries      int64
	Packages     int64
}

// RuntimeHandler exposes the shared process/storage sample to Admin. It never
// triggers a new sample, and it never walks the cache directory: logical cache
// size and package inventory come from the metadata table, disk capacity from
// statfs.
type RuntimeHandler struct {
	db          *gorm.DB
	sampler     *sysmetrics.Sampler
	storageType string
	storagePath string
	maxSizeGB   int

	mu          sync.Mutex
	inventory   *cacheInventory
	inventoried time.Time
	now         func() time.Time
}

func NewRuntimeHandler(database *gorm.DB, sampler *sysmetrics.Sampler, storageType, storagePath string, maxSizeGB int) *RuntimeHandler {
	return &RuntimeHandler{
		db:          database,
		sampler:     sampler,
		storageType: storageType,
		storagePath: storagePath,
		maxSizeGB:   maxSizeGB,
		now:         time.Now,
	}
}

// Get returns the most recent resource sample plus cache storage facts.
//
// Cache fields are kept distinct on purpose:
//   - logical_bytes: SUM(cache_entries.size), the metadata view of cached bytes
//   - entries/packages: cache_entries rows and distinct (adapter, package)
//     pairs, omitted entirely when no inventory has ever been read
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
		"entries":        nil,
		"packages":       nil,
		"quota_bytes":    nil,
		"physical_bytes": nil,
		"physical_known": false,
	}
	if inventory, ok := h.cacheInventory(); ok {
		cache["logical_bytes"] = inventory.LogicalBytes
		cache["entries"] = inventory.Entries
		cache["packages"] = inventory.Packages
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

// cacheInventory returns a memoized inventory. The aggregate is one indexed
// scan of the metadata table, never a directory walk or a per-request recount
// for every open dashboard. A failed refresh keeps the last good sample so a
// transient database error cannot render an empty cache; with no prior sample
// the caller omits the fields instead of claiming zero.
func (h *RuntimeHandler) cacheInventory() (cacheInventory, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.inventory != nil && h.now().Sub(h.inventoried) < cacheInventoryTTL {
		return *h.inventory, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var row cacheInventory
	// char(31) is the unit separator: adapter types never contain it, so
	// concatenating it keeps (adapter, package) pairs distinct. Counting a
	// CASE expression skips metadata rows without a package name.
	err := h.db.WithContext(ctx).
		Model(&db.CacheEntry{}).
		Select(`COALESCE(SUM(size), 0) AS logical_bytes,
			COUNT(*) AS entries,
			COUNT(DISTINCT CASE WHEN package_name <> '' THEN adapter_type || char(31) || package_name END) AS packages`).
		Scan(&row).Error
	if err != nil {
		zap.L().Warn("sample cache inventory", zap.Error(err))
		if h.inventory != nil {
			return *h.inventory, true
		}
		return cacheInventory{}, false
	}

	h.inventory = &row
	h.inventoried = h.now()
	return row, true
}
