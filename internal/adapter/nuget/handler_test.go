package nuget

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

func TestPackageUpstreamFailureIsRecordedAsAuditError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "nuget-error.db"))
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
	handler.Register(router.Group("/nuget"))
	response := httptest.NewRecorder()
	target := "/nuget/v3-flatcontainer/newtonsoft.json/13.0.3/newtonsoft.json.13.0.3.nupkg"
	request := httptest.NewRequest(http.MethodGet, target, nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
	}
	if len(audit.entries) != 1 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	entry := audit.entries[0]
	if entry.Ecosystem != "nuget" || entry.PackageName != "newtonsoft.json" || entry.Version != "13.0.3" ||
		entry.Action != "download" || entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
		t.Fatalf("audit entry = %#v", entry)
	}
}
