package composer

import (
	"context"
	"encoding/json"
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
	"depsilo/internal/quarantine"
	"depsilo/internal/upstream"
)

func TestComposerDistUsesSourceBoundPublishTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	const artifactBody = "composer-dist-bytes"
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/p2/acme/demo.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"packages": map[string]any{
					"acme/demo": []any{
						map[string]any{
							"version":            "1.0.0",
							"version_normalized": "1.0.0.0",
							"time":               now.Add(-time.Hour).Format(time.RFC3339),
							"dist": map[string]any{
								"url":       "http://" + r.Host + "/files/demo-1.0.0.zip",
								"type":      "zip",
								"reference": "ref-young",
							},
						},
						map[string]any{
							"version":            "2.0.0",
							"version_normalized": "2.0.0.0",
							"time":               now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
							"dist": map[string]any{
								"url":       "http://" + r.Host + "/files/demo-2.0.0.zip",
								"type":      "zip",
								"reference": "ref-old",
							},
						},
						map[string]any{
							"version":            "3.0.0",
							"version_normalized": "3.0.0.0",
							"time":               "__unset",
							"dist": map[string]any{
								"url":       "http://" + r.Host + "/files/demo-3.0.0.zip",
								"type":      "zip",
								"reference": "ref-missing",
							},
						},
					},
				},
			})
		case "/files/demo-1.0.0.zip", "/files/demo-2.0.0.zip", "/files/demo-3.0.0.zip":
			_, _ = io.WriteString(w, artifactBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "composer-quarantine.db"))
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
	handler := New(
		manager,
		upstream.NewPassiveRecoverySelector(pool),
		config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour},
		database,
	)
	router := gin.New()
	handler.Register(router.Group("/composer"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"composer": "168h"},
	}, func(ecosystem string) bool { return ecosystem == "composer" })
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	scoped := adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router)

	young := requestComposerDist(t, scoped, "/composer/dist/acme/demo/1.0.0.0/ref-young.zip")
	if young.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(young.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("young dist status=%d body=%s", young.Code, young.Body.String())
	}

	old := requestComposerDist(t, scoped, "/composer/dist/acme/demo/2.0.0.0/ref-old.zip")
	if old.Code != http.StatusOK || old.Body.String() != artifactBody {
		t.Fatalf("old dist status=%d body=%q", old.Code, old.Body.String())
	}

	missing := requestComposerDist(t, scoped, "/composer/dist/acme/demo/3.0.0.0/ref-missing.zip")
	if missing.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(missing.Body.String(), "source-bound publish time") {
		t.Fatalf("missing-time dist status=%d body=%s", missing.Code, missing.Body.String())
	}
}

func requestComposerDist(t *testing.T, handler http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}
