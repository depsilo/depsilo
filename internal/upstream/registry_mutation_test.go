package upstream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
	"gorm.io/gorm"
)

func registryFixture(t *testing.T, ecosystem string, count int) (*gorm.DB, *Registry) {
	t.Helper()
	database := bootstrapDB(t)
	for i := 0; i < count; i++ {
		record := db.UpstreamRecord{
			AdapterType:   ecosystem,
			Name:          fmt.Sprintf("source-%d", i+1),
			URL:           fmt.Sprintf("https://source-%d.example", i+1),
			Priority:      i + 1,
			ProbeMode:     "passive",
			ProbeInterval: "30m",
			Healthy:       true,
			SuccessRate:   1,
		}
		if err := database.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	registry, err := NewRegistry(database, []string{ecosystem})
	if err != nil {
		t.Fatal(err)
	}
	return database, registry
}

func TestRegistryMutationsPublishCommittedSnapshot(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	ctx := context.Background()
	created, err := registry.Create(ctx, MutationInput{AdapterType: "pypi", Name: "two", URL: "https://two.example", Priority: 2, ProbeMode: "passive", ProbeInterval: "30m"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Pools()["pypi"].Find(created.ID); !ok {
		t.Fatal("created row absent from runtime pool")
	}

	updated, err := registry.Update(ctx, created.ID, MutationInput{AdapterType: "pypi", Name: "two", URL: "https://changed.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.URL != "https://changed.example" {
		t.Fatalf("url=%q", updated.URL)
	}

	deleted, err := registry.Delete(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.ID != created.ID {
		t.Fatalf("deleted=%d", deleted.ID)
	}
	if _, ok := registry.Pools()["pypi"].Find(created.ID); ok {
		t.Fatal("deleted row remains in runtime pool")
	}
	var count int64
	if err := database.Model(&db.UpstreamRecord{}).Where("id = ?", created.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("deleted row remains in database")
	}
}

func TestRegistryDeleteLastUpstreamLeavesDBAndPoolUnchanged(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	before := registry.Pools()["pypi"].Snapshot()
	_, err := registry.Delete(context.Background(), before[0].ID)
	if !errors.Is(err, ErrLastUpstream) {
		t.Fatalf("err=%v", err)
	}
	if got := registry.Pools()["pypi"].Snapshot(); len(got) != 1 || got[0].ID != before[0].ID {
		t.Fatalf("snapshot changed: %#v", got)
	}
	var count int64
	if err := database.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
}

func TestRegistryConcurrentDeleteSameIDReturnsNotFoundAfterWinner(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 2)
	id := registry.Pools()["pypi"].Snapshot()[0].ID
	lock := registry.mutationLocks["pypi"]
	lock.Lock()

	results := make(chan error, 2)
	started := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			started <- struct{}{}
			_, err := registry.Delete(context.Background(), id)
			results <- err
		}()
	}
	<-started
	<-started
	time.Sleep(20 * time.Millisecond)
	lock.Unlock()

	var successes, notFound int
	for i := 0; i < 2; i++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrNotFound):
			notFound++
		case errors.Is(err, ErrLastUpstream):
			t.Fatalf("losing delete returned ErrLastUpstream: %v", err)
		default:
			t.Fatalf("unexpected delete error: %v", err)
		}
	}
	if successes != 1 || notFound != 1 {
		t.Fatalf("successes=%d notFound=%d", successes, notFound)
	}
}

func TestRegistryRejectsInactiveAndImmutableEcosystem(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 1)
	_, err := registry.Create(context.Background(), MutationInput{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"})
	if !errors.Is(err, ErrEcosystemNotActive) {
		t.Fatalf("create err=%v", err)
	}
	id := registry.Pools()["pypi"].Snapshot()[0].ID
	_, err = registry.Update(context.Background(), id, MutationInput{AdapterType: "npm", Name: "one", URL: "https://one.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"})
	if !errors.Is(err, ErrImmutableEcosystem) {
		t.Fatalf("update err=%v", err)
	}
}

func TestRegistryMutationValidation(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 1)
	cases := []MutationInput{
		{AdapterType: "pypi", Name: "", URL: "https://valid.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"},
		{AdapterType: "pypi", Name: "two", URL: "ftp://invalid.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"},
		{AdapterType: "pypi", Name: "two", URL: "https://valid.example", Proxy: "file:///tmp/proxy", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m"},
		{AdapterType: "pypi", Name: "two", URL: "https://valid.example", Priority: 1, ProbeMode: "sometimes", ProbeInterval: "30m"},
		{AdapterType: "pypi", Name: "two", URL: "https://valid.example", Priority: 1, ProbeMode: "passive", ProbeInterval: "0s"},
	}
	for _, input := range cases {
		if _, err := registry.Create(context.Background(), input); !errors.Is(err, ErrInvalidUpstream) {
			t.Fatalf("input=%#v err=%v", input, err)
		}
	}
}

func TestRegistryConcurrentMutationsRemainConsistent(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 1)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: fmt.Sprintf("mirror-%02d", i), URL: fmt.Sprintf("https://mirror-%02d.example", i), Priority: i + 2, ProbeMode: "passive", ProbeInterval: "30m"})
		}(i)
	}
	wg.Wait()
	if err := registry.verify("pypi"); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryConflictRollsBackWithoutPublishing(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	before := registry.Pools()["pypi"].Snapshot()
	_, err := registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: "source-1", URL: "https://duplicate.example", Priority: 2, ProbeMode: "passive", ProbeInterval: "30m"})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err=%v", err)
	}
	if got := registry.Pools()["pypi"].Snapshot(); len(got) != 1 || got[0] != before[0] {
		t.Fatalf("snapshot changed: %#v", got)
	}
	var count int64
	if err := database.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d", count)
	}
}

func TestRegistryCommitFailureLeavesDBPoolAndWorkersUnpublished(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var record db.UpstreamRecord
	if err := database.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&record).Updates(map[string]any{"probe_mode": "active", "probe_interval": "1h"}).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	registry.Start(context.Background())
	defer registry.Close()

	beforePool := registry.Pools()["pypi"].Snapshot()
	beforeWorker := registryWorker(t, registry, record.ID)
	injected := errors.New("injected commit failure")
	registry.commit = func(*gorm.DB) error { return injected }
	_, err = registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: "two", URL: "https://two.example", Priority: 2, ProbeMode: "active", ProbeInterval: "1h"})
	if !errors.Is(err, injected) {
		t.Fatalf("err=%v", err)
	}

	var count int64
	if err := database.Model(&db.UpstreamRecord{}).Where("adapter_type = ?", "pypi").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("committed rows=%d want=1", count)
	}
	afterPool := registry.Pools()["pypi"].Snapshot()
	if len(afterPool) != 1 || afterPool[0] != beforePool[0] {
		t.Fatalf("pool published after commit failure: %#v", afterPool)
	}
	if got := registryWorker(t, registry, record.ID); got.done != beforeWorker.done {
		t.Fatal("worker plan published after commit failure")
	}
	select {
	case <-beforeWorker.done:
		t.Fatal("existing worker was stopped")
	default:
	}
}

func TestRegistryPublishMismatchReloadsCommittedDatabase(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var records []db.UpstreamRecord
	if err := database.Where("adapter_type = ?", "pypi").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	wrong, err := buildPoolSnapshot(nil, registry.Pools()["pypi"].load())
	if err != nil {
		t.Fatal(err)
	}
	current := registry.Pools()["pypi"].load()
	if err := registry.publish("pypi", preparedMutation{next: wrong, workers: planWorkers(current, wrong)}); err != nil {
		t.Fatal(err)
	}
	if got := registry.Pools()["pypi"].Snapshot(); len(got) != 1 || got[0].ID != records[0].ID {
		t.Fatalf("reload=%#v", got)
	}
}

func TestRegistryPublishReturnsReconcileFailureWhenReloadFails(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	wrong, err := buildPoolSnapshot(nil, registry.Pools()["pypi"].load())
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	current := registry.Pools()["pypi"].load()
	err = registry.publish("pypi", preparedMutation{next: wrong, workers: planWorkers(current, wrong)})
	if !errors.Is(err, ErrReconcileFailed) {
		t.Fatalf("err=%v", err)
	}
	if registry.degradedError("pypi") == nil {
		t.Fatal("ecosystem was not marked degraded")
	}
}

func TestRegistryIntervalUpdateStopsOldWorkerAndStartsReplacement(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var record db.UpstreamRecord
	if err := database.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&record).Updates(map[string]any{"probe_mode": "active", "probe_interval": "1h"}).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	registry.Start(context.Background())
	defer registry.Close()
	old := registryWorker(t, registry, record.ID)
	_, err = registry.Update(context.Background(), record.ID, MutationInput{AdapterType: "pypi", Name: record.Name, URL: record.URL, Priority: record.Priority, ProbeMode: "active", ProbeInterval: "2h"})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, old.done)
	if next := registryWorker(t, registry, record.ID); next.done == old.done {
		t.Fatal("worker was not replaced")
	}
}

func TestRegistryNameOnlyUpdateReplacesWorkerWithPublishedObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	database, registry := registryFixture(t, "pypi", 1)
	var record db.UpstreamRecord
	if err := database.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&record).Updates(map[string]any{"url": server.URL, "probe_mode": "active", "probe_interval": "10ms"}).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	registry.Start(context.Background())
	defer registry.Close()
	oldObject, ok := registry.Pools()["pypi"].Find(record.ID)
	if !ok {
		t.Fatal("missing old runtime object")
	}
	oldWorker := registryWorker(t, registry, record.ID)

	_, err = registry.Update(context.Background(), record.ID, MutationInput{AdapterType: "pypi", Name: "renamed", URL: server.URL, Priority: record.Priority, ProbeMode: "active", ProbeInterval: "10ms"})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, oldWorker.done)
	newWorker := registryWorker(t, registry, record.ID)
	if newWorker.done == oldWorker.done {
		t.Fatal("name-only update retained old worker")
	}
	current, ok := registry.Pools()["pypi"].Find(record.ID)
	if !ok || current == oldObject || current.Name != "renamed" {
		t.Fatalf("published object=%#v old=%p current=%p", current, oldObject, current)
	}

	deadline := time.Now().Add(time.Second)
	for current.HealthSnapshot().LastCheckedAt.IsZero() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if current.HealthSnapshot().LastCheckedAt.IsZero() {
		t.Fatal("replacement worker did not update published object health")
	}
	var logs int64
	deadline = time.Now().Add(time.Second)
	for logs == 0 && time.Now().Before(deadline) {
		if err := database.Model(&db.UpstreamLatencyLog{}).Where("upstream_id = ? AND name = ?", record.ID, "renamed").Count(&logs).Error; err != nil {
			t.Fatal(err)
		}
		if logs == 0 {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if logs == 0 {
		t.Fatal("replacement worker did not persist the new upstream name")
	}
}

func TestRegistryPriorityOnlyUpdateReplacesActiveWorker(t *testing.T) {
	database, registry := registryFixture(t, "pypi", 1)
	var record db.UpstreamRecord
	if err := database.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&record).Updates(map[string]any{"probe_mode": "active", "probe_interval": "1h"}).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	registry.Start(context.Background())
	defer registry.Close()
	oldObject, _ := registry.Pools()["pypi"].Find(record.ID)
	oldWorker := registryWorker(t, registry, record.ID)

	_, err = registry.Update(context.Background(), record.ID, MutationInput{AdapterType: "pypi", Name: record.Name, URL: record.URL, Priority: record.Priority + 1, ProbeMode: "active", ProbeInterval: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	waitDone(t, oldWorker.done)
	newWorker := registryWorker(t, registry, record.ID)
	current, _ := registry.Pools()["pypi"].Find(record.ID)
	if newWorker.done == oldWorker.done || current == oldObject {
		t.Fatal("priority-only update did not move worker ownership to the published object")
	}
}

func TestRegistryCheckPersistsProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	database, registry := registryFixture(t, "pypi", 1)
	record := registry.Pools()["pypi"].Snapshot()[0]
	if err := database.Model(&db.UpstreamRecord{}).Where("id = ?", record.ID).Update("url", server.URL).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	updated, result, err := registry.Check(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Healthy || updated.LastCheckedAt.IsZero() {
		t.Fatalf("updated=%#v result=%#v", updated, result)
	}
	var stored db.UpstreamRecord
	if err := database.First(&stored, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.LastCheckedAt.IsZero() || !stored.Healthy {
		t.Fatalf("stored=%#v", stored)
	}
	u, _ := registry.Pools()["pypi"].Find(record.ID)
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.health.totalReqs != 1 || u.health.successReqs != 1 {
		t.Fatalf("health applied more than once: %#v", u.health)
	}
}

func TestRegistryCheckPersistenceFailureLeavesPublishedHealthUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	database, registry := registryFixture(t, "pypi", 1)
	var record db.UpstreamRecord
	if err := database.First(&record).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&record).Update("url", server.URL).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := registry.Pools()["pypi"].Find(record.ID)
	before := u.HealthSnapshot()

	callbackName := "test:fail_manual_check_health"
	if err := database.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "UpstreamRecord" {
			tx.AddError(errors.New("health store unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Update().Remove(callbackName) })

	_, result, err := registry.Check(context.Background(), record.ID)
	if err == nil || err.Error() != "health store unavailable" {
		t.Fatalf("err=%v", err)
	}
	if result != (ProbeResult{}) {
		t.Fatalf("failed check exposed a result: %#v", result)
	}
	if after := u.HealthSnapshot(); after != before {
		t.Fatalf("live health changed before=%#v after=%#v", before, after)
	}
	var logs int64
	if err := database.Model(&db.UpstreamLatencyLog{}).Count(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if logs != 0 {
		t.Fatalf("latency logs=%d", logs)
	}
}

func TestRegistryMutationAndLifecycleDoNotRace(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			registry.Start(ctx)
			registry.Close()
		}()
		go func(i int) {
			defer wg.Done()
			_, _ = registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: fmt.Sprintf("lifecycle-%02d", i), URL: fmt.Sprintf("https://lifecycle-%02d.example", i), Priority: i + 2, ProbeMode: "active", ProbeInterval: "1h"})
		}(i)
	}
	wg.Wait()
	registry.Close()
	if err := registry.verify("pypi"); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryReadersAndMutationsAreRaceFree(t *testing.T) {
	_, registry := registryFixture(t, "pypi", 2)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 500; n++ {
				for _, pool := range registry.Pools() {
					for _, u := range pool.Snapshot() {
						_ = u.HealthSnapshot()
					}
				}
				_ = registry.List()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				_, _ = registry.Create(context.Background(), MutationInput{AdapterType: "pypi", Name: fmt.Sprintf("r-%d-%d", i, n), URL: "https://example.invalid", Priority: n + 3, ProbeMode: "passive", ProbeInterval: "30m"})
			}
		}(i)
	}
	wg.Wait()
	if err := registry.verify("pypi"); err != nil {
		t.Fatal(err)
	}
}
