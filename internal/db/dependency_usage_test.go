package db

import (
	"context"
	"testing"
	"time"
)

func TestDependencyUsageFreshAndLegacyInitialization(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "fresh"
		if legacy {
			name = "legacy-empty-history"
		}
		t.Run(name, func(t *testing.T) {
			database := openCompileCacheMigrationTestDB(t)
			if legacy {
				if err := database.AutoMigrate(&AccessLog{}); err != nil {
					t.Fatal(err)
				}
			}
			if err := AutoMigrate(database); err != nil {
				t.Fatal(err)
			}
			want := DependencyNeverUsed
			if legacy {
				want = DependencyUnknown
			}
			for range 2 {
				usage, err := ReadDependencyUsage(context.Background(), database)
				if err != nil || usage.State != want || usage.FirstSuccessfulRequestAt != nil {
					t.Fatalf("usage = %+v, %v; want %s and no fabricated first request", usage, err, want)
				}
				if err := AutoMigrate(database); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestDependencyUsageDirtyRestartAndCleanClose(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	usage, err := BeginDependencyUsage(ctx, database)
	if err != nil || usage.State != DependencyNeverUsed {
		t.Fatalf("first begin = %+v, %v", usage, err)
	}
	if disk, err := ReadDependencyUsage(ctx, database); err != nil || disk.State != DependencyUnknown {
		t.Fatalf("disk must not claim no activity while a writer is active: %+v, %v", disk, err)
	}
	if err := SaveDependencyUsage(ctx, database, usage, true); err != nil {
		t.Fatal(err)
	}
	usage, err = BeginDependencyUsage(ctx, database)
	if err != nil || usage.State != DependencyNeverUsed {
		t.Fatalf("clean restart = %+v, %v", usage, err)
	}
	// Simulate termination without a clean close. Even if a successful
	// download was still in memory, its absence can no longer be asserted.
	usage, err = BeginDependencyUsage(ctx, database)
	if err != nil || usage.State != DependencyUnknown {
		t.Fatalf("dirty restart = %+v, %v", usage, err)
	}
}

func TestDependencyUsageSurvivesRetentionAndKeepsPositiveEvidence(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := BeginDependencyUsage(ctx, database); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	usage := DependencyUsage{State: DependencyUsed, FirstSuccessfulRequestAt: &at}
	if err := SaveDependencyUsage(ctx, database, usage, false); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"access_logs", "access_log_hourlies", "access_log_dailies", "cache_entries"} {
		if database.Migrator().HasTable(table) {
			if err := database.Exec("DELETE FROM " + table).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := SaveDependencyUsage(ctx, database, DependencyUsage{State: DependencyUnknown}, true); err != nil {
		t.Fatal(err)
	}
	got, err := BeginDependencyUsage(ctx, database)
	if err != nil || got.State != DependencyUsed || got.FirstSuccessfulRequestAt == nil || !got.FirstSuccessfulRequestAt.Equal(at) {
		t.Fatalf("usage lost across cleanup/restart: %+v, %v", got, err)
	}
}

func TestDependencyUsageDirtyRestartKeepsUseButRevokesExactFirstTime(t *testing.T) {
	database := openCompileCacheMigrationTestDB(t)
	if err := AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := BeginDependencyUsage(ctx, database); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	if err := SaveDependencyUsage(ctx, database, DependencyUsage{State: DependencyUsed, FirstSuccessfulRequestAt: &at}, false); err != nil {
		t.Fatal(err)
	}
	// An earlier unknown or completed observation may have been in memory
	// when the process stopped. Positive use is durable; exact order is not.
	usage, err := BeginDependencyUsage(ctx, database)
	if err != nil || usage.State != DependencyUsed || usage.FirstSuccessfulRequestAt != nil {
		t.Fatalf("dirty restart kept an unproven first time: %+v, %v", usage, err)
	}
}
