package server

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/db"
	"go.uber.org/zap"
)

func TestStartServerReclaimsRetiredHuggingFaceEntriesBeforeServing(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "server.db")
	storagePath := filepath.Join(directory, "cache")
	configPath := filepath.Join(directory, "config.toml")
	configDocument := fmt.Sprintf(`
[server]
host = "127.0.0.1"
port = 0
log_level = "error"

[database]
driver = "sqlite"
dsn = %q

[storage]
type = "local"
path = %q

[access_log]
rollup_enabled = false
backfill_on_start = false
retention_days = 0
rollup_retention_days = 0

[upstream_updates]
enabled = false

[security]
enabled = false

[supply_chain.blocklist]
enabled = false

[supply_chain.tamper_detection]
enabled = false

[auth]
enabled = true
jwt_secret = "integration-test-secret-with-sufficient-length"
token_ttl = "1h"
`, databasePath, storagePath)
	if err := os.WriteFile(configPath, []byte(configDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEPSILO_CONFIG", configPath)
	t.Setenv("DEPSILO_ADMIN_PASSWORD", "integration-Test-password-123")

	seedDatabase, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(seedDatabase); err != nil {
		t.Fatal(err)
	}
	if err := seedDatabase.Exec("DELETE FROM schema_migrations WHERE version = ?", 3).Error; err != nil {
		t.Fatalf("rewind migration ledger to schema v2: %v", err)
	}
	storage, err := cache.NewLocalStorage(storagePath)
	if err != nil {
		t.Fatal(err)
	}
	const legacyKey = "huggingface/openai/whisper-tiny/resolve/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/config.json"
	payload := []byte("legacy Hugging Face object")
	if err := storage.Put(context.Background(), legacyKey, bytes.NewReader(payload), int64(len(payload)), "application/octet-stream"); err != nil {
		t.Fatalf("store v2 object: %v", err)
	}
	legacy := db.CacheEntry{
		Key:          legacyKey,
		AdapterType:  "huggingface",
		CacheKind:    db.CacheKindArtifact,
		PackageName:  "openai/whisper-tiny",
		StoragePath:  legacyKey,
		Size:         int64(len(payload)),
		ContentType:  "application/octet-stream",
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
		LastAccessed: time.Now().UTC(),
	}
	if err := seedDatabase.Create(&legacy).Error; err != nil {
		t.Fatalf("seed v2 cache row: %v", err)
	}
	seedSQL, err := seedDatabase.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := seedSQL.Close(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	server, err := StartServer(ctx, zap.NewAtomicLevel())
	if err != nil {
		cancel()
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := Shutdown(shutdownCtx, server); err != nil {
			t.Errorf("shutdown server: %v", err)
		}
	})

	readDatabase, err := db.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	readSQL, err := readDatabase.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = readSQL.Close() })
	var remaining int64
	if err := readDatabase.Model(&db.CacheEntry{}).Where("id = ?", legacy.ID).Count(&remaining).Error; err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("server returned before retiring cache row was reclaimed: %d rows remain", remaining)
	}
	if exists, err := storage.Exists(context.Background(), legacy.StoragePath); err != nil || exists {
		t.Fatalf("server returned before retiring object was reclaimed: (%v, %v)", exists, err)
	}
}
