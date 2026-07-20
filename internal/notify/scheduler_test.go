package notify

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"depsilo/internal/asyncruntime"
)

func TestItoa(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{7, "7"},
		{14, "14"},
		{365, "365"},
		{-1, "-1"},
	}
	for _, tt := range tests {
		got := itoa(tt.n)
		if got != tt.want {
			t.Errorf("itoa(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestLogDispatchAdmissionOnlyLogsOperationalFailures(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	undo := zap.ReplaceGlobals(zap.New(core))
	t.Cleanup(undo)
	event := Event{Type: EventUpstreamDown}

	logDispatchAdmission(context.Background(), event, nil)
	logDispatchAdmission(context.Background(), event, asyncruntime.ErrClosed)
	logDispatchAdmission(context.Background(), event, context.Canceled)
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	logDispatchAdmission(cancelledContext, event, errors.New("ignored during shutdown"))
	logDispatchAdmission(context.Background(), event, errors.New("submitter failed"))

	if got := logs.Len(); got != 1 {
		t.Fatalf("warning logs = %d, want 1", got)
	}
	if got := logs.All()[0].ContextMap()["event_type"]; got != EventUpstreamDown {
		t.Fatalf("event_type field = %v, want %q", got, EventUpstreamDown)
	}
}
