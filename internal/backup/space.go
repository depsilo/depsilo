package backup

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const restoreSafetyMargin = 64 << 20

type restoreVolumeNeed struct {
	path      string
	required  uint64
	available uint64
}

func preflightRestoreSpace(document manifest, targets Paths) error {
	var configSize, databaseSize uint64
	for _, file := range document.Files {
		switch file.Name {
		case "config.toml":
			configSize = uint64(file.Size)
		case "depsilo.db":
			databaseSize = uint64(file.Size)
		}
	}
	needs := make(map[string]*restoreVolumeNeed)
	add := func(path string, amount uint64) error {
		key, available, err := filesystemFreeSpace(path)
		if err != nil {
			return fmt.Errorf("check free space near %q: %w", path, err)
		}
		need := needs[key]
		if need == nil {
			need = &restoreVolumeNeed{path: path, available: available}
			needs[key] = need
		}
		need.required = saturatingAdd(need.required, amount)
		return nil
	}
	// Extraction and target-local candidates coexist until publication.
	if err := add(os.TempDir(), saturatingAdd(configSize, databaseSize)); err != nil {
		return err
	}
	configNeed := saturatingAdd(configSize, existingRegularSize(targets.Config))
	if err := add(filepath.Dir(targets.Config), configNeed); err != nil {
		return err
	}
	databaseNeed := saturatingAdd(databaseSize, existingRegularSize(targets.Database))
	databaseNeed = saturatingAdd(databaseNeed, existingRegularSize(targets.Database+"-wal"))
	databaseNeed = saturatingAdd(databaseNeed, existingRegularSize(targets.Database+"-shm"))
	if err := add(filepath.Dir(targets.Database), databaseNeed); err != nil {
		return err
	}
	for _, need := range needs {
		margin := uint64(restoreSafetyMargin)
		if percent := need.required / 20; percent > margin {
			margin = percent
		}
		withMargin := saturatingAdd(need.required, margin)
		if need.available < withMargin {
			return fmt.Errorf("insufficient free space near %q: restore needs %d bytes plus safety margin, only %d bytes are available", need.path, need.required, need.available)
		}
	}
	return nil
}

func existingRegularSize(path string) uint64 {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return 0
	}
	return uint64(info.Size())
}

func saturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}

func nearestExistingDirectory(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	current := filepath.Clean(abs)
	for {
		info, err := os.Stat(current)
		if err == nil {
			if info.IsDir() {
				return current, nil
			}
			return filepath.Dir(current), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		current = parent
	}
}
