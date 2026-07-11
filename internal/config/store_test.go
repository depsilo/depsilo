package config

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const storeConfigFixture = `# operator header
[server]
host = "127.0.0.1"
port = 23333
log_level = "info" # keep level comment

[database]
driver = "sqlite"
dsn = "./data/depsilo.db"

[storage]
type = "local"
path = "./data/cache"

[cache]
max_size_gb = 20
ttl_index = "5m"
ttl_blob = "72h"
lru_threshold = 90

[auth]
enabled = true
jwt_secret = "test-secret"
token_ttl = "168h"

[custom]
untouched = "preserve me" # keep custom comment
`

var settingsEnvironmentNames = []string{
	"DEPSILO_SERVER_HOST", "DEPSILO_SERVER_PORT", "DEPSILO_SERVER_LOG_LEVEL",
	"DEPSILO_DATABASE_DRIVER", "DEPSILO_STORAGE_TYPE", "DEPSILO_STORAGE_PATH",
	"DEPSILO_CACHE_MAX_SIZE_GB", "DEPSILO_CACHE_TTL_INDEX", "DEPSILO_CACHE_TTL_BLOB",
	"DEPSILO_CACHE_LRU_THRESHOLD", "DEPSILO_AUTH_TOKEN_TTL",
}

func clearSettingsEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range settingsEnvironmentNames {
		t.Setenv(name, "")
	}
}

func writeStoreFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(storeConfigFixture), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadStoreFixture(t *testing.T, path string) (*Config, zap.AtomicLevel) {
	t.Helper()
	t.Setenv("DEPSILO_CONFIG", path)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := zap.ParseAtomicLevel(cfg.Server.LogLevel)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, parsed
}

func newStoreFixture(t *testing.T) (string, *Store, zap.AtomicLevel) {
	t.Helper()
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	cfg, level := loadStoreFixture(t, path)
	return path, NewStore(path, cfg, level), level
}

func assertPaths(t *testing.T, got, want []SettingPath) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
}

func assertStoreCode(t *testing.T, err error, want StoreErrorCode) {
	t.Helper()
	var storeErr *StoreError
	if !errors.As(err, &storeErr) || storeErr.Code != want {
		t.Fatalf("error = %v, want code %s", err, want)
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write(string, []byte, fs.FileMode) (atomicWriteOutcome, error) {
	return atomicWriteOutcome{}, w.err
}

type uncommittedNilErrorWriter struct{}

func (uncommittedNilErrorWriter) Write(string, []byte, fs.FileMode) (atomicWriteOutcome, error) {
	return atomicWriteOutcome{committed: false}, nil
}

type committedErrorWriter struct{ err error }

func (w committedErrorWriter) Write(path string, data []byte, mode fs.FileMode) (atomicWriteOutcome, error) {
	if err := os.WriteFile(path, data, mode.Perm()); err != nil {
		return atomicWriteOutcome{}, err
	}
	return atomicWriteOutcome{committed: true}, w.err
}

func TestStoreUpdatePersistsClassifiesAndPreservesComments(t *testing.T) {
	path, store, level := newStoreFixture(t)
	logLevel, blobTTL := "debug", "96h"
	result, err := store.Update(context.Background(), SettingsPatch{
		Server: &SettingsServerPatch{LogLevel: &logLevel},
		Cache:  &SettingsCachePatch{TTLBlob: &blobTTL},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertPaths(t, result.Changed, []SettingPath{SettingServerLogLevel, SettingCacheTTLBlob})
	assertPaths(t, result.AppliedNow, []SettingPath{SettingServerLogLevel})
	assertPaths(t, result.RestartRequired, []SettingPath{SettingCacheTTLBlob})
	assertPaths(t, result.BlockedByOverride, []SettingPath{})
	assertPaths(t, result.PendingRestart, []SettingPath{SettingCacheTTLBlob})
	if result.Configured.Cache.TTLBlob != "96h" || result.Effective.Cache.TTLBlob != "72h" || result.Effective.Server.LogLevel != "debug" {
		t.Fatalf("result = %+v", result)
	}
	if level.Level() != zap.DebugLevel {
		t.Fatalf("atomic level = %s", level.Level())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`untouched = "preserve me" # keep custom comment`)) || !bytes.Contains(data, []byte(`log_level = "debug" # keep level comment`)) {
		t.Fatalf("comments changed:\n%s", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
}

func TestStoreUpdateRejectsInvalidPatchWithoutWriting(t *testing.T) {
	tests := []struct {
		name  string
		patch func() SettingsPatch
	}{
		{"max size", func() SettingsPatch { value := 0; return SettingsPatch{Cache: &SettingsCachePatch{MaxSizeGB: &value}} }},
		{"index ttl", func() SettingsPatch {
			value := "bad"
			return SettingsPatch{Cache: &SettingsCachePatch{TTLIndex: &value}}
		}},
		{"lru", func() SettingsPatch {
			value := 101
			return SettingsPatch{Cache: &SettingsCachePatch{LRUThreshold: &value}}
		}},
		{"token never", func() SettingsPatch {
			value := "never"
			return SettingsPatch{Auth: &SettingsAuthPatch{TokenTTL: &value}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, store, level := newStoreFixture(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.Update(context.Background(), tt.patch())
			assertStoreCode(t, err, StoreInvalidSetting)
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("invalid patch changed disk:\n%s", after)
			}
			if level.Level() != zap.InfoLevel {
				t.Fatalf("level changed to %s", level.Level())
			}
		})
	}
}

func TestStoreUpdateReadOnlyFile(t *testing.T) {
	path, store, _ := newStoreFixture(t)
	before, _ := os.ReadFile(path)
	if err := os.Chmod(path, 0o440); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o640) })
	value := "24h"
	_, err := store.Update(context.Background(), SettingsPatch{Auth: &SettingsAuthPatch{TokenTTL: &value}})
	assertStoreCode(t, err, StoreConfigReadOnly)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) {
		t.Fatal("read-only update changed disk")
	}
}

func TestStoreUpdatePreRenameFailureLeavesFileAndEffectiveUntouched(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	cfg, level := loadStoreFixture(t, path)
	store := newStore(path, cfg, level, failingWriter{err: errors.New("injected pre-rename failure")})
	before, _ := os.ReadFile(path)
	value := "debug"
	_, err := store.Update(context.Background(), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &value}})
	assertStoreCode(t, err, StoreConfigWriteFailed)
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, before) {
		t.Fatal("failed atomic write changed disk")
	}
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Effective.Server.LogLevel != "info" || level.Level() != zap.InfoLevel {
		t.Fatalf("effective/level changed: %+v %s", state.Effective, level.Level())
	}
}

func TestStoreUpdateUncommittedNilErrorFailsAndLeavesStateAligned(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	cfg, level := loadStoreFixture(t, path)
	store := newStore(path, cfg, level, uncommittedNilErrorWriter{})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	value := "debug"
	result, err := store.Update(context.Background(), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &value}})
	assertStoreCode(t, err, StoreConfigWriteFailed)
	if !strings.Contains(err.Error(), "did not commit") {
		t.Fatalf("error = %q, want synthesized uncommitted outcome detail", err)
	}
	if !reflect.DeepEqual(result, SettingsUpdateResult{}) {
		t.Fatalf("failed result = %+v, want zero value", result)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("uncommitted outcome changed disk")
	}
	state, snapshotErr := store.Snapshot(context.Background())
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	if state.Configured.Server.LogLevel != "info" || state.Effective.Server.LogLevel != "info" || level.Level() != zap.InfoLevel {
		t.Fatalf("disk/effective/level diverged: %+v %s", state, level.Level())
	}
}

func TestStoreUpdateCommittedErrorSucceedsWarnsAndAlignsState(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	cfg, level := loadStoreFixture(t, path)
	writerErr := errors.New("writer returned an error after commit")
	store := newStore(path, cfg, level, committedErrorWriter{err: writerErr})

	var logs bytes.Buffer
	encoderConfig := zap.NewDevelopmentEncoderConfig()
	logger := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), zapcore.AddSync(&logs), zap.WarnLevel))
	undoLogger := zap.ReplaceGlobals(logger)
	t.Cleanup(undoLogger)

	value := "debug"
	result, err := store.Update(context.Background(), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &value}})
	if err != nil {
		t.Fatalf("committed update reported failure: %v", err)
	}
	assertPaths(t, result.AppliedNow, []SettingPath{SettingServerLogLevel})
	if result.Configured.Server.LogLevel != "debug" || result.Effective.Server.LogLevel != "debug" || level.Level() != zap.DebugLevel {
		t.Fatalf("result/effective/level diverged: %+v %s", result, level.Level())
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Contains(data, []byte(`log_level = "debug"`)) {
		t.Fatalf("committed file not authoritative:\n%s", data)
	}
	state, snapshotErr := store.Snapshot(context.Background())
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	if state.Configured.Server.LogLevel != "debug" || state.Effective.Server.LogLevel != "debug" {
		t.Fatalf("snapshot diverged after committed error: %+v", state)
	}
	warning := logs.String()
	if !strings.Contains(warning, "config writer returned an error after commit") || !strings.Contains(warning, path) || !strings.Contains(warning, writerErr.Error()) {
		t.Fatalf("structured committed-error warning = %q", warning)
	}
}

func TestStoreUpdatePostRenameSyncFailureReturnsCommittedResultAndAlignsRuntime(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	cfg, level := loadStoreFixture(t, path)
	store := newStore(path, cfg, level, osAtomicFileWriter{
		syncDir: func(string) error { return errors.New("directory sync failed after rename") },
	})
	value := "debug"
	result, err := store.Update(context.Background(), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &value}})
	if err != nil {
		t.Fatalf("committed update reported failure: %v", err)
	}
	assertPaths(t, result.AppliedNow, []SettingPath{SettingServerLogLevel})
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Contains(data, []byte(`log_level = "debug"`)) {
		t.Fatalf("committed file not returned as success:\n%s", data)
	}
	state, snapshotErr := store.Snapshot(context.Background())
	if snapshotErr != nil {
		t.Fatal(snapshotErr)
	}
	if state.Effective.Server.LogLevel != "debug" || level.Level() != zap.DebugLevel {
		t.Fatalf("committed disk/runtime diverged: %+v %s", state.Effective, level.Level())
	}
}

func TestStoreConcurrentUpdatesMergeDistinctFields(t *testing.T) {
	path, store, _ := newStoreFixture(t)
	maxSize, tokenTTL := 40, "24h"
	patches := []SettingsPatch{{Cache: &SettingsCachePatch{MaxSizeGB: &maxSize}}, {Auth: &SettingsAuthPatch{TokenTTL: &tokenTTL}}}
	errs := make(chan error, len(patches))
	var wg sync.WaitGroup
	for _, patch := range patches {
		patch := patch
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.Update(context.Background(), patch); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeConfigDocument(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cache.MaxSizeGB != 40 || cfg.Auth.TokenTTL != 24*time.Hour {
		t.Fatalf("merged config = %+v %+v", cfg.Cache, cfg.Auth)
	}
}

func TestStoreEnvironmentOverrideIsBlockedButPersisted(t *testing.T) {
	clearSettingsEnvironment(t)
	path := writeStoreFixture(t)
	t.Setenv("DEPSILO_SERVER_LOG_LEVEL", "debug")
	cfg, level := loadStoreFixture(t, path)
	store := NewStore(path, cfg, level)
	value := "error"
	result, err := store.Update(context.Background(), SettingsPatch{Server: &SettingsServerPatch{LogLevel: &value}})
	if err != nil {
		t.Fatal(err)
	}
	assertPaths(t, result.Changed, []SettingPath{SettingServerLogLevel})
	assertPaths(t, result.AppliedNow, []SettingPath{})
	assertPaths(t, result.RestartRequired, []SettingPath{})
	assertPaths(t, result.BlockedByOverride, []SettingPath{SettingServerLogLevel})
	if result.Configured.Server.LogLevel != "error" || result.Effective.Server.LogLevel != "debug" || result.Sources[SettingServerLogLevel] != SettingSourceEnv || result.Overrides[SettingServerLogLevel] != "DEPSILO_SERVER_LOG_LEVEL" {
		t.Fatalf("result = %+v", result)
	}
	if level.Level() != zap.DebugLevel {
		t.Fatalf("override level changed to %s", level.Level())
	}
	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte(`log_level = "error"`)) {
		t.Fatalf("file not updated:\n%s", data)
	}
}

func TestStoreSnapshotSourcesCoverEveryPath(t *testing.T) {
	_, store, _ := newStoreFixture(t)
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if state.Sources == nil || state.Overrides == nil || state.PendingRestart == nil || state.Editable == nil {
		t.Fatalf("nil collection in %+v", state)
	}
	if len(state.Sources) != len(AllSettingPaths()) {
		t.Fatalf("sources = %#v", state.Sources)
	}
	for _, path := range AllSettingPaths() {
		if state.Sources[path] != SettingSourceFile {
			t.Fatalf("source[%s] = %s", path, state.Sources[path])
		}
	}
}

func TestStoreUpdatedFileLoadsAfterRestart(t *testing.T) {
	path, store, _ := newStoreFixture(t)
	value := "24h"
	if _, err := store.Update(context.Background(), SettingsPatch{Auth: &SettingsAuthPatch{TokenTTL: &value}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", path)
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Auth.TokenTTL != 24*time.Hour {
		t.Fatalf("reloaded token ttl = %s", reloaded.Auth.TokenTTL)
	}
}
