package nuget

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestRegistrationBasePathPrefersSemver2(t *testing.T) {
	t.Parallel()
	index := []byte(`{
		"resources": [
			{"@id": "https://example.test/v3/registration/", "@type": "RegistrationsBaseUrl"},
			{"@id": "https://example.test/v3/registration5-gz-semver2/", "@type": "RegistrationsBaseUrl/3.6.0"},
			{"@id": "https://example.test/v3/registration3/", "@type": ["RegistrationsBaseUrl/3.4.0"]}
		]
	}`)
	path, ok := registrationBasePath(index)
	if !ok || path != "/v3/registration5-gz-semver2/" {
		t.Fatalf("registrationBasePath = %q, %v", path, ok)
	}

	if path, ok := registrationPagePath(
		"https://example.test/v3/registration5-gz-semver2/acme/lib/page/1.0.0/2.0.0.json",
		"/v3/registration5-gz-semver2/",
	); !ok || path != "/v3/registration5-gz-semver2/acme/lib/page/1.0.0/2.0.0.json" {
		t.Fatalf("absolute page path = %q, %v", path, ok)
	}
	if path, ok := registrationPagePath("page/1.0.0.json", "/v3/registration5-gz-semver2/acme/lib/"); !ok ||
		path != "/v3/registration5-gz-semver2/acme/lib/page/1.0.0.json" {
		t.Fatalf("relative page path = %q, %v", path, ok)
	}
}

func TestNuGetArtifactSourceIDIsStableAndVersionSpecific(t *testing.T) {
	t.Parallel()
	first := nugetArtifactSourceID("Acme.Demo", "1.0.0")
	if len(first) != 43 {
		t.Fatalf("source id length = %d, want 43", len(first))
	}
	if again := nugetArtifactSourceID("acme.demo", "1.0.0"); again != first {
		t.Fatalf("source id is case-sensitive: %q vs %q", first, again)
	}
	if other := nugetArtifactSourceID("acme.demo", "2.0.0"); other == first {
		t.Fatal("source id ignores the version")
	}
}

func TestNuGetFlatContainerGateUsesRegistrationPublishedTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	const artifactBody = "nuget-nupkg-bytes"
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/index.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"resources":[`+
				`{"@id":"http://`+r.Host+`/v3/registration5-gz-semver2/","@type":"RegistrationsBaseUrl/3.6.0"},`+
				`{"@id":"http://`+r.Host+`/v3-flatcontainer/","@type":"PackageBaseAddress/3.0.0"}]}`)
		case "/v3/registration5-gz-semver2/acme.demo/index.json":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{
					map[string]any{
						"@id":   "http://" + r.Host + "/v3/registration5-gz-semver2/acme.demo/page/1.0.0/2.0.0.json",
						"lower": "1.0.0",
						"upper": "2.0.0",
						"items": []any{
							map[string]any{"catalogEntry": map[string]any{
								"version":   "1.0.0",
								"published": now.Add(-time.Hour).Format(time.RFC3339),
								"listed":    true,
							}},
							map[string]any{"catalogEntry": map[string]any{
								"version":   "2.0.0",
								"published": now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
								"listed":    true,
							}},
						},
					},
					map[string]any{
						"@id":   "http://" + r.Host + "/v3/registration5-gz-semver2/acme.demo/page/3.0.0/3.0.0.json",
						"lower": "3.0.0",
						"upper": "3.0.0",
					},
				},
			})
		case "/v3/registration5-gz-semver2/acme.demo/page/3.0.0/3.0.0.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"items":[{"catalogEntry":{`+
				`"version":"3.0.0","published":"1900-01-01T00:00:00Z","listed":false}}]}`)
		case "/v3-flatcontainer/acme.demo/1.0.0/acme.demo.1.0.0.nupkg",
			"/v3-flatcontainer/acme.demo/2.0.0/acme.demo.2.0.0.nupkg",
			"/v3-flatcontainer/acme.demo/3.0.0/acme.demo.3.0.0.nupkg":
			_, _ = io.WriteString(w, artifactBody)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "nuget-provenance.db"))
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
	handler.SetProvenanceRequired(true)
	router := gin.New()
	handler.Register(router.Group("/nuget"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"nuget": "168h"},
	}, func(ecosystem string) bool { return ecosystem == "nuget" })
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	scoped := adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router)

	young := requestNupkg(t, scoped, "1.0.0")
	if young.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(young.Body.String(), `"code":"QUARANTINED"`) {
		t.Fatalf("young nupkg status=%d body=%s", young.Code, young.Body.String())
	}
	old := requestNupkg(t, scoped, "2.0.0")
	if old.Code != http.StatusOK || old.Body.String() != artifactBody {
		t.Fatalf("old nupkg status=%d body=%q", old.Code, old.Body.String())
	}
	unlisted := requestNupkg(t, scoped, "3.0.0")
	if unlisted.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(unlisted.Body.String(), "source-bound publish time") {
		t.Fatalf("unlisted nupkg status=%d body=%s", unlisted.Code, unlisted.Body.String())
	}
}

func requestNupkg(t *testing.T, handler http.Handler, version string) *httptest.ResponseRecorder {
	t.Helper()
	target := "/nuget/v3-flatcontainer/acme.demo/" + version + "/acme.demo." + version + ".nupkg"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	return response
}

func TestNuGetProvenanceMemoizesRegistrationLookups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	var serviceIndexHits, registrationHits atomic.Int64
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/index.json":
			serviceIndexHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"resources":[{"@id":"http://`+r.Host+
				`/v3/registration5-gz-semver2/","@type":"RegistrationsBaseUrl/3.6.0"}]}`)
		case "/v3/registration5-gz-semver2/acme.demo/index.json":
			registrationHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []any{map[string]any{
					"@id":   "http://" + r.Host + "/v3/registration5-gz-semver2/acme.demo/page/2.0.0/2.0.0.json",
					"lower": "2.0.0",
					"upper": "2.0.0",
					"items": []any{map[string]any{"catalogEntry": map[string]any{
						"version":   "2.0.0",
						"published": now.Add(-30 * 24 * time.Hour).Format(time.RFC3339),
						"listed":    true,
					}}},
				}},
			})
		case "/v3-flatcontainer/acme.demo/2.0.0/acme.demo.2.0.0.nupkg":
			_, _ = io.WriteString(w, "nupkg")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "nuget-memo.db"))
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
	// TTLIndex 0 expires metadata immediately, so a second provenance lookup
	// would hit the upstream again without the in-process memo.
	handler := New(
		manager,
		upstream.NewPassiveRecoverySelector(pool),
		config.CacheConfig{TTLIndex: 0, TTLBlob: time.Hour},
		database,
	)
	handler.SetProvenanceRequired(true)
	router := gin.New()
	handler.Register(router.Group("/nuget"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"nuget": "168h"},
	}, func(ecosystem string) bool { return ecosystem == "nuget" })
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	scoped := adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router)

	for attempt := 0; attempt < 2; attempt++ {
		response := requestNupkg(t, scoped, "2.0.0")
		if response.Code != http.StatusOK {
			t.Fatalf("attempt %d status=%d body=%s", attempt, response.Code, response.Body.String())
		}
	}
	if got := serviceIndexHits.Load(); got != 1 {
		t.Fatalf("service index hits = %d, want 1", got)
	}
	if got := registrationHits.Load(); got != 1 {
		t.Fatalf("registration index hits = %d, want 1", got)
	}
}
