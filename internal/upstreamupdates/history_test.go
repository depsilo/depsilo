package upstreamupdates

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/db"
)

func openHistoryContractTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return database
}

func newHistoryContractTestSubject(t *testing.T) (*History, *gorm.DB) {
	t.Helper()
	database := openHistoryContractTestDB(t)
	history, err := NewHistory(database)
	if err != nil {
		t.Fatal(err)
	}
	return history, database
}

func historyObservation(cacheEntryID uint, result string, observedAt time.Time) Observation {
	return Observation{
		CacheEntryID: cacheEntryID,
		Ecosystem:    "pypi",
		Upstream:     "primary",
		Package:      "pillow",
		Result:       result,
		Detail:       "validator matched",
		Latency:      7 * time.Millisecond,
		ObservedAt:   observedAt,
	}
}

func mustRecordHistory(t *testing.T, history *History, observation Observation) Receipt {
	t.Helper()
	receipt, err := history.Record(context.Background(), observation)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func mustListHistory(t *testing.T, history *History, query HistoryQuery) HistoryPage {
	t.Helper()
	page, err := history.List(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestHistoryRecordCoalescesConsecutiveUnchangedAndErrorEpisodes(t *testing.T) {
	for _, result := range []string{ResultUnchanged, ResultError} {
		t.Run(result, func(t *testing.T) {
			history, _ := newHistoryContractTestSubject(t)
			firstSeen := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
			lastSeen := firstSeen.Add(3 * time.Minute)

			first := historyObservation(41, result, firstSeen)
			firstReceipt := mustRecordHistory(t, history, first)
			if firstReceipt.Coalesced || firstReceipt.OccurrenceCount != 1 || firstReceipt.EventID == 0 {
				t.Fatalf("first receipt = %+v", firstReceipt)
			}

			second := first
			second.ObservedAt = lastSeen
			second.Latency = 19 * time.Millisecond
			secondReceipt := mustRecordHistory(t, history, second)
			if !secondReceipt.Coalesced {
				t.Fatalf("second receipt = %+v, want coalesced", secondReceipt)
			}
			if secondReceipt.EventID != firstReceipt.EventID || secondReceipt.OccurrenceCount != 2 {
				t.Fatalf("second receipt = %+v, first = %+v", secondReceipt, firstReceipt)
			}

			page := mustListHistory(t, history, HistoryQuery{Limit: 10})
			if page.Total != 1 || len(page.Items) != 1 || page.NextCursor != "" {
				t.Fatalf("page = %+v", page)
			}
			event := page.Items[0]
			if event.OccurrenceCount != 2 || event.LatencyMs != 19 {
				t.Fatalf("coalesced event = %+v", event)
			}
			if !event.FirstSeenAt.Equal(firstSeen) || !event.CreatedAt.Equal(firstSeen) || !event.LastSeenAt.Equal(lastSeen) {
				t.Fatalf("coalesced event times = first %v, created %v, last %v", event.FirstSeenAt, event.CreatedAt, event.LastSeenAt)
			}
		})
	}
}

func TestHistoryRecordStartsANewEpisodeWhenMeaningChanges(t *testing.T) {
	baseTime := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		change func(*Observation)
	}{
		{name: "detail", change: func(observation *Observation) { observation.Detail = "different outcome" }},
		{name: "result", change: func(observation *Observation) { observation.Result = ResultError }},
		{name: "upstream", change: func(observation *Observation) { observation.Upstream = "mirror" }},
		{name: "cache subject", change: func(observation *Observation) { observation.CacheEntryID++ }},
		{name: "ecosystem", change: func(observation *Observation) { observation.Ecosystem = "extra:mirror" }},
		{name: "package", change: func(observation *Observation) { observation.Package = "requests" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			history, _ := newHistoryContractTestSubject(t)
			first := historyObservation(51, ResultUnchanged, baseTime)
			firstReceipt := mustRecordHistory(t, history, first)

			second := first
			second.ObservedAt = baseTime.Add(time.Minute)
			test.change(&second)
			secondReceipt := mustRecordHistory(t, history, second)

			if secondReceipt.Coalesced || secondReceipt.OccurrenceCount != 1 {
				t.Fatalf("changed observation receipt = %+v", secondReceipt)
			}
			if secondReceipt.EventID == firstReceipt.EventID {
				t.Fatalf("changed observation reused event %d", firstReceipt.EventID)
			}
			page := mustListHistory(t, history, HistoryQuery{Limit: 10})
			if page.Total != 2 || len(page.Items) != 2 {
				t.Fatalf("page = %+v", page)
			}
		})
	}
}

func TestHistoryRecordNeverCoalescesUpdatedEvents(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	observedAt := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
	observation := historyObservation(61, ResultUpdated, observedAt)
	first := mustRecordHistory(t, history, observation)
	observation.ObservedAt = observedAt.Add(time.Minute)
	second := mustRecordHistory(t, history, observation)

	if first.Coalesced || second.Coalesced || first.EventID == second.EventID {
		t.Fatalf("updated receipts = first %+v, second %+v", first, second)
	}
	page := mustListHistory(t, history, HistoryQuery{Limit: 10})
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
}

func TestHistoryRecordUsesAppendOrderWhenTheClockMovesBackward(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	baseTime := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)

	unchanged := historyObservation(62, ResultUnchanged, baseTime)
	first := mustRecordHistory(t, history, unchanged)
	errorObservation := unchanged
	errorObservation.Result = ResultError
	errorObservation.ObservedAt = baseTime.Add(-time.Hour)
	mustRecordHistory(t, history, errorObservation)

	recovered := unchanged
	recovered.ObservedAt = baseTime.Add(-30 * time.Minute)
	recovery := mustRecordHistory(t, history, recovered)
	if recovery.Coalesced || recovery.EventID == first.EventID {
		t.Fatalf("recovery crossed the intervening error episode: first=%+v recovery=%+v", first, recovery)
	}

	// A still earlier wall-clock observation is nevertheless the next append,
	// so it must extend the recovery episode rather than an older row.
	recovered.ObservedAt = baseTime.Add(-2 * time.Hour)
	coalesced := mustRecordHistory(t, history, recovered)
	if !coalesced.Coalesced || coalesced.EventID != recovery.EventID || coalesced.OccurrenceCount != 2 {
		t.Fatalf("append-order coalescing = %+v, recovery = %+v", coalesced, recovery)
	}

	page := mustListHistory(t, history, HistoryQuery{Limit: 10})
	if page.Total != 3 || len(page.Items) != 3 {
		t.Fatalf("clock rollback page = %+v", page)
	}
}

func TestHistoryListMapsLegacyRowsToOneOccurrence(t *testing.T) {
	history, database := newHistoryContractTestSubject(t)
	createdAt := time.Date(2026, 7, 16, 12, 30, 0, 0, time.UTC)
	legacy := db.UpstreamUpdateEvent{
		Ecosystem: "pypi",
		Upstream:  "primary",
		Package:   "legacy-package",
		Result:    ResultUnchanged,
		Detail:    "created before episode coalescing",
		LatencyMs: 5,
		CreatedAt: createdAt,
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.UpstreamUpdateEvent{}).
		Where("id = ?", legacy.ID).
		UpdateColumns(map[string]any{"occurrence_count": 0, "last_seen_at": time.Time{}}).Error; err != nil {
		t.Fatal(err)
	}

	page := mustListHistory(t, history, HistoryQuery{Limit: 10})
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	event := page.Items[0]
	if event.OccurrenceCount != 1 {
		t.Fatalf("legacy occurrence count = %d, want 1", event.OccurrenceCount)
	}
	if !event.FirstSeenAt.Equal(createdAt) || !event.LastSeenAt.Equal(createdAt) || !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("legacy times = first %v, last %v, created %v", event.FirstSeenAt, event.LastSeenAt, event.CreatedAt)
	}
}

func TestHistoryMigrationPreservesLegacyRowsWithoutCompressingThem(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "legacy-history.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	if err := database.Exec(`CREATE TABLE upstream_update_events (
		id integer PRIMARY KEY AUTOINCREMENT,
		ecosystem text,
		upstream text,
		package text,
		result text,
		detail text,
		latency_ms integer,
		created_at datetime
	)`).Error; err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 7, 16, 6, 30, 0, 0, time.UTC)
	for id := 7; id <= 8; id++ {
		if err := database.Exec(
			`INSERT INTO upstream_update_events
				(id, ecosystem, upstream, package, result, detail, latency_ms, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, "pypi", "primary", "legacy-package", ResultUnchanged,
			"upstream metadata not modified", 5, createdAt.Add(time.Duration(id-7)*time.Minute),
		).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	history, err := NewHistory(database)
	if err != nil {
		t.Fatal(err)
	}
	page := mustListHistory(t, history, HistoryQuery{Limit: 10})
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("migration compressed legacy rows: %+v", page)
	}
	if page.Items[0].ID != 8 || page.Items[1].ID != 7 {
		t.Fatalf("legacy IDs changed: %+v", page.Items)
	}
	for _, event := range page.Items {
		if event.CacheEntryID != 0 || event.OccurrenceCount != 1 || !event.LastSeenAt.Equal(event.CreatedAt) {
			t.Fatalf("legacy fallback = %+v", event)
		}
	}
}

func TestHistoryCursorKeepsAStableSnapshotAcrossInsertsAndCoalescing(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	sharedTime := time.Date(2026, 7, 17, 11, 0, 0, 0, time.UTC)
	var recorded []Receipt
	for subject := uint(1); subject <= 5; subject++ {
		observation := historyObservation(subject, ResultUnchanged, sharedTime)
		observation.Package = fmt.Sprintf("package-%d", subject)
		recorded = append(recorded, mustRecordHistory(t, history, observation))
	}

	firstPage := mustListHistory(t, history, HistoryQuery{Limit: 2})
	if firstPage.Total != 5 || len(firstPage.Items) != 2 || firstPage.NextCursor == "" {
		t.Fatalf("first page = %+v", firstPage)
	}

	inserted := historyObservation(99, ResultError, sharedTime.Add(time.Hour))
	inserted.Package = "new-after-snapshot"
	insertedReceipt := mustRecordHistory(t, history, inserted)

	coalesced := historyObservation(1, ResultUnchanged, sharedTime.Add(2*time.Hour))
	coalesced.Package = "package-1"
	coalescedReceipt := mustRecordHistory(t, history, coalesced)
	if !coalescedReceipt.Coalesced || coalescedReceipt.EventID != recorded[0].EventID {
		t.Fatalf("coalesced receipt = %+v, original = %+v", coalescedReceipt, recorded[0])
	}

	items := append([]HistoryEvent(nil), firstPage.Items...)
	cursor := firstPage.NextCursor
	for cursor != "" {
		page := mustListHistory(t, history, HistoryQuery{Limit: 2, Cursor: cursor})
		if page.Total != 5 {
			t.Fatalf("snapshot total changed to %d", page.Total)
		}
		items = append(items, page.Items...)
		cursor = page.NextCursor
	}

	if len(items) != 5 {
		t.Fatalf("snapshot items = %+v", items)
	}
	gotIDs := make([]uint, 0, len(items))
	seen := make(map[uint]bool, len(items))
	for _, item := range items {
		if seen[item.ID] {
			t.Fatalf("event %d appeared on multiple pages", item.ID)
		}
		seen[item.ID] = true
		gotIDs = append(gotIDs, item.ID)
		if item.ID == insertedReceipt.EventID {
			t.Fatalf("post-snapshot event %d leaked into pagination", item.ID)
		}
	}
	wantIDs := []uint{
		recorded[4].EventID,
		recorded[3].EventID,
		recorded[2].EventID,
		recorded[1].EventID,
		recorded[0].EventID,
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("snapshot ids = %v, want %v", gotIDs, wantIDs)
	}
	last := items[len(items)-1]
	if last.OccurrenceCount != 2 || !last.CreatedAt.Equal(sharedTime) || !last.FirstSeenAt.Equal(sharedTime) || !last.LastSeenAt.Equal(sharedTime.Add(2*time.Hour)) {
		t.Fatalf("coalesced snapshot event = %+v", last)
	}
}

func TestHistoryListRejectsInvalidAndMismatchedCursors(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	observedAt := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	for subject := uint(71); subject <= 72; subject++ {
		mustRecordHistory(t, history, historyObservation(subject, ResultUnchanged, observedAt))
	}
	page := mustListHistory(t, history, HistoryQuery{Limit: 1, Ecosystem: "pypi"})
	if page.NextCursor == "" {
		t.Fatal("first page did not return a cursor")
	}

	zero := 0
	tests := []struct {
		name  string
		query HistoryQuery
	}{
		{name: "malformed cursor", query: HistoryQuery{Limit: 1, Cursor: "not-a-cursor"}},
		{name: "filter mismatch", query: HistoryQuery{Limit: 1, Cursor: page.NextCursor, Ecosystem: "npm"}},
		{name: "cursor and offset", query: HistoryQuery{Limit: 1, Cursor: page.NextCursor, LegacyOffset: &zero, Ecosystem: "pypi"}},
		{name: "unknown result", query: HistoryQuery{Limit: 1, Result: "success"}},
		{name: "ecosystem filter too long", query: HistoryQuery{Limit: 1, Ecosystem: strings.Repeat("e", eventEcosystemLimit+1)}},
		{name: "upstream filter too long", query: HistoryQuery{Limit: 1, Upstream: strings.Repeat("u", eventUpstreamLimit+1)}},
		{name: "package filter too long", query: HistoryQuery{Limit: 1, Package: strings.Repeat("p", eventPackageLimit+1)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := history.List(context.Background(), test.query)
			if !errors.Is(err, ErrInvalidHistoryQuery) {
				t.Fatalf("List error = %v, want ErrInvalidHistoryQuery", err)
			}
		})
	}
}

func TestHistoryRejectsInvalidObservations(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	valid := historyObservation(81, ResultUnchanged, time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC))
	tests := []struct {
		name   string
		change func(*Observation)
	}{
		{name: "missing subject", change: func(observation *Observation) { observation.CacheEntryID = 0 }},
		{name: "unknown result", change: func(observation *Observation) { observation.Result = "success" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observation := valid
			test.change(&observation)
			_, err := history.Record(context.Background(), observation)
			if !errors.Is(err, ErrInvalidObservation) {
				t.Fatalf("Record error = %v, want ErrInvalidObservation", err)
			}
		})
	}
}

func TestHistorySanitizesCredentialBearingURLs(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	observation := historyObservation(91, ResultError, time.Date(2026, 7, 17, 14, 0, 0, 0, time.UTC))
	observation.Upstream = "https://upstream-user:upstream-secret@mirror.example/private?token=hidden"
	observation.Package = "https://package-user:package-secret@packages.example/private?token=hidden"
	observation.Detail = "https://detail-user:detail-secret@detail.example/private?token=hidden"
	mustRecordHistory(t, history, observation)

	page := mustListHistory(t, history, HistoryQuery{Limit: 1})
	if len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	event := page.Items[0]
	if event.Upstream != "https://mirror.example/***" ||
		event.Package != "https://packages.example/***" ||
		event.Detail != "https://detail.example/***" {
		t.Fatalf("sanitized event = %+v", event)
	}
	persisted := event.Ecosystem + event.Upstream + event.Package + event.Detail
	for _, secret := range []string{"upstream-user", "upstream-secret", "package-user", "package-secret", "detail-user", "detail-secret", "token", "hidden", "/private"} {
		if strings.Contains(persisted, secret) {
			t.Fatalf("event disclosed %q: %+v", secret, event)
		}
	}
}

func TestHistorySanitizesLegacyRowsAtReadBoundary(t *testing.T) {
	history, database := newHistoryContractTestSubject(t)
	legacy := db.UpstreamUpdateEvent{
		CacheEntryID: 92,
		Ecosystem:    "https://ecosystem-user:ecosystem-secret@ecosystem.example/private?token=hidden",
		Upstream:     "https://upstream-user:upstream-secret@mirror.example/private?token=hidden",
		Package:      "https://package-user:package-secret@packages.example/private?token=hidden",
		Result:       ResultError,
		Detail:       "https://detail-user:detail-secret@detail.example/private?token=hidden",
		CreatedAt:    time.Date(2026, 7, 17, 14, 30, 0, 0, time.UTC),
	}
	if err := database.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	page := mustListHistory(t, history, HistoryQuery{Limit: 1})
	if len(page.Items) != 1 {
		t.Fatalf("page = %+v", page)
	}
	event := page.Items[0]
	if event.Ecosystem != "https://ecosystem.example/***" ||
		event.Upstream != "https://mirror.example/***" ||
		event.Package != "https://packages.example/***" ||
		event.Detail != "https://detail.example/***" {
		t.Fatalf("sanitized legacy event = %+v", event)
	}
	projected := event.Ecosystem + event.Upstream + event.Package + event.Detail
	for _, secret := range []string{"user", "secret", "token", "hidden", "/private"} {
		if strings.Contains(projected, secret) {
			t.Fatalf("legacy event disclosed %q: %+v", secret, event)
		}
	}
}

func TestHistoryHonorsCancelledContexts(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := history.Record(ctx, historyObservation(101, ResultUnchanged, time.Now().UTC()))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Record error = %v, want context.Canceled", err)
	}
	page := mustListHistory(t, history, HistoryQuery{Limit: 10})
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("cancelled Record persisted an event: %+v", page)
	}

	_, err = history.List(ctx, HistoryQuery{Limit: 10})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("List error = %v, want context.Canceled", err)
	}
}

func TestHistoryConcurrentRecordsDoNotLoseOccurrences(t *testing.T) {
	history, _ := newHistoryContractTestSubject(t)
	observation := historyObservation(111, ResultUnchanged, time.Date(2026, 7, 17, 15, 0, 0, 0, time.UTC))
	const writers = 24

	start := make(chan struct{})
	errorsByWriter := make(chan error, writers)
	var wait sync.WaitGroup
	for writer := 0; writer < writers; writer++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := history.Record(context.Background(), observation)
			errorsByWriter <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errorsByWriter)
	for err := range errorsByWriter {
		if err != nil {
			t.Fatalf("concurrent Record error = %v", err)
		}
	}

	page := mustListHistory(t, history, HistoryQuery{Limit: 10})
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].OccurrenceCount != writers {
		t.Fatalf("concurrent page = %+v", page)
	}
}
