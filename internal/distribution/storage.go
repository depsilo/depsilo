package distribution

import (
	"errors"

	"depsilo/internal/cache"
	"depsilo/internal/config"
)

// NewStorage creates a separately accounted retained-content store. Never pass
// this store to ordinary cache retention or create cache entries for its keys.
func NewStorage(cfg config.DistributionConfig, packageStorage, compileStorage config.StorageConfig) (cache.Storage, error) {
	if !cfg.Enabled {
		return nil, errors.New("distribution storage is disabled")
	}
	if err := config.ValidateDistributionConfig(cfg, packageStorage, compileStorage); err != nil {
		return nil, err
	}
	switch cfg.Storage.Type {
	case "local":
		return cache.NewPrivateLocalStorage(cfg.Storage.Path)
	case "s3":
		return cache.NewS3Storage(cfg.Storage.Endpoint, cfg.Storage.Bucket, cfg.Storage.Region,
			cfg.Storage.AccessKey, cfg.Storage.SecretKey)
	default:
		return nil, errors.New("unsupported distribution storage")
	}
}
