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
