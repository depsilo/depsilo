package huggingface

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

var _ repositoryRevocationStorage = (*repositoryRevocationStore)(nil)

func TestRepositoryRevocationStoreBeginLoadsAndRotatesToken(t *testing.T) {
	database := openRepositoryRevocationTestDB(t)
	store := newRepositoryRevocationStore(database)
	tokens := []string{
		"11111111111111111111111111111111",
		"22222222222222222222222222222222",
	}
	store.newToken = tokenSequence(t, tokens...)

	first, err := store.Begin(
		context.Background(),
		"acme/model",
		"acme/model",
	)
	if err != nil {
		t.Fatal(err)
	}
	if first != tokens[0] {
		t.Fatalf("first token = %q, want %q", first, tokens[0])
	}

	markers, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 1 {
		t.Fatalf("loaded %d markers, want 1", len(markers))
	}
	if got := markers[0]; got != (repositoryRevocationMarker{
		Repository:  "acme/model",
		EscapedRepo: "acme/model",
		Token:       tokens[0],
		CleanupSafe: false,
	}) {
		t.Fatalf("first marker = %+v", got)
	}

	safe, err := store.MarkCleanupSafe(context.Background(), "acme/model", first)
	if err != nil {
		t.Fatal(err)
	}
	if !safe {
		t.Fatal("current token did not mark cleanup safe")
	}

	second, err := store.Begin(
		context.Background(),
		"acme/model",
		"acme/model-renamed",
	)
	if err != nil {
		t.Fatal(err)
	}
	if second != tokens[1] || second == first {
		t.Fatalf("rotated token = %q, want new token %q", second, tokens[1])
	}

	markers, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 1 {
		t.Fatalf("loaded %d markers after upsert, want 1", len(markers))
	}
	if got := markers[0]; got != (repositoryRevocationMarker{
		Repository:  "acme/model",
		EscapedRepo: "acme/model-renamed",
		Token:       tokens[1],
		CleanupSafe: false,
	}) {
		t.Fatalf("rotated marker = %+v", got)
	}
}

func TestRepositoryRevocationStoreTokenCASPreventsOldGenerationRestore(
	t *testing.T,
) {
	database := openRepositoryRevocationTestDB(t)
	store := newRepositoryRevocationStore(database)
	store.newToken = tokenSequence(
		t,
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	)

	oldToken, err := store.Begin(context.Background(), "datasets/acme/data", "datasets/acme/data")
	if err != nil {
		t.Fatal(err)
	}
	currentToken, err := store.Begin(
		context.Background(),
		"datasets/acme/data",
		"datasets/acme/data",
	)
	if err != nil {
		t.Fatal(err)
	}

	if updated, err := store.MarkCleanupSafe(
		context.Background(),
		"datasets/acme/data",
		oldToken,
	); err != nil || updated {
		t.Fatalf("old MarkCleanupSafe = (%v, %v), want (false, nil)", updated, err)
	}
	if deleted, err := store.DeleteCleanupSafe(
		context.Background(),
		"datasets/acme/data",
		oldToken,
	); err != nil || deleted {
		t.Fatalf("old DeleteCleanupSafe = (%v, %v), want (false, nil)", deleted, err)
	}
	if deleted, err := store.DeleteCleanupSafe(
		context.Background(),
		"datasets/acme/data",
		currentToken,
	); err != nil || deleted {
		t.Fatalf("unsafe DeleteCleanupSafe = (%v, %v), want (false, nil)", deleted, err)
	}

	if updated, err := store.MarkCleanupSafe(
		context.Background(),
		"datasets/acme/data",
		currentToken,
	); err != nil || !updated {
		t.Fatalf("current MarkCleanupSafe = (%v, %v), want (true, nil)", updated, err)
	}
	if deleted, err := store.DeleteCleanupSafe(
		context.Background(),
		"datasets/acme/data",
		currentToken,
	); err != nil || !deleted {
		t.Fatalf("current DeleteCleanupSafe = (%v, %v), want (true, nil)", deleted, err)
	}

	markers, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != 0 {
		t.Fatalf("markers after safe CAS delete = %+v, want none", markers)
	}
}

func TestRepositoryRevocationStoreConcurrentCleanupObserversBothConverge(
	t *testing.T,
) {
	database := openRepositoryRevocationTestDB(t)
	store := newRepositoryRevocationStore(database)
	store.newToken = tokenSequence(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	token, err := store.Begin(context.Background(), "acme/model", "acme/model")
	if err != nil {
		t.Fatal(err)
	}
	if updated, err := store.MarkCleanupSafe(
		context.Background(),
		"acme/model",
		token,
	); err != nil || !updated {
		t.Fatalf("MarkCleanupSafe = (%v, %v), want (true, nil)", updated, err)
	}

	start := make(chan struct{})
	results := make(chan struct {
		deleted bool
		err     error
	}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			deleted, deleteErr := store.DeleteCleanupSafe(
				context.Background(),
				"acme/model",
				token,
			)
			results <- struct {
				deleted bool
				err     error
			}{deleted: deleted, err: deleteErr}
		}()
	}
	close(start)

	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil || !result.deleted {
			t.Fatalf(
				"concurrent DeleteCleanupSafe = (%v, %v), want (true, nil)",
				result.deleted,
				result.err,
			)
		}
	}
}

func TestRepositoryRevocationStoreEnforcesAndDetectsCapacity(t *testing.T) {
	database := openRepositoryRevocationTestDB(t)
	records := make([]db.HuggingFaceRepositoryRevocation, 0, maxPersistedRepositoryRevocations+1)
	for i := 0; i < maxPersistedRepositoryRevocations; i++ {
		repository := fmt.Sprintf("acme/model-%04d", i)
		records = append(records, db.HuggingFaceRepositoryRevocation{
			Repository:  repository,
			EscapedRepo: repository,
			Token:       fmt.Sprintf("%032x", i+1),
		})
	}
	if err := database.CreateInBatches(records, 100).Error; err != nil {
		t.Fatal(err)
	}

	store := newRepositoryRevocationStore(database)
	store.newToken = tokenSequence(
		t,
		"cccccccccccccccccccccccccccccccc",
		"dddddddddddddddddddddddddddddddd",
	)
	markers, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(markers) != maxPersistedRepositoryRevocations {
		t.Fatalf("loaded %d markers, want %d", len(markers), maxPersistedRepositoryRevocations)
	}

	if _, err := store.Begin(
		context.Background(),
		"acme/model-0000",
		"acme/model-0000",
	); err != nil {
		t.Fatalf("rotate existing marker at capacity: %v", err)
	}
	if _, err := store.Begin(
		context.Background(),
		"acme/over-capacity",
		"acme/over-capacity",
	); !errors.Is(err, errRepositoryRevocationStoreCapacity) {
		t.Fatalf("new marker at capacity error = %v, want capacity error", err)
	}
	var count int64
	if err := database.Model(&db.HuggingFaceRepositoryRevocation{}).
		Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != maxPersistedRepositoryRevocations {
		t.Fatalf("row count after rejected Begin = %d, want %d", count, maxPersistedRepositoryRevocations)
	}

	overflow := db.HuggingFaceRepositoryRevocation{
		Repository:  "acme/external-overflow",
		EscapedRepo: "acme/external-overflow",
		Token:       "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	if err := database.Create(&overflow).Error; err != nil {
		t.Fatal(err)
	}
	markers, err = store.Load(context.Background())
	if !errors.Is(err, errRepositoryRevocationStoreCapacity) {
		t.Fatalf("Load overflow error = %v, want capacity error", err)
	}
	if markers != nil {
		t.Fatalf("Load returned %d partial markers on overflow", len(markers))
	}
}

func TestRepositoryRevocationStoreRejectsCorruptPersistedMarker(t *testing.T) {
	database := openRepositoryRevocationTestDB(t)
	if err := database.Create(&db.HuggingFaceRepositoryRevocation{
		Repository:  "acme/model",
		EscapedRepo: "acme/model",
		Token:       "not-a-valid-token",
	}).Error; err != nil {
		t.Fatal(err)
	}

	markers, err := newRepositoryRevocationStore(database).Load(context.Background())
	if !errors.Is(err, errRepositoryRevocationMarkerCorrupt) {
		t.Fatalf("Load corrupt marker error = %v, want corrupt-marker error", err)
	}
	if markers != nil {
		t.Fatalf("Load returned partial markers for corrupt persistence: %+v", markers)
	}
}

func TestRepositoryRevocationStoreErrorsFailClosed(t *testing.T) {
	var nilStore *repositoryRevocationStore
	if _, err := nilStore.Load(context.Background()); !errors.Is(
		err,
		errRepositoryRevocationStoreUnavailable,
	) {
		t.Fatalf("nil Load error = %v", err)
	}
	if _, err := nilStore.Begin(context.Background(), "acme/model", "acme/model"); !errors.Is(
		err,
		errRepositoryRevocationStoreUnavailable,
	) {
		t.Fatalf("nil Begin error = %v", err)
	}
	if _, err := nilStore.MarkCleanupSafe(
		context.Background(),
		"acme/model",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	); !errors.Is(err, errRepositoryRevocationStoreUnavailable) {
		t.Fatalf("nil MarkCleanupSafe error = %v", err)
	}
	if _, err := nilStore.DeleteCleanupSafe(
		context.Background(),
		"acme/model",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	); !errors.Is(err, errRepositoryRevocationStoreUnavailable) {
		t.Fatalf("nil DeleteCleanupSafe error = %v", err)
	}

	database := openRepositoryRevocationTestDB(t)
	store := newRepositoryRevocationStore(database)
	store.newToken = func() (string, error) {
		return "", errors.New("entropy unavailable")
	}
	if _, err := store.Begin(
		context.Background(),
		"acme/model",
		"acme/model",
	); !errors.Is(err, errRepositoryRevocationTokenUnavailable) {
		t.Fatalf("token generation error = %v", err)
	}
	if _, err := store.Begin(
		context.Background(),
		"",
		"acme/model",
	); !errors.Is(err, errRepositoryRevocationMarkerInvalid) {
		t.Fatalf("invalid marker error = %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(
		err,
		errRepositoryRevocationStoreUnavailable,
	) {
		t.Fatalf("closed database error = %v, want store-unavailable error", err)
	}
}

func openRepositoryRevocationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open(
		"sqlite",
		filepath.Join(t.TempDir(), "repository-revocations.db"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&db.HuggingFaceRepositoryRevocation{}); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return database
}

func tokenSequence(t *testing.T, tokens ...string) func() (string, error) {
	t.Helper()
	index := 0
	return func() (string, error) {
		if index >= len(tokens) {
			t.Fatalf("token generator called more than %d times", len(tokens))
		}
		token := tokens[index]
		index++
		return token, nil
	}
}
