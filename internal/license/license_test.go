package license_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/license"
)

type queuedTaskRunner struct {
	mu        sync.Mutex
	tasks     []asyncruntime.Task
	submitErr error
}

func (runner *queuedTaskRunner) Submit(task asyncruntime.Task) error {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.submitErr != nil {
		return runner.submitErr
	}
	runner.tasks = append(runner.tasks, task)
	return nil
}

func (runner *queuedTaskRunner) Len() int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	return len(runner.tasks)
}

func (runner *queuedTaskRunner) RunNext(t *testing.T, ctx context.Context) {
	t.Helper()
	runner.mu.Lock()
	if len(runner.tasks) == 0 {
		runner.mu.Unlock()
		t.Fatal("no queued task")
	}
	task := runner.tasks[0]
	runner.tasks = runner.tasks[1:]
	runner.mu.Unlock()
	task(ctx)
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	d, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	if err := d.AutoMigrate(&db.LicenseStorage{}, &db.AuditLog{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return d
}

func TestSetKey_PersistsAndUpdatesState(t *testing.T) {
	d := newTestDB(t)
	m := license.NewManager(config.LicenseConfig{}, d)
	if m.IsPro() {
		t.Fatal("IsPro() = true on fresh manager, want false")
	}

	// Enterprise-contract keys are validated locally today: any non-empty key
	// activates Pro and must be persisted with its audit actor.
	_ = m.SetKey(context.Background(), "depsilo-test-key-1234", 7)

	var stored db.LicenseStorage
	if err := d.First(&stored).Error; err != nil {
		t.Fatalf("expected LicenseStorage row to exist after SetKey: %v", err)
	}
	if stored.Key != "depsilo-test-key-1234" {
		t.Errorf("stored.Key = %q, want %q", stored.Key, "depsilo-test-key-1234")
	}
	if stored.UpdatedBy != 7 {
		t.Errorf("stored.UpdatedBy = %d, want 7", stored.UpdatedBy)
	}
	if m.Status().KeyMasked != "depsilo-***" {
		t.Errorf("KeyMasked = %q, want %q", m.Status().KeyMasked, "depsilo-***")
	}
}

func TestSetKey_RecordsSuccessfulManagementAuditWithoutLicenseKey(t *testing.T) {
	d := newTestDB(t)
	m := license.NewManager(config.LicenseConfig{}, d)
	const rawKey = "depsilo-super-secret-key"

	if err := m.SetKey(context.Background(), rawKey, 7); err != nil {
		t.Fatalf("SetKey: %v", err)
	}

	var entry db.AuditLog
	if err := d.Where("action = ?", "license.set").First(&entry).Error; err != nil {
		t.Fatalf("find license management audit: %v", err)
	}
	if entry.Ecosystem != "admin" || entry.PackageName != "license" {
		t.Fatalf("audit target = %q/%q, want admin/license", entry.Ecosystem, entry.PackageName)
	}
	if entry.UserAgent != "operator:7" {
		t.Fatalf("audit actor = %q, want operator:7", entry.UserAgent)
	}
	if entry.CacheResult != "success" || entry.StatusCode != 200 {
		t.Fatalf("audit result = %q status=%d, want success/200", entry.CacheResult, entry.StatusCode)
	}
	if entry.CreatedAt.IsZero() {
		t.Fatal("audit CreatedAt is zero")
	}
	if strings.Contains(fmt.Sprintf("%+v", entry), rawKey) || strings.Contains(fmt.Sprintf("%+v", entry), "super-secret") {
		t.Fatalf("audit entry leaked license key material: %+v", entry)
	}
}

func TestClearKey_RemovesPersistenceAndResetsStatus(t *testing.T) {
	d := newTestDB(t)
	if err := d.Create(&db.LicenseStorage{ID: 1, Key: "depsilo-stale", UpdatedBy: 1}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := license.NewManager(config.LicenseConfig{}, d)
	if m.Status().KeyMasked != "depsilo-***" {
		t.Fatalf("setup: KeyMasked = %q, want depsilo-*** (key should load from DB)", m.Status().KeyMasked)
	}

	if err := m.ClearKey(context.Background(), 7); err != nil {
		t.Fatalf("ClearKey: %v", err)
	}

	var count int64
	if err := d.Model(&db.LicenseStorage{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("LicenseStorage count = %d, want 0", count)
	}
	if m.Status().KeyMasked != "" {
		t.Errorf("KeyMasked = %q after ClearKey, want empty", m.Status().KeyMasked)
	}
	if m.IsPro() {
		t.Error("IsPro() = true after ClearKey, want false")
	}
}

func TestClearKey_RecordsSuccessfulManagementAuditWithoutPriorLicenseKey(t *testing.T) {
	d := newTestDB(t)
	const rawKey = "depsilo-prior-secret-key"
	if err := d.Create(&db.LicenseStorage{ID: 1, Key: rawKey, UpdatedBy: 1}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := license.NewManager(config.LicenseConfig{}, d)

	if err := m.ClearKey(context.Background(), 9); err != nil {
		t.Fatalf("ClearKey: %v", err)
	}

	var entry db.AuditLog
	if err := d.Where("action = ?", "license.clear").First(&entry).Error; err != nil {
		t.Fatalf("find license management audit: %v", err)
	}
	if entry.Ecosystem != "admin" || entry.PackageName != "license" || entry.UserAgent != "operator:9" {
		t.Fatalf("audit identity = ecosystem:%q target:%q actor:%q", entry.Ecosystem, entry.PackageName, entry.UserAgent)
	}
	if entry.CacheResult != "success" || entry.StatusCode != 200 {
		t.Fatalf("audit result = %q status=%d, want success/200", entry.CacheResult, entry.StatusCode)
	}
	if strings.Contains(fmt.Sprintf("%+v", entry), rawKey) || strings.Contains(fmt.Sprintf("%+v", entry), "prior-secret") {
		t.Fatalf("audit entry leaked prior license key material: %+v", entry)
	}
}

func TestNewManager_DBKeyOverridesConfigKey(t *testing.T) {
	d := newTestDB(t)
	if err := d.Create(&db.LicenseStorage{ID: 1, Key: "fromdb-key-xxxx", UpdatedBy: 1}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	m := license.NewManager(config.LicenseConfig{Key: "cfgkey-yyyyyy"}, d)
	// Masks differ by prefix: "fromdb-***" vs "cfgkey-***"
	if got := m.Status().KeyMasked; got != "fromdb-***" {
		t.Errorf("KeyMasked = %q, want %q (DB key should win over config key)", got, "fromdb-***")
	}
}

func TestRevalidateWithRuntimeDeduplicatesConcurrentRequests(t *testing.T) {
	t.Setenv("DEPSILO_DEV_PRO", "0")
	runner := &queuedTaskRunner{}
	m := license.NewManagerWithSubmitter(
		runner,
		config.LicenseConfig{Key: "depsilo-runtime-key"},
		newTestDB(t),
	)

	const callers = 64
	start := make(chan struct{})
	var accepted atomic.Int32
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			<-start
			if m.Revalidate() == nil {
				accepted.Add(1)
			}
		}()
	}
	close(start)
	wait.Wait()

	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted revalidations = %d, want 1", got)
	}
	if got := runner.Len(); got != 1 {
		t.Fatalf("queued revalidation tasks = %d, want 1", got)
	}
	runner.RunNext(t, context.Background())
	if err := m.Revalidate(); err != nil {
		t.Fatalf("revalidation reservation was not released after task completion: %v", err)
	}
}

func TestRevalidateWithRuntimeObservesCancellationAndReleasesReservation(t *testing.T) {
	t.Setenv("DEPSILO_DEV_PRO", "0")
	runner := &queuedTaskRunner{}
	m := license.NewManagerWithSubmitter(
		runner,
		config.LicenseConfig{Key: "depsilo-runtime-key"},
		newTestDB(t),
	)
	before := m.Status().LastChecked

	if err := m.Revalidate(); err != nil {
		t.Fatalf("first revalidation was not accepted: %v", err)
	}
	taskContext, cancelTask := context.WithCancel(context.Background())
	cancelTask()
	runner.RunNext(t, taskContext)
	if after := m.Status().LastChecked; !after.Equal(before) {
		t.Fatalf("cancelled revalidation changed LastChecked: before=%v after=%v", before, after)
	}
	if err := m.Revalidate(); err != nil {
		t.Fatalf("cancelled revalidation did not release its reservation: %v", err)
	}
}

func TestRevalidateWithoutKeyDoesNotSchedule(t *testing.T) {
	t.Setenv("DEPSILO_DEV_PRO", "0")
	runner := &queuedTaskRunner{}
	m := license.NewManagerWithSubmitter(runner, config.LicenseConfig{}, newTestDB(t))
	if err := m.Revalidate(); !errors.Is(err, license.ErrNoLicenseKey) {
		t.Fatalf("Revalidate error = %v, want ErrNoLicenseKey", err)
	}
	if got := runner.Len(); got != 0 {
		t.Fatalf("queued revalidation tasks = %d, want 0", got)
	}
}

func TestRevalidateRollsBackReservationWhenRuntimeRejects(t *testing.T) {
	t.Setenv("DEPSILO_DEV_PRO", "0")
	runner := &queuedTaskRunner{submitErr: asyncruntime.ErrClosed}
	m := license.NewManagerWithSubmitter(runner, config.LicenseConfig{Key: "depsilo-runtime-key"}, newTestDB(t))

	err := m.Revalidate()
	if !errors.Is(err, license.ErrRevalidationClosed) || !errors.Is(err, asyncruntime.ErrClosed) {
		t.Fatalf("Revalidate error = %v, want closed runtime errors", err)
	}
	runner.mu.Lock()
	runner.submitErr = nil
	runner.mu.Unlock()
	if err := m.Revalidate(); err != nil {
		t.Fatalf("revalidation reservation was not rolled back: %v", err)
	}
}
