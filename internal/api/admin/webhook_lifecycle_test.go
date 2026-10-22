package admin

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestWebhookCreateCooldownContract(t *testing.T) {
	tests := []struct {
		name         string
		cooldownJSON string
		wantStatus   int
		wantCooldown int
	}{
		{name: "omitted defaults to thirty", wantStatus: http.StatusCreated, wantCooldown: 30},
		{name: "zero defaults to thirty", cooldownJSON: `,"cooldown_minutes":0`, wantStatus: http.StatusCreated, wantCooldown: 30},
		{name: "minimum", cooldownJSON: `,"cooldown_minutes":5`, wantStatus: http.StatusCreated, wantCooldown: 5},
		{name: "maximum", cooldownJSON: `,"cooldown_minutes":1440`, wantStatus: http.StatusCreated, wantCooldown: 1440},
		{name: "negative", cooldownJSON: `,"cooldown_minutes":-1`, wantStatus: http.StatusUnprocessableEntity},
		{name: "below minimum", cooldownJSON: `,"cooldown_minutes":4`, wantStatus: http.StatusUnprocessableEntity},
		{name: "above maximum", cooldownJSON: `,"cooldown_minutes":1441`, wantStatus: http.StatusUnprocessableEntity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openCredentialTestDB(t, "webhook-create-cooldown.db", &db.WebhookConfig{})
			router := principalTestRouter(true)
			router.POST("/webhooks", NewWebhookHandler(database, nil).Create)
			body := `{"name":"alerts","platform":"generic","url":"https://hooks.example.test"` + test.cooldownJSON + `}`
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/webhooks", strings.NewReader(body)))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}

			var stored []db.WebhookConfig
			if err := database.Find(&stored).Error; err != nil {
				t.Fatal(err)
			}
			if test.wantStatus == http.StatusUnprocessableEntity {
				if !strings.Contains(response.Body.String(), `"code":"INVALID_SETTING"`) {
					t.Fatalf("missing cooldown validation code: %s", response.Body.String())
				}
				if len(stored) != 0 {
					t.Fatalf("rejected creation persisted %d webhooks", len(stored))
				}
				return
			}
			if len(stored) != 1 || stored[0].CooldownMinutes != test.wantCooldown {
				t.Fatalf("stored webhooks = %+v, want one with cooldown %d", stored, test.wantCooldown)
			}
			var result db.WebhookConfig
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.ID != stored[0].ID || result.CooldownMinutes != test.wantCooldown {
				t.Fatalf("response = %+v, want stored ID and cooldown %d", result, test.wantCooldown)
			}
		})
	}
}

func TestWebhookUpdateCooldownContract(t *testing.T) {
	tests := []struct {
		name         string
		cooldownJSON string
		wantStatus   int
		wantCooldown int
	}{
		{name: "omitted preserves cooldown", wantStatus: http.StatusOK, wantCooldown: 45},
		{name: "minimum", cooldownJSON: `,"cooldown_minutes":5`, wantStatus: http.StatusOK, wantCooldown: 5},
		{name: "maximum", cooldownJSON: `,"cooldown_minutes":1440`, wantStatus: http.StatusOK, wantCooldown: 1440},
		{name: "zero is not an update default", cooldownJSON: `,"cooldown_minutes":0`, wantStatus: http.StatusUnprocessableEntity},
		{name: "negative", cooldownJSON: `,"cooldown_minutes":-1`, wantStatus: http.StatusUnprocessableEntity},
		{name: "below minimum", cooldownJSON: `,"cooldown_minutes":4`, wantStatus: http.StatusUnprocessableEntity},
		{name: "above maximum", cooldownJSON: `,"cooldown_minutes":1441`, wantStatus: http.StatusUnprocessableEntity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := openCredentialTestDB(t, "webhook-update-cooldown.db", &db.WebhookConfig{})
			original := db.WebhookConfig{
				Name: "alerts", Platform: "generic", URL: "https://hooks.example.test/original",
				Events: "cache_eviction", CooldownMinutes: 45, Enabled: true,
			}
			if err := database.Create(&original).Error; err != nil {
				t.Fatal(err)
			}
			router := principalTestRouter(true)
			router.PUT("/webhooks/:id", NewWebhookHandler(database, nil).Update)
			body := `{"name":"changed","platform":"slack","url":"https://hooks.example.test/changed","events":"*","enabled":false` + test.cooldownJSON + `}`
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/webhooks/%d", original.ID), strings.NewReader(body)))
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			var stored db.WebhookConfig
			if err := database.First(&stored, original.ID).Error; err != nil {
				t.Fatal(err)
			}
			if test.wantStatus == http.StatusUnprocessableEntity {
				if !strings.Contains(response.Body.String(), `"code":"INVALID_SETTING"`) {
					t.Fatalf("missing cooldown validation code: %s", response.Body.String())
				}
				if stored.Name != original.Name || stored.Platform != original.Platform || stored.URL != original.URL ||
					stored.Events != original.Events || stored.Enabled != original.Enabled || stored.CooldownMinutes != original.CooldownMinutes ||
					!stored.UpdatedAt.Equal(original.UpdatedAt) {
					t.Fatalf("rejected update changed stored config: before = %+v, after = %+v", original, stored)
				}
				return
			}
			if stored.Name != "changed" || stored.Platform != "slack" || stored.URL != "https://hooks.example.test/changed" ||
				stored.Events != "*" || stored.Enabled || stored.CooldownMinutes != test.wantCooldown {
				t.Fatalf("accepted update not persisted: %+v; want cooldown %d", stored, test.wantCooldown)
			}
		})
	}
}
