package huggingface

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

func TestMutableRefPinKeyIsSharedAcrossFilesQueriesAndResolveRaw(t *testing.T) {
	resolve := ParseRequestPath("/acme/model/resolve/refs%2Fpr%2F1/a.bin")
	raw := ParseRequestPath("/acme/model/raw/refs%2Fpr%2F1/b.json")
	resolveKey, ok := mutableRefPinKey(resolve)
	if !ok {
		t.Fatal("resolve ref was not pinnable")
	}
	rawKey, ok := mutableRefPinKey(raw)
	if !ok {
		t.Fatal("raw ref was not pinnable")
	}
	want := "huggingface/acme/model/ref/refs%2Fpr%2F1"
	if resolveKey != want || rawKey != want {
		t.Fatalf("pin keys = (%q, %q), want shared %q", resolveKey, rawKey, want)
	}
}

func TestMutableRefPinKeyCollapsesRepositoryCaseAliasesButPreservesRefCase(t *testing.T) {
	upperRepository := ParseRequestPath("/OpenAI/Whisper-Tiny/resolve/Main/config.json")
	lowerRepository := ParseRequestPath("/openai/whisper-tiny/raw/Main/model.bin")
	upperKey, ok := mutableRefPinKey(upperRepository)
	if !ok {
		t.Fatal("mixed-case repository ref was not pinnable")
	}
	lowerKey, ok := mutableRefPinKey(lowerRepository)
	if !ok {
		t.Fatal("lowercase repository ref was not pinnable")
	}
	const want = "huggingface/openai/whisper-tiny/ref/Main"
	if upperKey != want || lowerKey != want {
		t.Fatalf("case-alias pin keys = (%q, %q), want shared %q", upperKey, lowerKey, want)
	}

	lowerRef := ParseRequestPath("/openai/whisper-tiny/resolve/main/config.json")
	lowerRefKey, ok := mutableRefPinKey(lowerRef)
	if !ok {
		t.Fatal("lowercase ref was not pinnable")
	}
	if lowerRefKey == want {
		t.Fatalf("case-sensitive refs collapsed to one pin key %q", lowerRefKey)
	}
}

func TestSchemaV3RefPinCleanupCannotBeResplitByRepositoryCaseAliases(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "schema-v3-ref-pins.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacyPins := []db.HuggingFaceRefPin{
		{
			Key:       "huggingface/OpenAI/Whisper-Tiny/ref/Main",
			Commit:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ExpiresAt: now.Add(time.Hour),
		},
		{
			Key:       "huggingface/openai/whisper-tiny/ref/Main",
			Commit:    "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ExpiresAt: now.Add(time.Hour),
		},
	}
	if err := database.Create(&legacyPins).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec("DELETE FROM schema_migrations WHERE version >= 3").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatalf("apply schema v3 cleanup: %v", err)
	}
	var pinsAfterCleanup int64
	if err := database.Model(&db.HuggingFaceRefPin{}).Count(&pinsAfterCleanup).Error; err != nil {
		t.Fatal(err)
	}
	if pinsAfterCleanup != 0 {
		t.Fatalf("schema v3 retained %d legacy ref pins", pinsAfterCleanup)
	}

	upperKey, ok := mutableRefPinKey(ParseRequestPath(
		"/OpenAI/Whisper-Tiny/resolve/Main/config.json",
	))
	if !ok {
		t.Fatal("mixed-case repository ref was not pinnable")
	}
	lowerKey, ok := mutableRefPinKey(ParseRequestPath(
		"/openai/whisper-tiny/raw/Main/model.bin",
	))
	if !ok {
		t.Fatal("lowercase repository ref was not pinnable")
	}
	handler := &Handler{db: database}
	first, err := handler.claimRefPin(context.Background(), db.HuggingFaceRefPin{
		Key:       upperKey,
		Commit:    legacyPins[0].Commit,
		ExpiresAt: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := handler.claimRefPin(context.Background(), db.HuggingFaceRefPin{
		Key:       lowerKey,
		Commit:    legacyPins[1].Commit,
		ExpiresAt: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key != second.Key || first.Commit != second.Commit {
		t.Fatalf("case aliases split after schema v3: first=%+v second=%+v", first, second)
	}
	var pinsAfterRequests int64
	if err := database.Model(&db.HuggingFaceRefPin{}).Count(&pinsAfterRequests).Error; err != nil {
		t.Fatal(err)
	}
	if pinsAfterRequests != 1 {
		t.Fatalf("case aliases recreated %d ref pins after schema v3, want 1", pinsAfterRequests)
	}
}

func TestSelectAfterFailureNeverReadmitsCriticallyFailedOnlySource(t *testing.T) {
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "only",
		URL:       "https://only.example",
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	failed := pool.Snapshot()[0]
	failed.ReportCriticalFailure(time.Millisecond)
	handler := &Handler{selector: upstream.NewPrioritySelector(pool)}
	if selected := handler.selectAfterFailure(context.Background(), failed); selected != nil {
		t.Fatalf("critically failed source was reselected: %s", selected.Name)
	}
}

func TestClaimRefPinConcurrentRenewalReturnsOneWinner(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "ref-pins.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	handler := &Handler{db: database}
	const (
		key    = "huggingface/acme/model/ref/main"
		old    = "0000000000000000000000000000000000000000"
		first  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		second = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	now := time.Now().UTC()
	if err := database.Create(&db.HuggingFaceRefPin{
		Key: key, Commit: old, ExpiresAt: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan db.HuggingFaceRefPin, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, commit := range []string{first, second} {
		wg.Add(1)
		go func(commit string) {
			defer wg.Done()
			<-start
			result, claimErr := handler.claimRefPin(context.Background(), db.HuggingFaceRefPin{
				Key: key, Commit: commit, ExpiresAt: now.Add(time.Minute),
			}, now)
			if claimErr != nil {
				errs <- claimErr
				return
			}
			results <- result
		}(commit)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for claimErr := range errs {
		t.Fatal(claimErr)
	}

	var winner string
	for result := range results {
		if winner == "" {
			winner = result.Commit
		}
		if result.Commit != winner {
			t.Fatalf("concurrent callers observed different winners: %q and %q", winner, result.Commit)
		}
	}
	if winner != first && winner != second {
		t.Fatalf("unexpected winner %q", winner)
	}
	var persisted db.HuggingFaceRefPin
	if err := database.Where("key = ?", key).First(&persisted).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Commit != winner {
		t.Fatalf("persisted winner = %q, callers saw %q", persisted.Commit, winner)
	}
}
