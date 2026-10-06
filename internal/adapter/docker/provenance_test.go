package docker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/adapter"
	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

type registryFixture struct {
	mu        sync.Mutex
	manifest  map[string]string // reference → digest
	noDigest  bool
	requested []string
}

func newRegistryFixture(t *testing.T) (*httptest.Server, *registryFixture) {
	t.Helper()
	fixture := &registryFixture{manifest: map[string]string{
		"latest":      "sha256:aaaa",
		"sha256:aaaa": "sha256:aaaa",
	}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.requested = append(fixture.requested, r.Method+" "+r.URL.Path)
		noDigest := fixture.noDigest
		fixture.mu.Unlock()

		if r.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if !strings.Contains(r.URL.Path, "/manifests/") {
			http.NotFound(w, r)
			return
		}
		reference := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		fixture.mu.Lock()
		digest, known := fixture.manifest[reference]
		fixture.mu.Unlock()
		if !known {
			http.NotFound(w, r)
			return
		}
		if !noDigest {
			w.Header().Set("Docker-Content-Digest", digest)
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = io.WriteString(w, `{"schemaVersion":2,"config":{"digest":"sha256:cccc"}}`)
	}))
	t.Cleanup(server.Close)
	return server, fixture
}

func newDockerTestHandler(t *testing.T, upstreamURL string, threshold string) (http.Handler, *gorm.DB) {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "docker-provenance.db"))
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
	handler := New(manager, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database, config.DockerConfig{
		DefaultRegistry: "mock",
		Registries:      []config.RegistryConfig{{Name: "mock", URL: upstreamURL}},
	})
	handler.SetProvenanceRequired(true)
	router := gin.New()
	handler.Register(router.Group("/v2"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"docker": threshold},
		ObservationSources:   []string{"docker"},
	}, nil)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	return adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router), database
}

func requestDockerManifest(t *testing.T, handler http.Handler, reference string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/v2/acme/demo/manifests/"+reference,
		nil,
	))
	return response
}

func seedDockerObservation(t *testing.T, database *gorm.DB, registry, digest string, firstSeen time.Time) {
	t.Helper()
	if err := database.Create(&db.DockerImageObservation{
		Registry: registry, Digest: digest, FirstSeenAt: firstSeen.UTC(),
	}).Error; err != nil {
		t.Fatal(err)
	}
}

func TestDockerTagResolvesPinsAndGatesOnFirstObservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, fixture := newRegistryFixture(t)
	scoped, database := newDockerTestHandler(t, upstreamServer.URL, "168h")

	// First sight: the observation starts now, so the digest is quarantined and
	// the reason says the age is first-observation, not a publish time.
	fresh := requestDockerManifest(t, scoped, "latest")
	if fresh.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(fresh.Body.String(), `"code":"QUARANTINED"`) ||
		!strings.Contains(fresh.Body.String(), "first observed") {
		t.Fatalf("fresh tag status=%d body=%s", fresh.Code, fresh.Body.String())
	}
	var observation db.DockerImageObservation
	if err := database.First(&observation, "registry = ? AND digest = ?", "mock", "sha256:aaaa").Error; err != nil {
		t.Fatalf("observation row missing: %v", err)
	}

	// Age the observation past the threshold: the same tag now serves, and the
	// upstream manifest was pinned to the resolved digest rather than the tag.
	if err := database.Model(&db.DockerImageObservation{}).Where("id = ?", observation.ID).
		Update("first_seen_at", time.Now().UTC().Add(-30*24*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	served := requestDockerManifest(t, scoped, "latest")
	if served.Code != http.StatusOK || !strings.Contains(served.Body.String(), "schemaVersion") {
		t.Fatalf("aged tag status=%d body=%s", served.Code, served.Body.String())
	}
	fixture.mu.Lock()
	requests := append([]string(nil), fixture.requested...)
	fixture.mu.Unlock()
	pinned := false
	for _, request := range requests {
		if request == "GET /v2/acme/demo/manifests/sha256:aaaa" {
			pinned = true
		}
	}
	if !pinned {
		t.Fatalf("manifest fetch was not pinned to the resolved digest: %v", requests)
	}
}

func TestDockerDigestAndMissingDigestBehaviour(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, fixture := newRegistryFixture(t)
	scoped, database := newDockerTestHandler(t, upstreamServer.URL, "168h")
	seedDockerObservation(t, database, "mock", "sha256:aaaa", time.Now().UTC().Add(-30*24*time.Hour))

	// Digest pulls are gated on the same observation and served when aged.
	digest := requestDockerManifest(t, scoped, "sha256:aaaa")
	if digest.Code != http.StatusOK {
		t.Fatalf("aged digest status=%d body=%s", digest.Code, digest.Body.String())
	}

	// A registry that omits Docker-Content-Digest cannot prove identity while
	// the gate is armed, so the request is refused instead of guessed.
	fixture.mu.Lock()
	fixture.noDigest = true
	fixture.mu.Unlock()
	missing := requestDockerManifest(t, scoped, "latest")
	if missing.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(missing.Body.String(), "did not return a manifest digest") {
		t.Fatalf("missing digest status=%d body=%s", missing.Code, missing.Body.String())
	}
}
