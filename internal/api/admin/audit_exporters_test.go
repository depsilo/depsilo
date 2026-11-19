package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/audit"
	"depsilo/internal/db"
)

func newAuditExporterTestRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "audit-exporters.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	rows := []db.AuditLog{
		{Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()},
		{Action: "download", CacheResult: "miss", StatusCode: 200, CreatedAt: time.Now().UTC()},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	handler := NewAuditExporterHandler(database, audit.NewForwarder(database))
	router := gin.New()
	router.GET("/audit/exporters", handler.List)
	router.POST("/audit/exporters", handler.Create)
	router.PUT("/audit/exporters/:id", handler.Update)
	router.DELETE("/audit/exporters/:id", handler.Delete)
	router.POST("/audit/exporters/:id/test", handler.Test)
	return router, database
}

func TestAuditExporterCRUDStartsAtTheAuditHead(t *testing.T) {
	router, database := newAuditExporterTestRouter(t)

	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/audit/exporters",
		bytes.NewBufferString(`{"name":"splunk","kind":"splunk_hec","url":"https://splunk.example:8088/services/collector/event","token":"hec-secret","events":"blocked, Download ,download"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(create.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["cursor"] != float64(2) {
		t.Fatalf("new exporter cursor = %v, want the audit head 2", created["cursor"])
	}
	if created["events"] != "blocked,download" {
		t.Fatalf("normalized events = %v", created["events"])
	}
	if created["token_set"] != true {
		t.Fatalf("token_set = %v", created["token_set"])
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/audit/exporters", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var listed struct {
		Items []struct {
			ID       uint   `json:"id"`
			URL      string `json:"url"`
			Cursor   uint   `json:"cursor"`
			Lag      int64  `json:"lag"`
			TokenSet bool   `json:"token_set"`
		} `json:"items"`
		AuditHead uint `json:"audit_head"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	// Creating the exporter wrote its own management audit row, so the head
	// moved past the cursor the exporter started from.
	if listed.AuditHead != 3 || len(listed.Items) != 1 {
		t.Fatalf("listed = %+v", listed)
	}
	if listed.Items[0].Lag != 1 {
		t.Fatalf("new exporter lag = %d, want 1 (its own creation row)", listed.Items[0].Lag)
	}
	// A request without a write principal must not see the collector URL
	// (HEC endpoints often carry credentials in the path).
	if listed.Items[0].URL == "https://splunk.example:8088/services/collector/event" {
		t.Fatalf("read-only list leaked the exporter URL: %s", listed.Items[0].URL)
	}

	update := httptest.NewRecorder()
	router.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/audit/exporters/1",
		bytes.NewBufferString(`{"enabled":false,"events":"*"}`)))
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), `"enabled":false`) {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
	var reloaded db.AuditExporter
	if err := database.First(&reloaded, 1).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Cursor != 2 || reloaded.Token != "hec-secret" {
		t.Fatalf("update changed cursor/token: %+v", reloaded)
	}

	remove := httptest.NewRecorder()
	router.ServeHTTP(remove, httptest.NewRequest(http.MethodDelete, "/audit/exporters/1", nil))
	if remove.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", remove.Code, remove.Body.String())
	}
}

func TestAuditExporterValidationAndTestDelivery(t *testing.T) {
	router, _ := newAuditExporterTestRouter(t)

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/audit/exporters",
		bytes.NewBufferString(`{"name":"bad","kind":"syslog","url":"ftp://example"}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(collector.Close)
	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/audit/exporters",
		bytes.NewBufferString(`{"name":"ndjson","kind":"ndjson","url":"`+collector.URL+`"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	test := httptest.NewRecorder()
	router.ServeHTTP(test, httptest.NewRequest(http.MethodPost, "/audit/exporters/1/test", nil))
	if test.Code != http.StatusOK || !strings.Contains(test.Body.String(), `"delivered":true`) {
		t.Fatalf("test delivery status=%d body=%s", test.Code, test.Body.String())
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(failing.Close)
	create = httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/audit/exporters",
		bytes.NewBufferString(`{"name":"down","kind":"ndjson","url":"`+failing.URL+`"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create failing exporter status=%d body=%s", create.Code, create.Body.String())
	}
	test = httptest.NewRecorder()
	router.ServeHTTP(test, httptest.NewRequest(http.MethodPost, "/audit/exporters/2/test", nil))
	if test.Code != http.StatusBadGateway || !strings.Contains(test.Body.String(), "DELIVERY_FAILED") {
		t.Fatalf("failing delivery status=%d body=%s", test.Code, test.Body.String())
	}
}

// An exporter created disabled must stay disabled: GORM writes the column
// default in place of a false bool, which would start delivery at once.
func TestAuditExporterCreateStoresTheRequestedEnabledState(t *testing.T) {
	router, database := newAuditExporterTestRouter(t)
	create := func(body string) (bool, bool) {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/audit/exporters", bytes.NewBufferString(body)))
		if recorder.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var created struct {
			ID      uint `json:"id"`
			Enabled bool `json:"enabled"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		var stored db.AuditExporter
		if err := database.First(&stored, created.ID).Error; err != nil {
			t.Fatal(err)
		}
		return created.Enabled, stored.Enabled
	}

	if answered, stored := create(`{"name":"off","kind":"ndjson","url":"https://collector.example/off","enabled":false}`); answered || stored {
		t.Fatalf("exporter created with enabled=false: answered %v, stored %v; want false for both", answered, stored)
	}
	if answered, stored := create(`{"name":"on","kind":"ndjson","url":"https://collector.example/on","enabled":true}`); !answered || !stored {
		t.Fatalf("exporter created with enabled=true: answered %v, stored %v; want true for both", answered, stored)
	}
	if answered, stored := create(`{"name":"default","kind":"ndjson","url":"https://collector.example/default"}`); !answered || !stored {
		t.Fatalf("exporter created without enabled: answered %v, stored %v; want the default true", answered, stored)
	}

	// Updates still switch an exporter either way.
	for _, step := range []struct {
		id      string
		enabled bool
	}{{"1", true}, {"2", false}} {
		update := httptest.NewRecorder()
		router.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/audit/exporters/"+step.id,
			bytes.NewBufferString(`{"enabled":`+strconv.FormatBool(step.enabled)+`}`)))
		if update.Code != http.StatusOK {
			t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
		}
		var stored db.AuditExporter
		if err := database.First(&stored, step.id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Enabled != step.enabled {
			t.Fatalf("exporter %s after update: enabled %v, want %v", step.id, stored.Enabled, step.enabled)
		}
	}
}

// Package request outcomes are access-log history, not audit events, so an
// event filter naming them would forward nothing; it is refused instead.
func TestAuditExporterRefusesRequestOutcomeEventFilters(t *testing.T) {
	router, _ := newAuditExporterTestRouter(t)
	for _, events := range []string{"hit", "blocked, MISS"} {
		create := httptest.NewRecorder()
		router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/audit/exporters",
			bytes.NewBufferString(`{"name":"feed","kind":"ndjson","url":"https://collector.example/intake","events":"`+events+`"}`)))
		if create.Code != http.StatusBadRequest || !strings.Contains(create.Body.String(), "access log") {
			t.Fatalf("events %q create status=%d body=%s", events, create.Code, create.Body.String())
		}
	}
	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/audit/exporters",
		bytes.NewBufferString(`{"name":"feed","kind":"ndjson","url":"https://collector.example/intake","events":"blocked,error"}`)))
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	update := httptest.NewRecorder()
	router.ServeHTTP(update, httptest.NewRequest(http.MethodPut, "/audit/exporters/1",
		bytes.NewBufferString(`{"events":"miss"}`)))
	if update.Code != http.StatusBadRequest {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
}
