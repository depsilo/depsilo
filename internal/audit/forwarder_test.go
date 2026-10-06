package audit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/db"
)

type collectorStub struct {
	mu       sync.Mutex
	requests []collectedBatch
	status   int
	failures int
}

type collectedBatch struct {
	contentType string
	auth        string
	body        string
}

func (s *collectorStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		status := s.status
		if s.failures > 0 {
			s.failures--
			status = http.StatusInternalServerError
		}
		if status == 0 {
			status = http.StatusOK
		}
		s.requests = append(s.requests, collectedBatch{
			contentType: r.Header.Get("Content-Type"),
			auth:        r.Header.Get("Authorization"),
			body:        string(raw),
		})
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server
}

func (s *collectorStub) batches() []collectedBatch {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]collectedBatch(nil), s.requests...)
}

func TestForwarderDeliversNDJSONAndAdvancesCursor(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "forwarder.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	rows := []db.AuditLog{
		{Ecosystem: "pypi", PackageName: "requests", Version: "2.32.4", Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()},
		{Ecosystem: "npm", PackageName: "left-pad", Version: "1.3.0", Action: "download", CacheResult: "miss", StatusCode: 200, CreatedAt: time.Now().UTC()},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	stub := &collectorStub{}
	server := stub.server(t)
	exporter := db.AuditExporter{
		Name: "siem", Kind: db.AuditExporterKindNDJSON, URL: server.URL, Token: "secret", Events: "*", Enabled: true,
	}
	if err := database.Create(&exporter).Error; err != nil {
		t.Fatal(err)
	}

	forwarder := NewForwarder(database)
	delivered, err := forwarder.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2", delivered)
	}
	batches := stub.batches()
	if len(batches) != 1 {
		t.Fatalf("batches = %d, want 1", len(batches))
	}
	if batches[0].contentType != "application/x-ndjson" || batches[0].auth != "Bearer secret" {
		t.Fatalf("batch headers = %+v", batches[0])
	}
	lines := strings.Split(strings.TrimSpace(batches[0].body), "\n")
	if len(lines) != 2 {
		t.Fatalf("ndjson lines = %d, want 2", len(lines))
	}
	var first db.AuditLog
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if first.PackageName != "requests" || first.CacheResult != "hit" {
		t.Fatalf("first row = %+v", first)
	}

	var reloaded db.AuditExporter
	if err := database.First(&reloaded, exporter.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Cursor != rows[len(rows)-1].ID || reloaded.DeliveredCount != 2 {
		t.Fatalf("exporter state = %+v", reloaded)
	}
	if reloaded.LastError != "" || reloaded.LastSuccessAt == nil {
		t.Fatalf("exporter status = %+v", reloaded)
	}

	// Nothing new: a second run is a no-op.
	delivered, err = forwarder.RunOnce(context.Background())
	if err != nil || delivered != 0 {
		t.Fatalf("second run delivered=%d err=%v", delivered, err)
	}
	if len(stub.batches()) != 1 {
		t.Fatalf("second run sent another batch: %+v", stub.batches())
	}
}

func TestForwarderKeepsCursorOnCollectorFailureAndBacksOff(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "forwarder-failure.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	row := db.AuditLog{Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	stub := &collectorStub{status: http.StatusInternalServerError}
	server := stub.server(t)
	exporter := db.AuditExporter{
		Name: "siem", Kind: db.AuditExporterKindNDJSON, URL: server.URL, Events: "*", Enabled: true,
	}
	if err := database.Create(&exporter).Error; err != nil {
		t.Fatal(err)
	}

	forwarder := NewForwarder(database)
	if _, err := forwarder.RunOnce(context.Background()); err == nil {
		t.Fatal("failing collector reported success")
	}
	var failed db.AuditExporter
	if err := database.First(&failed, exporter.ID).Error; err != nil {
		t.Fatal(err)
	}
	if failed.Cursor != 0 || !strings.Contains(failed.LastError, "500") || failed.LastAttemptAt == nil {
		t.Fatalf("failed exporter state = %+v", failed)
	}

	// Backoff: the immediate next run must not hammer the collector.
	if _, err := forwarder.RunOnce(context.Background()); err != nil {
		t.Fatalf("backoff run errored: %v", err)
	}
	if got := len(stub.batches()); got != 1 {
		t.Fatalf("collector saw %d attempts during backoff, want 1", got)
	}

	// Once the backoff elapses and the collector recovers, the row is delivered.
	stub.mu.Lock()
	stub.status = http.StatusOK
	stub.mu.Unlock()
	forwarder.now = func() time.Time { return time.Now().UTC().Add(2 * forwarderMinBackoff) }
	if _, err := forwarder.RunOnce(context.Background()); err != nil {
		t.Fatalf("recovery run: %v", err)
	}
	var recovered db.AuditExporter
	if err := database.First(&recovered, exporter.ID).Error; err != nil {
		t.Fatal(err)
	}
	if recovered.Cursor != row.ID || recovered.DeliveredCount != 1 || recovered.LastError != "" {
		t.Fatalf("recovered exporter = %+v", recovered)
	}
}

func TestForwarderFiltersEventsButStillAdvancesCursor(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "forwarder-filter.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	rows := []db.AuditLog{
		{Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Now().UTC()},
		{Action: "download", CacheResult: "blocked", StatusCode: 451, CreatedAt: time.Now().UTC()},
		{Action: "snapshot_create", CacheResult: "", StatusCode: 201, CreatedAt: time.Now().UTC()},
	}
	if err := database.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	stub := &collectorStub{}
	server := stub.server(t)
	exporter := db.AuditExporter{
		Name: "security-only", Kind: db.AuditExporterKindNDJSON, URL: server.URL,
		Events: "blocked,snapshot_create", Enabled: true,
	}
	if err := database.Create(&exporter).Error; err != nil {
		t.Fatal(err)
	}

	forwarder := NewForwarder(database)
	delivered, err := forwarder.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if delivered != 2 {
		t.Fatalf("delivered = %d, want 2 filtered rows", delivered)
	}
	batches := stub.batches()
	if len(batches) != 1 || strings.Count(strings.TrimSpace(batches[0].body), "\n") != 1 {
		t.Fatalf("filtered batch = %+v", batches)
	}
	var reloaded db.AuditExporter
	if err := database.First(&reloaded, exporter.ID).Error; err != nil {
		t.Fatal(err)
	}
	if reloaded.Cursor != rows[len(rows)-1].ID {
		t.Fatalf("cursor = %d, want %d (filtered rows still advance)", reloaded.Cursor, rows[len(rows)-1].ID)
	}
}

func TestForwarderSplunkHECEnvelope(t *testing.T) {
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "forwarder-splunk.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	row := db.AuditLog{Action: "download", CacheResult: "hit", StatusCode: 200, CreatedAt: time.Unix(1_786_032_000, 0).UTC()}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	stub := &collectorStub{}
	server := stub.server(t)
	exporter := db.AuditExporter{
		Name: "splunk", Kind: db.AuditExporterKindSplunkHEC, URL: server.URL + "/services/collector/event",
		Token: "hec-token", Events: "*", Enabled: true,
	}
	if err := database.Create(&exporter).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := NewForwarder(database).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	batches := stub.batches()
	if len(batches) != 1 || batches[0].auth != "Splunk hec-token" || batches[0].contentType != "application/json" {
		t.Fatalf("splunk batch = %+v", batches)
	}
	var envelope struct {
		Time       int64          `json:"time"`
		Source     string         `json:"source"`
		Sourcetype string         `json:"sourcetype"`
		Event      map[string]any `json:"event"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(batches[0].body)), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Time != 1_786_032_000 || envelope.Source != "depsilo" || envelope.Sourcetype != "depsilo:audit" {
		t.Fatalf("splunk envelope = %+v", envelope)
	}
	if envelope.Event["action"] != "download" {
		t.Fatalf("splunk event = %+v", envelope.Event)
	}
}
