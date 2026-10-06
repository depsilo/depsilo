package rubygems

import (
	"context"
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

// staticBlocklist stands in for the synced known-malicious dataset.
type staticBlocklist struct {
	ecosystem string
	pkg       string
	version   string
	sourceID  string
}

func (b staticBlocklist) Check(
	_ context.Context,
	ecosystem, pkg, version string,
) (*quarantine.BlocklistMatch, bool, error) {
	if ecosystem == b.ecosystem && pkg == b.pkg && version == b.version {
		return &quarantine.BlocklistMatch{SourceID: b.sourceID, Summary: "test malware"}, false, nil
	}
	return nil, false, nil
}

// newRubyGemsDatasetHandler builds the adapter with a zero release-age
// threshold and identity required because the dataset covers RubyGems.
func newRubyGemsDatasetHandler(t *testing.T, upstreamURL string, blocklist quarantine.Blocklist) http.Handler {
	t.Helper()
	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "rubygems-dataset.db"))
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
		Name: "mock", URL: upstreamURL, Priority: 1, ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(manager, upstream.NewPassiveRecoverySelector(pool), config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour}, database)
	handler.SetProvenanceRequired(true)
	router := gin.New()
	handler.Register(router.Group("/rubygems"))

	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
	}, nil)
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	checker.SetBlocklist(blocklist)
	return adapter.NewRequestScope(nil, nil, quarantine.Wrap(checker), nil).Wrap(router)
}

func requestRubyGemsArtifact(t *testing.T, handler http.Handler, name string) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/rubygems/gems/"+name+".gem", nil))
	return response
}

func TestRubyGemsDatasetBlocksMaliciousGemWithoutAgeGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := rubyGemsTestFixture(t, time.Now().UTC())
	scoped := newRubyGemsDatasetHandler(t, upstreamServer.URL, staticBlocklist{
		ecosystem: "rubygems",
		pkg:       "acme-demo",
		version:   "2.0.0",
		sourceID:  "MAL-2026-0001",
	})

	blocked := requestRubyGemsArtifact(t, scoped, "acme-demo-2.0.0")
	if blocked.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(blocked.Body.String(), `"code":"MALICIOUS_BLOCKED"`) {
		t.Fatalf("malicious gem status=%d body=%s", blocked.Code, blocked.Body.String())
	}

	clean := requestRubyGemsArtifact(t, scoped, "acme-demo-1.0.0")
	if clean.Code != http.StatusOK || clean.Body.String() != "gem-bytes" {
		t.Fatalf("clean gem status=%d body=%q", clean.Code, clean.Body.String())
	}
}

func TestRubyGemsIdentityUnavailableIsRefusedUnderDatasetCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := rubyGemsTestFixture(t, time.Now().UTC())
	scoped := newRubyGemsDatasetHandler(t, upstreamServer.URL, staticBlocklist{})

	response := requestRubyGemsArtifact(t, scoped, "unknown-gem-1.0.0")
	if response.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(response.Body.String(), "identity is unavailable") {
		t.Fatalf("unresolvable gem status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRubyGemsPlatformArtifactUsesBaseVersionForDatasetMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer, _ := rubyGemsTestFixture(t, time.Now().UTC())
	// The dataset knows the platform-less release only.
	scoped := newRubyGemsDatasetHandler(t, upstreamServer.URL, staticBlocklist{
		ecosystem: "rubygems",
		pkg:       "acme-demo",
		version:   "1.0.0",
		sourceID:  "MAL-2026-0002",
	})

	blocked := requestRubyGemsArtifact(t, scoped, "acme-demo-1.0.0-x86_64-linux")
	if blocked.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(blocked.Body.String(), `"code":"MALICIOUS_BLOCKED"`) {
		t.Fatalf("platform gem status=%d body=%s", blocked.Code, blocked.Body.String())
	}
}

func TestBaseVersionTokenRequiresListedPrefix(t *testing.T) {
	t.Parallel()
	info := []byte("1.0.0 dep|checksum:aaa,created_at:2026-01-01T00:00:00Z\n" +
		"1.0.0-x86_64-linux dep|checksum:bbb,created_at:2026-01-01T00:00:00Z\n" +
		"2.0.0-java|checksum:ccc\n")
	if got := baseVersionToken(info, "1.0.0-x86_64-linux"); got != "1.0.0" {
		t.Fatalf("base version = %q, want 1.0.0", got)
	}
	if got := baseVersionToken(info, "2.0.0-java"); got != "2.0.0-java" {
		t.Fatalf("unlisted base version = %q, want the full token", got)
	}
	if got := baseVersionToken(info, "1.0.0"); got != "1.0.0" {
		t.Fatalf("plain version = %q, want unchanged", got)
	}
}
