package cargo

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

type unavailableSelector struct{}

func (unavailableSelector) Select(context.Context) (*upstream.Upstream, error) {
	return nil, errors.New("test upstream unavailable")
}

type auditCapture struct {
	entries []db.AuditLog
}

func (capture *auditCapture) Log(entry db.AuditLog) {
	capture.entries = append(capture.entries, entry)
}

func TestPackageUpstreamFailuresAreRecordedAsAuditErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "cargo-error.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Errorf("close cache manager: %v", err)
		}
	})
	audit := &auditCapture{}
	release := adapter.InstallAccessHooks(nil, audit)
	t.Cleanup(release)

	handler := New(manager, unavailableSelector{}, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/crates"))
	for _, target := range []string{
		"/crates/se/rd/serde",
		"/crates/api/v1/crates/serde/1.0.219/download",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
		}
	}

	if len(audit.entries) != 2 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	for index, entry := range audit.entries {
		if entry.Ecosystem != "cargo" || entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
			t.Errorf("audit entry %d = %#v", index, entry)
		}
	}
	if audit.entries[0].Action != "metadata" || audit.entries[0].PackageName != "" || audit.entries[0].Version != "" {
		t.Errorf("index audit entry = %#v", audit.entries[0])
	}
	if audit.entries[1].Action != "download" || audit.entries[1].PackageName != "serde" || audit.entries[1].Version != "1.0.219" {
		t.Errorf("download audit entry = %#v", audit.entries[1])
	}
}
