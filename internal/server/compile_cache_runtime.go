package server

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"depsilo/internal/api"
	"depsilo/internal/cache"
	"depsilo/internal/compilecache"
	"depsilo/internal/config"
)

// compileCacheRuntime owns the compiler-cache storage, startup repair and
// maintenance lifetime. A disabled runtime still supplies coherent handler
// dependencies so the protocol routes can return their intentional 404s.
type compileCacheRuntime struct {
	handlers api.CompileCacheRouteDependencies

	cancelMaintenance context.CancelFunc
	maintenanceDone   <-chan struct{}
	cancelOnce        sync.Once
}

// openCompileCacheRuntime constructs the complete compiler-cache data domain.
// Every fallible startup operation completes before maintenance starts, so an
// error cannot leave a server-owned goroutine behind.
func openCompileCacheRuntime(
	parent context.Context,
	packageStorage config.StorageConfig,
	cfg config.CompileCacheConfig,
	database *gorm.DB,
) (*compileCacheRuntime, error) {
	if parent == nil {
		parent = context.Background()
	}
	runtime := &compileCacheRuntime{
		handlers: api.CompileCacheRouteDependencies{
			Enabled:    cfg.Enabled,
			PublicURL:  cfg.PublicURL,
			Authorizer: compilecache.NewAuthorizer(database),
		},
		maintenanceDone: closedSignal(),
	}
	if !cfg.Enabled {
		return runtime, nil
	}

	if err := validateCompileCacheStorageSeparation(packageStorage, cfg.Storage); err != nil {
		return nil, err
	}
	storage, err := openCompileCacheStorage(cfg.Storage)
	if err != nil {
		return nil, fmt.Errorf("init compiler-cache storage: %w", err)
	}
	service, err := compilecache.NewService(storage, database, compileCacheLimits(cfg))
	if err != nil {
		return nil, fmt.Errorf("init compiler cache: %w", err)
	}
	if err := service.ProcessPendingDeletions(parent, 1000); err != nil {
		return nil, fmt.Errorf("retry compiler-cache deletions: %w", err)
	}
	// No request can be active during single-instance startup, so every
	// unreferenced generation (including local .tmp files) is safe to reclaim.
	if err := service.Reconcile(parent, 0); err != nil {
		return nil, fmt.Errorf("reconcile compiler cache: %w", err)
	}
	if _, err := service.EnforceLimits(parent); err != nil {
		return nil, fmt.Errorf("enforce compiler-cache limits: %w", err)
	}
	service.SetObserver(compileCacheMetricsObserver())
	runtime.handlers.Service = service

	maintenanceContext, cancelMaintenance := context.WithCancel(parent)
	maintenanceDone := make(chan struct{})
	runtime.cancelMaintenance = cancelMaintenance
	runtime.maintenanceDone = maintenanceDone
	go func() {
		defer close(maintenanceDone)
		service.RunMaintenance(maintenanceContext)
	}()

	zap.L().Info("compiler cache enabled",
		zap.String("ccache_endpoint", "/ccache/v1/{namespace}"),
		zap.String("sccache_endpoint", "/sccache/v1/{namespace}"),
		zap.String("public_url", cfg.PublicURL),
		zap.String("storage_type", cfg.Storage.Type),
		zap.Int("max_size_gb", cfg.MaxSizeGB),
		zap.Int("max_entry_size_mb", cfg.MaxEntrySizeMB),
	)
	if strings.HasPrefix(strings.ToLower(cfg.PublicURL), "http://") && cfg.AllowInsecureHTTP {
		zap.L().Warn("compiler-cache bearer credentials are using explicitly enabled plaintext HTTP; restrict access to a trusted LAN/VPN")
	}
	return runtime, nil
}

func (runtime *compileCacheRuntime) handlerDependencies() api.CompileCacheRouteDependencies {
	if runtime == nil {
		return api.CompileCacheRouteDependencies{}
	}
	return runtime.handlers
}

// Close stops maintenance and waits for RunMaintenance's final metadata flush.
// A timed-out caller may retry; cancellation is issued at most once.
func (runtime *compileCacheRuntime) Close(ctx context.Context) error {
	if runtime == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	runtime.cancelOnce.Do(func() {
		if runtime.cancelMaintenance != nil {
			runtime.cancelMaintenance()
		}
	})
	select {
	case <-runtime.maintenanceDone:
		return nil
	default:
	}
	select {
	case <-runtime.maintenanceDone:
		return nil
	case <-ctx.Done():
		select {
		case <-runtime.maintenanceDone:
			return nil
		default:
			return ctx.Err()
		}
	}
}

func closedSignal() <-chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func validateCompileCacheStorageSeparation(packageStorage, compileStorage config.StorageConfig) error {
	if packageStorage.Type == "local" && compileStorage.Type == "local" {
		overlaps, err := localStoragePathsOverlap(packageStorage.Path, compileStorage.Path)
		if err != nil {
			return fmt.Errorf("compare package and compiler cache paths: %w", err)
		}
		if overlaps {
			return errors.New("compile_cache.storage.path must not overlap storage.path")
		}
	}
	if packageStorage.Type == "s3" && compileStorage.Type == "s3" &&
		strings.EqualFold(strings.TrimRight(packageStorage.Endpoint, "/"), strings.TrimRight(compileStorage.Endpoint, "/")) &&
		packageStorage.Bucket == compileStorage.Bucket {
		return errors.New("compile_cache.storage.bucket must be separate from the package-cache bucket")
	}
	return nil
}

func openCompileCacheStorage(cfg config.StorageConfig) (cache.Storage, error) {
	switch cfg.Type {
	case "local":
		return cache.NewPrivateLocalStorage(cfg.Path)
	case "s3":
		return cache.NewS3Storage(cfg.Endpoint, cfg.Bucket, cfg.Region, cfg.AccessKey, cfg.SecretKey)
	default:
		return nil, fmt.Errorf("unsupported storage type %q", cfg.Type)
	}
}

func compileCacheLimits(cfg config.CompileCacheConfig) compilecache.Limits {
	return compilecache.Limits{
		MaxBytes:               int64(cfg.MaxSizeGB) * 1024 * 1024 * 1024,
		MaxEntries:             cfg.MaxEntries,
		MaxEntryBytes:          int64(cfg.MaxEntrySizeMB) * 1024 * 1024,
		NamespaceMaxBytes:      int64(cfg.NamespaceMaxSizeGB) * 1024 * 1024 * 1024,
		NamespaceMaxEntries:    cfg.NamespaceMaxEntries,
		MaxConcurrentUploads:   cfg.MaxConcurrentUploads,
		MaxQueuedUploads:       cfg.MaxQueuedUploads,
		MaxInflightUploadBytes: int64(cfg.MaxInflightUploadSizeMB) * 1024 * 1024,
		UploadTimeout:          cfg.UploadTimeout,
		MaxConcurrentDownloads: cfg.MaxConcurrentDownloads,
		DownloadTimeout:        cfg.DownloadTimeout,
		HighWatermarkPercent:   cfg.LRUThreshold,
	}
}

func compileCacheMetricsObserver() compilecache.Observer {
	return compilecache.Observer{
		StatsUpdated: func(stats compilecache.Stats) {
			api.M.CompileCacheSizeBytes.Set(float64(stats.SizeBytes))
			api.M.CompileCacheEntries.Set(float64(stats.Entries))
		},
		Evicted: func(reason string, entries int) {
			api.M.CompileCacheEvictions.WithLabelValues(reason).Add(float64(entries))
		},
	}
}

func localStoragePathsOverlap(left, right string) (bool, error) {
	leftAbsolute, err := canonicalStoragePath(left)
	if err != nil {
		return false, err
	}
	rightAbsolute, err := canonicalStoragePath(right)
	if err != nil {
		return false, err
	}
	// Windows and the default macOS filesystems treat case-only path variants
	// as the same namespace. Be conservative on those platforms even when the
	// configured leaf does not exist yet and therefore cannot be compared with
	// os.SameFile.
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		leftAbsolute = strings.ToLower(leftAbsolute)
		rightAbsolute = strings.ToLower(rightAbsolute)
	}
	contains := func(parent, child string) (bool, error) {
		relative, err := filepath.Rel(parent, child)
		if err != nil {
			return false, err
		}
		return relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)), nil
	}
	leftContainsRight, err := contains(leftAbsolute, rightAbsolute)
	if err != nil {
		return false, err
	}
	rightContainsLeft, err := contains(rightAbsolute, leftAbsolute)
	if err != nil {
		return false, err
	}
	return leftContainsRight || rightContainsLeft, nil
}

// canonicalStoragePath resolves every existing symlink component, including
// when the final cache directory has not been created yet. This prevents two
// visually different configured paths from aliasing the same storage root.
func canonicalStoragePath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(absolute)
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}
