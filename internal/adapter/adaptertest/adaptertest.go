// Package adaptertest holds fixtures shared by package-adapter tests. Only
// tests import it; the depsilo binary does not link it.
//
// It imports no package adapter, so an adapter's in-package tests can use it.
// It does import quarantine, which imports the top-level adapter package, so
// tests of that package use it from the external adapter_test package.
package adaptertest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
	"depsilo/internal/upstream"
)

// NewManager returns a cache manager over a freshly migrated SQLite database
// and local storage under t's temporary directory, together with that
// database and storage. The manager is closed when t ends.
func NewManager(t testing.TB, immutableThreshold time.Duration) (*cache.Manager, *gorm.DB, *cache.LocalStorage) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "adapter.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), immutableThreshold)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	return manager, database, storage
}

// UnavailableSelector is an upstream.Selector that never yields an Upstream.
type UnavailableSelector struct{}

// Select always fails.
func (UnavailableSelector) Select(context.Context) (*upstream.Upstream, error) {
	return nil, errors.New("test upstream unavailable")
}

// AuditCapture records every audit entry logged to it.
type AuditCapture struct {
	Entries []db.AuditLog
}

// Log records entry.
func (capture *AuditCapture) Log(entry db.AuditLog) {
	capture.Entries = append(capture.Entries, entry)
}

// Provenance says how a provenance environment treats an ecosystem's release
// timestamp.
type Provenance int

const (
	// BoundProvenance reports the ecosystem as having an end-to-end
	// artifact-source and timestamp binding.
	BoundProvenance Provenance = iota
	// ApproximateProvenance lists the ecosystem as an approximate source.
	ApproximateProvenance
)

// ProvenanceEnv is what a provenance test builds its adapter handler from:
// a cache manager, a passive selector over one Upstream, the database, and a
// checker enforcing a 168h minimum release age for one ecosystem.
type ProvenanceEnv struct {
	Manager  *cache.Manager
	Selector upstream.Selector
	DB       *gorm.DB
	Checker  *quarantine.Checker
}

// NewProvenanceEnv returns a ProvenanceEnv whose selector targets upstreamURL
// and whose checker gates ecosystem with the given provenance.
func NewProvenanceEnv(t testing.TB, upstreamURL, ecosystem string, provenance Provenance) ProvenanceEnv {
	t.Helper()
	manager, database, _ := NewManager(t, 72*time.Hour)
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamURL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}

	enabled := true
	policyConfig := quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{ecosystem: "168h"},
	}
	var bound func(string) bool
	switch provenance {
	case BoundProvenance:
		bound = func(candidate string) bool { return candidate == ecosystem }
	case ApproximateProvenance:
		policyConfig.ApproximateSources = []string{ecosystem}
	}
	policy, err := quarantine.NewPolicyWithProvenance(policyConfig, bound)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return ProvenanceEnv{
		Manager:  manager,
		Selector: upstream.NewPassiveRecoverySelector(pool),
		DB:       database,
		Checker:  checker,
	}
}
