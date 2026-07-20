package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/db"
)

type queuedSubmitter struct {
	mu          sync.Mutex
	tasks       []asyncruntime.Task
	reject      error
	submitCalls int
}

func (submitter *queuedSubmitter) Submit(task asyncruntime.Task) error {
	submitter.mu.Lock()
	defer submitter.mu.Unlock()
	submitter.submitCalls++
	if submitter.reject != nil {
		return submitter.reject
	}
	submitter.tasks = append(submitter.tasks, task)
	return nil
}

func (submitter *queuedSubmitter) SetReject(err error) {
	submitter.mu.Lock()
	submitter.reject = err
	submitter.mu.Unlock()
}

func (submitter *queuedSubmitter) Len() int {
	submitter.mu.Lock()
	defer submitter.mu.Unlock()
	return len(submitter.tasks)
}

func (submitter *queuedSubmitter) SubmitCalls() int {
	submitter.mu.Lock()
	defer submitter.mu.Unlock()
	return submitter.submitCalls
}

func (submitter *queuedSubmitter) RunNext(t *testing.T, ctx context.Context) {
	t.Helper()
	submitter.mu.Lock()
	if len(submitter.tasks) == 0 {
		submitter.mu.Unlock()
		t.Fatal("no queued task")
	}
	task := submitter.tasks[0]
	submitter.tasks = submitter.tasks[1:]
	submitter.mu.Unlock()
	task(ctx)
}

func TestDispatchWithRuntimeIsNonBlockingAndCancellationRollsBack(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseHandler := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseHandler) }) }
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		select {
		case <-request.Context().Done():
		case <-releaseHandler:
		}
	}))
	t.Cleanup(server.Close)
	// Registered after Server.Close so LIFO cleanup releases a handler whose
	// server-side request context may outlive the cancelled client request.
	t.Cleanup(release)

	database := newNotifierTestDB(t)
	config := db.WebhookConfig{
		Name:            "lifecycle-test",
		Platform:        "generic",
		URL:             server.URL,
		Enabled:         true,
		Events:          "*",
		CooldownMinutes: 30,
	}
	if err := database.Create(&config).Error; err != nil {
		t.Fatalf("create webhook config: %v", err)
	}

	runtime := newNotifierTestRuntime(t)
	notifier := New(database, runtime)
	if err := notifier.LoadConfigs(t.Context()); err != nil {
		t.Fatalf("load webhook configs: %v", err)
	}

	returned := make(chan struct{})
	go func() {
		if err := notifier.Dispatch(testEvent()); err != nil {
			t.Errorf("dispatch: %v", err)
		}
		close(returned)
	}()
	waitForSignal(t, returned, "non-blocking Dispatch return")
	waitForSignal(t, requestStarted, "webhook request start")

	closeContext, cancelClose := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelClose()
	if err := runtime.Close(closeContext); err != nil {
		t.Fatalf("close runtime: %v", err)
	}

	notifier.mu.Lock()
	reservationCount := len(notifier.reservations)
	lastSentAt := cloneTime(notifier.configs[0].LastSentAt)
	notifier.mu.Unlock()
	if reservationCount != 0 {
		t.Fatalf("reservations after cancellation = %d, want 0", reservationCount)
	}
	if lastSentAt != nil {
		t.Fatalf("in-memory cooldown after cancellation = %v, want nil", lastSentAt)
	}
	var stored db.WebhookConfig
	if err := database.First(&stored, config.ID).Error; err != nil {
		t.Fatalf("load webhook config: %v", err)
	}
	if stored.LastSentAt != nil {
		t.Fatalf("persisted cooldown after cancellation = %v, want nil", stored.LastSentAt)
	}
	if err := notifier.Dispatch(testEvent()); !errors.Is(err, asyncruntime.ErrClosed) {
		t.Fatalf("dispatch after runtime close = %v, want ErrClosed", err)
	}
}

func TestDispatchReservesCooldownAcrossConcurrentCallsAndReload(t *testing.T) {
	var hits atomic.Int32
	requestStarted := make(chan struct{})
	requestFinished := make(chan struct{})
	allowResponse := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(allowResponse) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		hit := hits.Add(1)
		if hit == 1 {
			close(requestStarted)
		}
		select {
		case <-allowResponse:
		case <-request.Context().Done():
			return
		}
		w.WriteHeader(http.StatusNoContent)
		if hit == 1 {
			close(requestFinished)
		}
	}))
	t.Cleanup(server.Close)
	// Registered after server.Close so LIFO cleanup releases blocked handlers
	// before waiting for the server to stop, including on an early test failure.
	t.Cleanup(release)

	database := newNotifierTestDB(t)
	runtime := newNotifierTestRuntime(t)
	config := db.WebhookConfig{
		Name:            "cooldown-test",
		Platform:        "generic",
		URL:             server.URL,
		Enabled:         true,
		Events:          "*",
		CooldownMinutes: 30,
	}
	if err := database.Create(&config).Error; err != nil {
		t.Fatalf("create webhook config: %v", err)
	}
	notifier := New(database, runtime)
	if err := notifier.LoadConfigs(t.Context()); err != nil {
		t.Fatalf("load webhook configs: %v", err)
	}

	mustDispatch(t, notifier, testEvent())
	waitForSignal(t, requestStarted, "first webhook request")

	const dispatchers = 64
	start := make(chan struct{})
	var dispatchWG sync.WaitGroup
	dispatchWG.Add(dispatchers)
	for range dispatchers {
		go func() {
			defer dispatchWG.Done()
			<-start
			if err := notifier.Dispatch(testEvent()); err != nil {
				t.Errorf("concurrent dispatch: %v", err)
			}
		}()
	}
	close(start)
	dispatchWG.Wait()

	// A config refresh while the first HTTP request is still in flight must not
	// replace the reservation with the stale database value.
	if err := notifier.LoadConfigs(t.Context()); err != nil {
		t.Fatalf("reload webhook configs: %v", err)
	}
	mustDispatch(t, notifier, testEvent())
	time.Sleep(50 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("concurrent dispatches sent %d requests, want 1", got)
	}

	release()
	waitForSignal(t, requestFinished, "completed webhook request")
	waitForCondition(t, "persisted cooldown", func() bool {
		var stored db.WebhookConfig
		return database.First(&stored, config.ID).Error == nil && stored.LastSentAt != nil
	})

	// The successful reservation is also the in-memory cooldown, so a closely
	// following Dispatch must not need a reload to observe it.
	mustDispatch(t, notifier, testEvent())
	time.Sleep(50 * time.Millisecond)
	if got := hits.Load(); got != 1 {
		t.Fatalf("dispatch inside cooldown sent %d requests, want 1", got)
	}
}

func TestDispatchFailureRollsBackReservationForRetry(t *testing.T) {
	var hits atomic.Int32
	firstRequest := make(chan struct{})
	secondRequest := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch hits.Add(1) {
		case 1:
			w.WriteHeader(http.StatusBadGateway)
			close(firstRequest)
		case 2:
			w.WriteHeader(http.StatusNoContent)
			close(secondRequest)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(server.Close)

	database := newNotifierTestDB(t)
	runtime := newNotifierTestRuntime(t)
	previousSentAt := time.Now().Add(-2 * time.Minute).UTC()
	config := db.WebhookConfig{
		Name:            "retry-test",
		Platform:        "generic",
		URL:             server.URL,
		Enabled:         true,
		Events:          "*",
		CooldownMinutes: 1,
		LastSentAt:      &previousSentAt,
	}
	if err := database.Create(&config).Error; err != nil {
		t.Fatalf("create webhook config: %v", err)
	}
	notifier := New(database, runtime)
	if err := notifier.LoadConfigs(t.Context()); err != nil {
		t.Fatalf("load webhook configs: %v", err)
	}

	mustDispatch(t, notifier, testEvent())
	waitForSignal(t, firstRequest, "failed webhook request")
	waitForCondition(t, "failed reservation rollback", func() bool {
		notifier.mu.Lock()
		defer notifier.mu.Unlock()
		if len(notifier.reservations) != 0 || len(notifier.configs) != 1 {
			return false
		}
		lastSentAt := notifier.configs[0].LastSentAt
		return lastSentAt != nil && lastSentAt.Equal(previousSentAt)
	})

	var storedAfterFailure db.WebhookConfig
	if err := database.First(&storedAfterFailure, config.ID).Error; err != nil {
		t.Fatalf("load failed webhook config: %v", err)
	}
	if storedAfterFailure.LastSentAt == nil || !storedAfterFailure.LastSentAt.Equal(previousSentAt) {
		t.Fatalf("failed delivery changed persisted cooldown: got %v, want %v", storedAfterFailure.LastSentAt, previousSentAt)
	}

	mustDispatch(t, notifier, testEvent())
	waitForSignal(t, secondRequest, "retried webhook request")
	waitForCondition(t, "successful retry cooldown", func() bool {
		var stored db.WebhookConfig
		if database.First(&stored, config.ID).Error != nil || stored.LastSentAt == nil {
			return false
		}
		return stored.LastSentAt.After(previousSentAt)
	})
	if got := hits.Load(); got != 2 {
		t.Fatalf("failure and retry sent %d requests, want 2", got)
	}
}

func TestDispatchRejectionRollsBackEveryTargetAndCanRetry(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	database := newNotifierTestDB(t)
	configs := []db.WebhookConfig{
		{Name: "first", Platform: "generic", URL: server.URL, Enabled: true, Events: "*", CooldownMinutes: 30},
		{Name: "second", Platform: "generic", URL: server.URL, Enabled: true, Events: "*", CooldownMinutes: 30},
	}
	if err := database.Create(&configs).Error; err != nil {
		t.Fatalf("create webhook configs: %v", err)
	}

	submitter := &queuedSubmitter{reject: asyncruntime.ErrClosed}
	notifier := New(database, submitter)
	if err := notifier.LoadConfigs(t.Context()); err != nil {
		t.Fatalf("load webhook configs: %v", err)
	}

	if err := notifier.Dispatch(testEvent()); !errors.Is(err, asyncruntime.ErrClosed) {
		t.Fatalf("rejected dispatch error = %v, want ErrClosed", err)
	}
	if got := submitter.SubmitCalls(); got != 1 {
		t.Fatalf("submit calls after rejection = %d, want one parent admission", got)
	}
	if got := submitter.Len(); got != 0 {
		t.Fatalf("queued tasks after rejection = %d, want 0", got)
	}
	assertReservationsRolledBack(t, notifier)
	if got := hits.Load(); got != 0 {
		t.Fatalf("HTTP requests after rejected admission = %d, want 0", got)
	}

	// A rejected parent never ran, so every target is immediately retryable.
	submitter.SetReject(nil)
	mustDispatch(t, notifier, testEvent())
	if got := submitter.SubmitCalls(); got != 2 {
		t.Fatalf("submit calls after retry = %d, want 2", got)
	}
	if got := submitter.Len(); got != 1 {
		t.Fatalf("queued tasks for two targets = %d, want one joined parent", got)
	}
	submitter.RunNext(t, t.Context())
	if got := hits.Load(); got != int32(len(configs)) {
		t.Fatalf("HTTP requests after retry = %d, want %d", got, len(configs))
	}
	assertNoActiveReservations(t, notifier)

	var stored []db.WebhookConfig
	if err := database.Order("id ASC").Find(&stored).Error; err != nil {
		t.Fatalf("load persisted webhook configs: %v", err)
	}
	for _, config := range stored {
		if config.LastSentAt == nil {
			t.Fatalf("webhook %q did not persist its cooldown", config.Name)
		}
	}
}

func TestDispatchToOnlyDeliversPassedConfigAndDoesNotTouchCooldown(t *testing.T) {
	var targetHits atomic.Int32
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(targetServer.Close)
	var otherHits atomic.Int32
	otherServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(otherServer.Close)

	database := newNotifierTestDB(t)
	previousSentAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	target := db.WebhookConfig{
		Name:            "disabled-direct-target",
		Platform:        "generic",
		URL:             targetServer.URL,
		Enabled:         false,
		Events:          EventDiskHigh,
		CooldownMinutes: 24 * 60,
		LastSentAt:      &previousSentAt,
	}
	other := db.WebhookConfig{
		Name: "enabled-broadcast-target", Platform: "generic", URL: otherServer.URL,
		Enabled: true, Events: "*", CooldownMinutes: 30,
	}
	if err := database.Create(&target).Error; err != nil {
		t.Fatalf("create direct target: %v", err)
	}
	if err := database.Create(&other).Error; err != nil {
		t.Fatalf("create other target: %v", err)
	}

	submitter := &queuedSubmitter{}
	notifier := New(database, submitter)
	if err := notifier.LoadConfigs(t.Context()); err != nil {
		t.Fatalf("load webhook configs: %v", err)
	}
	if err := notifier.DispatchTo(target, testEvent()); err != nil {
		t.Fatalf("dispatch directly: %v", err)
	}
	if got := submitter.Len(); got != 1 {
		t.Fatalf("direct tasks = %d, want 1", got)
	}
	submitter.RunNext(t, t.Context())

	if got := targetHits.Load(); got != 1 {
		t.Fatalf("direct target hits = %d, want 1", got)
	}
	if got := otherHits.Load(); got != 0 {
		t.Fatalf("non-target hits = %d, want 0", got)
	}
	if target.LastSentAt == nil || !target.LastSentAt.Equal(previousSentAt) {
		t.Fatalf("input cooldown changed to %v, want %v", target.LastSentAt, previousSentAt)
	}

	var storedTarget db.WebhookConfig
	if err := database.First(&storedTarget, target.ID).Error; err != nil {
		t.Fatalf("load direct target: %v", err)
	}
	if storedTarget.LastSentAt == nil || !storedTarget.LastSentAt.Equal(previousSentAt) {
		t.Fatalf("persisted direct-target cooldown = %v, want %v", storedTarget.LastSentAt, previousSentAt)
	}
	var storedOther db.WebhookConfig
	if err := database.First(&storedOther, other.ID).Error; err != nil {
		t.Fatalf("load non-target: %v", err)
	}
	if storedOther.LastSentAt != nil {
		t.Fatalf("non-target cooldown changed to %v", storedOther.LastSentAt)
	}
	assertNoActiveReservations(t, notifier)
}

func TestDispatchToClosedRuntimeReturnsErrClosedWithoutDelivery(t *testing.T) {
	requestReceived := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestReceived <- struct{}{}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	runtime := asyncruntime.New(context.Background())
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("close runtime: %v", err)
	}
	notifier := New(nil, runtime)
	config := db.WebhookConfig{
		Name: "closed-runtime-target", Platform: "generic", URL: server.URL,
		Enabled: false, Events: EventDiskHigh, CooldownMinutes: 30,
	}
	if err := notifier.DispatchTo(config, testEvent()); !errors.Is(err, asyncruntime.ErrClosed) {
		t.Fatalf("DispatchTo error = %v, want ErrClosed", err)
	}
	select {
	case <-requestReceived:
		t.Fatal("closed runtime executed a rejected direct delivery")
	case <-time.After(25 * time.Millisecond):
	}
}

func TestDispatchWithNilSubmitterReturnsAdmissionErrorAndRollsBack(t *testing.T) {
	database := newNotifierTestDB(t)
	config := db.WebhookConfig{
		Name: "missing-runtime", Platform: "generic", URL: "https://hooks.example.test",
		Enabled: true, Events: "*", CooldownMinutes: 30,
	}
	if err := database.Create(&config).Error; err != nil {
		t.Fatalf("create webhook config: %v", err)
	}

	notifier := New(database, nil)
	if err := notifier.LoadConfigs(t.Context()); err != nil {
		t.Fatalf("load webhook configs: %v", err)
	}
	if err := notifier.Dispatch(testEvent()); !errors.Is(err, errNilSubmitter) {
		t.Fatalf("dispatch error = %v, want nil-submitter admission error", err)
	}
	assertReservationsRolledBack(t, notifier)
}

func TestLoadConfigsHonorsCancellation(t *testing.T) {
	database := newNotifierTestDB(t)
	notifier := New(database, &queuedSubmitter{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := notifier.LoadConfigs(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadConfigs error = %v, want context.Canceled", err)
	}
}

func newNotifierTestRuntime(t *testing.T) *asyncruntime.Runtime {
	t.Helper()
	runtime := asyncruntime.New(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := runtime.Close(ctx); err != nil {
			t.Errorf("close async runtime: %v", err)
		}
	})
	return runtime
}

func mustDispatch(t *testing.T, notifier *Notifier, event Event) {
	t.Helper()
	if err := notifier.Dispatch(event); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
}

func assertNoActiveReservations(t *testing.T, notifier *Notifier) {
	t.Helper()
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.reservations) != 0 {
		t.Fatalf("reservations = %d, want 0", len(notifier.reservations))
	}
}

func assertReservationsRolledBack(t *testing.T, notifier *Notifier) {
	t.Helper()
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	if len(notifier.reservations) != 0 {
		t.Fatalf("reservations = %d, want 0", len(notifier.reservations))
	}
	for _, config := range notifier.configs {
		if config.LastSentAt != nil {
			t.Fatalf("webhook %q retained in-memory cooldown %v", config.Name, config.LastSentAt)
		}
	}
}

func newNotifierTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "notify.db"))
	if err != nil {
		t.Fatalf("open notifier test database: %v", err)
	}
	if err := database.AutoMigrate(&db.WebhookConfig{}); err != nil {
		t.Fatalf("migrate webhook config: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("access notifier test database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close notifier test database: %v", err)
		}
	})
	return database
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForCondition(t *testing.T, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}
