package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DistributionConfig holds retained dependency content separately from both
// evictable caches. It is opt-in; disabling it never removes retained content.
type DistributionConfig struct {
	Enabled        bool          `mapstructure:"enabled"`
	MaxSizeBytes   int64         `mapstructure:"max_size_bytes"`
	MaxObjectBytes int64         `mapstructure:"max_object_bytes"`
	Storage        StorageConfig `mapstructure:"storage"`
	// TransferSigningKeyFile signs exported transfer bundles. It is a dedicated
	// PKCS#8 Ed25519 key, separate from SBOM, JWT and URL-signing keys.
	TransferSigningKeyFile string `mapstructure:"transfer_signing_key_file"`
	// TrustedTransferKeys are the only signers whose bundles may be imported.
	// They are restart-managed; the distribution module parses them.
	TrustedTransferKeys []string `mapstructure:"trusted_transfer_keys"`
}

// ValidateDistributionConfig is shared by config loading and storage creation.
// It performs no writes. Ordinary local and S3 TotalSize implementations cover
// their entire root/bucket, so a mere object-key prefix is not isolated storage.
func ValidateDistributionConfig(cfg DistributionConfig, packageStorage, compileStorage StorageConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.MaxSizeBytes <= 0 {
		return errors.New("distribution.max_size_bytes must be greater than zero")
	}
	if cfg.MaxObjectBytes <= 0 || cfg.MaxObjectBytes > cfg.MaxSizeBytes {
		return errors.New("distribution.max_object_bytes must be positive and no greater than max_size_bytes")
	}
	// Stream verification probes one byte beyond the declared length. Preserve
	// that byte in int64 arithmetic even with an operator-supplied upper limit.
	if cfg.MaxObjectBytes == math.MaxInt64 {
		return errors.New("distribution.max_object_bytes must be less than the maximum int64 value")
	}
	switch cfg.Storage.Type {
	case "local":
		root, err := distributionLocalRoot(cfg.Storage.Path)
		if err != nil {
			return fmt.Errorf("distribution.storage.path: %w", err)
		}
		if err := requireDedicatedDistributionRoot(root); err != nil {
			return err
		}
		for _, other := range []struct {
			name    string
			storage StorageConfig
		}{{"storage.path", packageStorage}, {"compile_cache.storage.path", compileStorage}} {
			if other.storage.Type != "local" || other.storage.Path == "" {
				continue
			}
			otherRoot, err := distributionLocalRoot(other.storage.Path)
			if err != nil {
				return fmt.Errorf("resolve %s for distribution isolation: %w", other.name, err)
			}
			if containsLocalPath(root, otherRoot) || containsLocalPath(otherRoot, root) {
				return fmt.Errorf("distribution.storage.path must not equal, contain, or be inside %s", other.name)
			}
		}
	case "s3":
		if cfg.Storage.Bucket == "" || cfg.Storage.Bucket != strings.TrimSpace(cfg.Storage.Bucket) {
			return errors.New("distribution.storage.bucket must be explicitly set without surrounding whitespace")
		}
		if err := validateDistributionS3Endpoint(cfg.Storage.Endpoint); err != nil {
			return fmt.Errorf("distribution.storage.endpoint: %w", err)
		}
		for _, other := range []StorageConfig{packageStorage, compileStorage} {
			// Regional endpoints, DNS aliases and proxies may reach the same
			// bucket. A different endpoint string cannot prove isolation.
			if other.Type == "s3" && other.Bucket == cfg.Storage.Bucket {
				return errors.New("distribution storage requires a differently named S3 bucket from package and compiler caches, regardless of endpoint")
			}
		}
	default:
		return errors.New("distribution.storage.type must be local or s3")
	}
	return nil
}

// Resolve the nearest existing ancestor before appending missing components.
// This catches roots nested through a symlink even before the new root exists.
func distributionLocalRoot(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("must not be empty")
	}
	absolute, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	ancestor := filepath.Clean(absolute)
	var suffix []string
	for {
		info, statErr := os.Lstat(ancestor)
		if statErr == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				info, err = os.Stat(resolved)
				if err != nil {
					return "", err
				}
			}
			if !info.IsDir() {
				return "", errors.New("existing storage ancestor is not a directory")
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return resolved, nil
		}
		if !os.IsNotExist(statErr) {
			return "", statErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", statErr
		}
		suffix = append(suffix, filepath.Base(ancestor))
		ancestor = parent
	}
}

func containsLocalPath(parent, child string) bool {
	// Be conservative for platforms whose usual filesystems fold path case.
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		parent, child = strings.ToLower(parent), strings.ToLower(child)
	}
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func requireDedicatedDistributionRoot(root string) error {
	if filepath.Dir(root) == root {
		return errors.New("distribution.storage.path must be a dedicated directory, not a filesystem root")
	}
	protected := []string{os.TempDir()}
	if directory, err := os.Getwd(); err == nil {
		protected = append(protected, directory)
	}
	if directory, err := os.UserHomeDir(); err == nil {
		protected = append(protected, directory)
	}
	for _, path := range protected {
		resolved, err := distributionLocalRoot(path)
		if err != nil {
			return err
		}
		if containsLocalPath(root, resolved) {
			return errors.New("distribution.storage.path must not contain the working, home, or temporary directory")
		}
	}
	return nil
}

func validateDistributionS3Endpoint(raw string) error {
	if raw == "" {
		return nil
	}
	value, err := url.Parse(raw)
	if err != nil || (value.Scheme != "http" && value.Scheme != "https") || value.Hostname() == "" ||
		value.User != nil || value.RawQuery != "" || value.Fragment != "" {
		return errors.New("must be an HTTP(S) endpoint without credentials, query, or fragment")
	}
	return nil
}
