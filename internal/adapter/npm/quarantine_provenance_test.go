package npm

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/quarantine"
)

// TestNPMQuarantineUsesSourceBoundPublishTime is the end-to-end acceptance
// test for the npm provenance slice: the packument timestamp travels through
// the signed tarball token into a real quarantine checker.
func TestNPMQuarantineUsesSourceBoundPublishTime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	published := map[string]time.Time{
		"1.0.0": now.Add(-time.Hour),
		"2.0.0": now.Add(-30 * 24 * time.Hour),
	}
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/fixture":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "fixture",
				"time": map[string]string{
					"1.0.0": published["1.0.0"].Format(time.RFC3339),
					"2.0.0": published["2.0.0"].Format(time.RFC3339),
				},
				"versions": map[string]any{
					"1.0.0": map[string]any{"dist": map[string]any{
						"tarball": "http://" + request.Host + "/fixture/-/young.tgz",
					}},
					"2.0.0": map[string]any{"dist": map[string]any{
						"tarball": "http://" + request.Host + "/fixture/-/old.tgz",
					}},
				},
			})
		case "/fixture/-/young.tgz", "/fixture/-/old.tgz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = io.WriteString(w, "tarball")
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(upstreamServer.Close)

	signer := deterministicTarballSigner(t, 0x21)
	router := newNPMProvenanceRouterFactory(t, []config.UpstreamConfig{{
		Name: "mock", URL: upstreamServer.URL, Priority: 1, ProbeMode: "passive",
	}})(signer)

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "npm-quarantine.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	enabled := true
	policy, err := quarantine.NewPolicyWithProvenance(quarantine.Config{
		MinReleaseAgeEnabled: &enabled,
		MinReleaseAge:        map[string]string{"npm": "168h"},
	}, func(ecosystem string) bool { return ecosystem == "npm" })
	if err != nil {
		t.Fatalf("NewPolicyWithProvenance: %v", err)
	}
	store := quarantine.NewStore(database)
	checker, err := quarantine.NewChecker(policy, quarantine.NewLookup(store, nil), store)
	if err != nil {
		t.Fatalf("NewChecker: %v", err)
	}
	scoped := adapter.NewRequestScope(
		nil,
		nil,
		quarantine.Wrap(checker),
		&npmRecordingPackageRuleChecker{decision: adapter.PackageRuleDecision{Outcome: adapter.PackageRuleAllow}},
	).Wrap(router)

	youngURL := requestNPMMetadataTarballURLAt(t, router, "/npm/fixture", "1.0.0")
	oldURL := requestNPMMetadataTarballURLAt(t, router, "/npm/fixture", "2.0.0")

	young := requestURLPath(t, scoped, youngURL)
	if young.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(young.Body.String(), `"code":"QUARANTINED"`) ||
		!strings.Contains(young.Body.String(), "source ") {
		t.Fatalf("young tarball status=%d body=%s", young.Code, young.Body.String())
	}

	old := requestURLPath(t, scoped, oldURL)
	if old.Code != http.StatusOK || old.Body.String() != "tarball" {
		t.Fatalf("old tarball status=%d body=%q", old.Code, old.Body.String())
	}

	// A valid signature without a publish time must fail closed while the
	// gate is enabled instead of falling back to a registry lookup.
	parsed, err := url.Parse(youngURL)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(parsed.EscapedPath(), "/")
	claims, valid := signer.verify(parts[len(parts)-2], "global", "fixture", "young.tgz")
	if !valid {
		t.Fatal("could not verify the fixture token")
	}
	claims.PublishAt = 0
	unboundToken, err := signer.sign(claims)
	if err != nil {
		t.Fatal(err)
	}
	unboundURL := "http://depsilo.example" + signedTarballPath("/npm", "fixture", unboundToken, "young.tgz")
	unbound := requestURLPath(t, scoped, unboundURL)
	if unbound.Code != http.StatusUnavailableForLegalReasons ||
		!strings.Contains(unbound.Body.String(), "source-bound publish time") {
		t.Fatalf("unbound tarball status=%d body=%s", unbound.Code, unbound.Body.String())
	}
}
