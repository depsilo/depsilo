package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/asyncruntime"
	"depsilo/internal/db"
	"depsilo/internal/notify"
)

type recordingWebhookNotifier struct {
	dispatchErr error
	configs     []db.WebhookConfig
	events      []notify.Event
}

func (notifier *recordingWebhookNotifier) DispatchTo(config db.WebhookConfig, event notify.Event) error {
	notifier.configs = append(notifier.configs, config)
	notifier.events = append(notifier.events, event)
	return notifier.dispatchErr
}

func (*recordingWebhookNotifier) LoadConfigs(context.Context) error { return nil }

func TestWebhookTestReportsDispatchAdmission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		dispatchErr error
		wantStatus  int
		wantBody    string
	}{
		{name: "accepted", wantStatus: http.StatusAccepted, wantBody: `"status":"test queued"`},
		{name: "runtime closed", dispatchErr: asyncruntime.ErrClosed, wantStatus: http.StatusServiceUnavailable, wantBody: `"code":"SERVER_SHUTTING_DOWN"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openCredentialTestDB(t, strings.ReplaceAll(test.name, " ", "-")+".db", &db.WebhookConfig{})
			config := db.WebhookConfig{
				Name:     "admission-test",
				Platform: "generic",
				URL:      "https://hooks.example.test",
				Enabled:  true,
				Events:   "*",
			}
			if err := database.Create(&config).Error; err != nil {
				t.Fatalf("seed webhook: %v", err)
			}

			notifier := &recordingWebhookNotifier{dispatchErr: test.dispatchErr}
			handler := NewWebhookHandler(database, notifier)
			router := gin.New()
			router.POST("/webhooks/:id/test", handler.Test)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(
				http.MethodPost,
				"/webhooks/"+strconv.FormatUint(uint64(config.ID), 10)+"/test",
				nil,
			))

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), test.wantBody) {
				t.Fatalf("body = %s, want %s", recorder.Body.String(), test.wantBody)
			}
			if len(notifier.events) != 1 || notifier.events[0].Type != "test" {
				t.Fatalf("dispatched events = %#v, want one test event", notifier.events)
			}
			if len(notifier.configs) != 1 || notifier.configs[0].ID != config.ID {
				t.Fatalf("dispatch configs = %#v, want only config %d", notifier.configs, config.ID)
			}
		})
	}
}
