package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParentCancellationDrainsHandlersBeforeCancellingRuntime(t *testing.T) {
	trigger, cancelTrigger := context.WithCancel(context.Background())
	defer cancelTrigger()
	serverCtx, cancelServer := newServerRuntimeContext(trigger)
	defer cancelServer()

	resources := newServerResources(cancelServer, nil)
	srv := &http.Server{}
	lifecycle := registerServerLifecycle(srv, resources)
	entered := make(chan struct{})
	release := make(chan struct{})
	srv.Handler = lifecycle.track(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	}))

	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		srv.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}()
	awaitServerSignal(t, entered, "handler entry")

	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		shutdownWhenContextEnds(trigger, serverCtx, srv)
	}()
	cancelTrigger()

	select {
	case <-serverCtx.Done():
		t.Fatal("server runtime was cancelled before the active handler drained")
	case <-time.After(30 * time.Millisecond):
	}

	close(release)
	awaitServerSignal(t, requestDone, "handler completion")
	awaitServerSignal(t, serverCtx.Done(), "runtime cancellation")
	awaitServerSignal(t, watcherDone, "parent shutdown watcher")
}

func awaitServerSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
