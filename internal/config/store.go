package config

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"sync"
	"time"

	"go.uber.org/zap"
)

type StoreErrorCode string

const (
	StoreInvalidSetting    StoreErrorCode = "INVALID_SETTING"
	StoreConfigReadOnly    StoreErrorCode = "CONFIG_READ_ONLY"
	StoreConfigReadFailed  StoreErrorCode = "CONFIG_READ_FAILED"
	StoreConfigWriteFailed StoreErrorCode = "CONFIG_WRITE_FAILED"
)

type StoreError struct {
	Code StoreErrorCode
	Err  error
}

func (e *StoreError) Error() string { return e.Err.Error() }
func (e *StoreError) Unwrap() error { return e.Err }

type Store struct {
	mu        sync.Mutex
	path      string
	effective SettingsSnapshot
	overrides map[SettingPath]string
	logLevel  zap.AtomicLevel
	writer    atomicFileWriter
}

var settingEnvNames = map[SettingPath]string{
	SettingServerHost:        "DEPSILO_SERVER_HOST",
	SettingServerPort:        "DEPSILO_SERVER_PORT",
	SettingServerLogLevel:    "DEPSILO_SERVER_LOG_LEVEL",
	SettingDatabaseDriver:    "DEPSILO_DATABASE_DRIVER",
	SettingStorageType:       "DEPSILO_STORAGE_TYPE",
	SettingStoragePath:       "DEPSILO_STORAGE_PATH",
	SettingCacheMaxSizeGB:    "DEPSILO_CACHE_MAX_SIZE_GB",
	SettingCacheTTLIndex:     "DEPSILO_CACHE_TTL_INDEX",
	SettingCacheTTLBlob:      "DEPSILO_CACHE_TTL_BLOB",
	SettingCacheLRUThreshold: "DEPSILO_CACHE_LRU_THRESHOLD",
	SettingAuthTokenTTL:      "DEPSILO_AUTH_TOKEN_TTL",
}

func NewStore(path string, effective *Config, level zap.AtomicLevel) *Store {
	return newStore(path, effective, level, osAtomicFileWriter{})
}

func newStore(path string, effective *Config, level zap.AtomicLevel, writer atomicFileWriter) *Store {
	overrides := make(map[SettingPath]string)
	for _, settingPath := range allSettingPaths {
		name := settingEnvNames[settingPath]
		if name != "" && os.Getenv(name) != "" {
			overrides[settingPath] = name
		}
	}
	return &Store{
		path:      path,
		effective: SettingsSnapshotFromConfig(effective),
		overrides: overrides,
		logLevel:  level,
		writer:    writer,
	}
}

func (s *Store) Snapshot(ctx context.Context) (SettingsState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return SettingsState{}, err
	}
	configured, explicit, _, _, err := s.readCurrent()
	if err != nil {
		return SettingsState{}, &StoreError{Code: StoreConfigReadFailed, Err: err}
	}
	return s.state(configured, explicit), nil
}

func (s *Store) Update(ctx context.Context, patch SettingsPatch) (SettingsUpdateResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return SettingsUpdateResult{}, err
	}
	if patch.empty() {
		return SettingsUpdateResult{}, &StoreError{Code: StoreInvalidSetting, Err: fmt.Errorf("settings patch is empty")}
	}

	configured, explicit, document, mode, err := s.readCurrent()
	if err != nil {
		return SettingsUpdateResult{}, &StoreError{Code: StoreConfigReadFailed, Err: err}
	}

	changedEntries := make([]settingPatchEntry, 0, len(patch.entries()))
	for _, entry := range patch.entries() {
		if explicit[entry.path] && settingEntryEqual(configured, entry) {
			continue
		}
		changedEntries = append(changedEntries, entry)
	}

	if len(changedEntries) == 0 {
		return emptyUpdateResult(s.state(configured, explicit)), nil
	}

	candidate := configured
	applySettingEntries(&candidate, changedEntries)
	if err := ValidateSettingsSnapshot(candidate); err != nil {
		return SettingsUpdateResult{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}

	updatedDocument, updatedExplicit, err := patchSettingsDocument(document, patchFromEntries(changedEntries))
	if err != nil {
		return SettingsUpdateResult{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	updatedConfig, err := decodeConfigDocument(updatedDocument)
	if err != nil {
		return SettingsUpdateResult{}, &StoreError{Code: StoreInvalidSetting, Err: err}
	}
	updatedConfigured := SettingsSnapshotFromConfig(updatedConfig)

	if !configWritable(s.path) {
		return SettingsUpdateResult{}, &StoreError{Code: StoreConfigReadOnly, Err: fmt.Errorf("config file %q is read-only", s.path)}
	}

	outcome, writeErr := s.writer.Write(s.path, updatedDocument, mode)
	if !outcome.committed {
		if writeErr == nil {
			writeErr = fmt.Errorf("config writer did not commit the update")
		}
		return SettingsUpdateResult{}, &StoreError{Code: StoreConfigWriteFailed, Err: writeErr}
	}
	if writeErr != nil {
		zap.L().Warn("config writer returned an error after commit", zap.String("path", s.path), zap.Error(writeErr))
	}
	if outcome.durabilityErr != nil {
		zap.L().Warn("config rename committed but directory sync failed", zap.String("path", s.path), zap.Error(outcome.durabilityErr))
	}

	changed := entryPaths(changedEntries)
	if containsPath(changed, SettingServerLogLevel) && s.overrides[SettingServerLogLevel] == "" {
		parsed, parseErr := zap.ParseAtomicLevel(updatedConfigured.Server.LogLevel)
		if parseErr != nil {
			return SettingsUpdateResult{}, &StoreError{Code: StoreInvalidSetting, Err: parseErr}
		}
		s.logLevel.SetLevel(parsed.Level())
		s.effective.Server.LogLevel = updatedConfigured.Server.LogLevel
	}

	result := SettingsUpdateResult{
		SettingsState:     s.state(updatedConfigured, updatedExplicit),
		Changed:           changed,
		AppliedNow:        make([]SettingPath, 0),
		RestartRequired:   make([]SettingPath, 0),
		BlockedByOverride: make([]SettingPath, 0),
	}
	for _, settingPath := range changed {
		if s.overrides[settingPath] != "" {
			result.BlockedByOverride = append(result.BlockedByOverride, settingPath)
		} else if settingPath == SettingServerLogLevel {
			result.AppliedNow = append(result.AppliedNow, settingPath)
		} else {
			result.RestartRequired = append(result.RestartRequired, settingPath)
		}
	}
	return result, nil
}

func (s *Store) readCurrent() (SettingsSnapshot, map[SettingPath]bool, []byte, fs.FileMode, error) {
	document, err := os.ReadFile(s.path)
	mode := fs.FileMode(0o644)
	if err != nil {
		if !os.IsNotExist(err) {
			return SettingsSnapshot{}, nil, nil, 0, err
		}
		document = []byte{}
	} else {
		info, statErr := os.Stat(s.path)
		if statErr != nil {
			return SettingsSnapshot{}, nil, nil, 0, statErr
		}
		mode = info.Mode().Perm()
	}
	cfg, err := decodeConfigDocument(document)
	if err != nil {
		return SettingsSnapshot{}, nil, nil, 0, err
	}
	index, err := indexSettingsDocument(document)
	if err != nil {
		return SettingsSnapshot{}, nil, nil, 0, err
	}
	return SettingsSnapshotFromConfig(cfg), index.explicit, document, mode, nil
}

func (s *Store) state(configured SettingsSnapshot, explicit map[SettingPath]bool) SettingsState {
	sources := make(map[SettingPath]SettingSource, len(allSettingPaths))
	overrides := make(map[SettingPath]string, len(s.overrides))
	for _, settingPath := range allSettingPaths {
		if name := s.overrides[settingPath]; name != "" {
			sources[settingPath] = SettingSourceEnv
			overrides[settingPath] = name
		} else if explicit[settingPath] {
			sources[settingPath] = SettingSourceFile
		} else {
			sources[settingPath] = SettingSourceDefault
		}
	}
	pending := make([]SettingPath, 0)
	for _, settingPath := range restartSettingPaths {
		if s.overrides[settingPath] == "" && !settingValuesEqual(configured, s.effective, settingPath) {
			pending = append(pending, settingPath)
		}
	}
	return SettingsState{
		Configured:     configured,
		Effective:      s.effective,
		PendingRestart: pending,
		Overrides:      overrides,
		Sources:        sources,
		Editable:       clonePaths(editableSettingPaths),
		ConfigWritable: configWritable(s.path),
	}
}

func emptyUpdateResult(state SettingsState) SettingsUpdateResult {
	return SettingsUpdateResult{
		SettingsState:     state,
		Changed:           make([]SettingPath, 0),
		AppliedNow:        make([]SettingPath, 0),
		RestartRequired:   make([]SettingPath, 0),
		BlockedByOverride: make([]SettingPath, 0),
	}
}

func applySettingEntries(snapshot *SettingsSnapshot, entries []settingPatchEntry) {
	for _, entry := range entries {
		switch entry.path {
		case SettingServerLogLevel:
			snapshot.Server.LogLevel = entry.value.(string)
		case SettingCacheMaxSizeGB:
			snapshot.Cache.MaxSizeGB = entry.value.(int)
		case SettingCacheTTLIndex:
			snapshot.Cache.TTLIndex = entry.value.(string)
		case SettingCacheTTLBlob:
			snapshot.Cache.TTLBlob = entry.value.(string)
		case SettingCacheLRUThreshold:
			snapshot.Cache.LRUThreshold = entry.value.(int)
		case SettingAuthTokenTTL:
			snapshot.Auth.TokenTTL = entry.value.(string)
		}
	}
}

func settingEntryEqual(snapshot SettingsSnapshot, entry settingPatchEntry) bool {
	candidate := snapshot
	applySettingEntries(&candidate, []settingPatchEntry{entry})
	return settingValuesEqual(snapshot, candidate, entry.path)
}

func settingValuesEqual(left, right SettingsSnapshot, path SettingPath) bool {
	switch path {
	case SettingServerHost:
		return left.Server.Host == right.Server.Host
	case SettingServerPort:
		return left.Server.Port == right.Server.Port
	case SettingServerLogLevel:
		return left.Server.LogLevel == right.Server.LogLevel
	case SettingDatabaseDriver:
		return left.Database.Driver == right.Database.Driver
	case SettingStorageType:
		return left.Storage.Type == right.Storage.Type
	case SettingStoragePath:
		return left.Storage.Path == right.Storage.Path
	case SettingCacheMaxSizeGB:
		return left.Cache.MaxSizeGB == right.Cache.MaxSizeGB
	case SettingCacheTTLIndex:
		return durationValuesEqual(left.Cache.TTLIndex, right.Cache.TTLIndex)
	case SettingCacheTTLBlob:
		return durationValuesEqual(left.Cache.TTLBlob, right.Cache.TTLBlob)
	case SettingCacheLRUThreshold:
		return left.Cache.LRUThreshold == right.Cache.LRUThreshold
	case SettingAuthTokenTTL:
		return durationValuesEqual(left.Auth.TokenTTL, right.Auth.TokenTTL)
	default:
		return reflect.DeepEqual(left, right)
	}
}

func durationValuesEqual(left, right string) bool {
	leftDuration, leftErr := time.ParseDuration(left)
	rightDuration, rightErr := time.ParseDuration(right)
	return leftErr == nil && rightErr == nil && leftDuration == rightDuration
}

func entryPaths(entries []settingPatchEntry) []SettingPath {
	paths := make([]SettingPath, len(entries))
	for i, entry := range entries {
		paths[i] = entry.path
	}
	return paths
}

func containsPath(paths []SettingPath, want SettingPath) bool {
	for _, settingPath := range paths {
		if settingPath == want {
			return true
		}
	}
	return false
}
