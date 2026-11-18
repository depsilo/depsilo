package composer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "composer-error.db"))
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
	scope := adapter.NewRequestScope(nil, audit, nil, nil)

	handler := New(manager, unavailableSelector{}, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/composer"))
	for _, target := range []string{
		"/composer/p2/monolog/monolog.json",
		"/composer/dist/monolog/monolog/3.8.1.0/abcdef.zip",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		scope.Wrap(router).ServeHTTP(response, request)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
		}
	}

	if len(audit.entries) != 2 {
		t.Fatalf("audit entries = %#v", audit.entries)
	}
	for index, entry := range audit.entries {
		if entry.Ecosystem != "composer" || entry.PackageName != "monolog/monolog" || entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
			t.Errorf("audit entry %d = %#v", index, entry)
		}
	}
	if audit.entries[0].Action != "metadata" {
		t.Errorf("metadata audit entry = %#v", audit.entries[0])
	}
	if audit.entries[1].Action != "download" {
		t.Errorf("dist audit entry = %#v", audit.entries[1])
	}
}

// X-Forwarded-Proto reaches the metadata and mirror URLs written into
// packages.json. Two chained proxies send a list, and any client can send
// anything, so the value must never become the URL scheme as it is.
func TestPackagesJSONURLsTakeOnlyAnHTTPSchemeFromForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/packages.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"metadata-url":"/p2/%package%.json","packages":[]}`)
	}))
	t.Cleanup(upstreamServer.Close)
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "composer-scheme.db"))
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
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPrioritySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/composer"))

	for _, test := range []struct {
		forwarded string
		want      string
	}{
		{forwarded: "https, http", want: "https://depsilo.example.test/composer/"},
		{forwarded: "gopher", want: "http://depsilo.example.test/composer/"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/composer/packages.json", nil)
		request.Host = "depsilo.example.test"
		request.Header.Set("X-Forwarded-Proto", test.forwarded)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("X-Forwarded-Proto %q: status = %d, body=%s", test.forwarded, response.Code, response.Body.String())
		}
		var document struct {
			MetadataURL string `json:"metadata-url"`
			Mirrors     []struct {
				DistURL string `json:"dist-url"`
			} `json:"mirrors"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
			t.Fatalf("X-Forwarded-Proto %q: decode packages.json: %v", test.forwarded, err)
		}
		if document.MetadataURL != test.want+"p2/%package%.json" {
			t.Errorf("X-Forwarded-Proto %q: metadata-url = %q, want prefix %q", test.forwarded, document.MetadataURL, test.want)
		}
		if len(document.Mirrors) != 1 || !strings.HasPrefix(document.Mirrors[0].DistURL, test.want+"dist/") {
			t.Errorf("X-Forwarded-Proto %q: mirrors = %#v, want one dist-url under %q", test.forwarded, document.Mirrors, test.want)
		}
	}
}
