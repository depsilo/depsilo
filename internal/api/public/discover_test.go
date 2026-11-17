package public

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDiscoverUsesCanonicalEcosystemRoutes(t *testing.T) {
	router := gin.New()
	router.GET("/api/v1/discover", NewDiscoverHandler([]string{"cargo", "docker", "custom"}).Discover)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/discover", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Ecosystems             []ecosystemInfo   `json:"ecosystems"`
		Endpoints              map[string]string `json:"endpoints"`
		EndpointAuthentication map[string]string `json:"endpoint_authentication"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := []ecosystemInfo{
		{Name: "cargo", Path: "/crates/", Purpose: ecosystemPurposes["cargo"]},
		{Name: "docker", Path: "/v2/", Purpose: ecosystemPurposes["docker"]},
		{Name: "custom", Path: "/custom/", Purpose: "custom"},
	}
	if len(response.Ecosystems) != len(want) {
		t.Fatalf("ecosystems = %#v", response.Ecosystems)
	}
	for index := range want {
		if response.Ecosystems[index] != want[index] {
			t.Fatalf("ecosystem[%d] = %#v, want %#v", index, response.Ecosystems[index], want[index])
		}
	}
	if response.Endpoints["public_packages"] != "/api/v1/packages" {
		t.Fatalf("stable public_packages endpoint = %q", response.Endpoints["public_packages"])
	}
	for _, endpoint := range []string{"mcp", "public_packages", "events_stream"} {
		if !strings.Contains(response.EndpointAuthentication[endpoint], "Bearer") {
			t.Fatalf("%s authentication = %q, want Bearer guidance", endpoint, response.EndpointAuthentication[endpoint])
		}
	}
}

type discoverCatalog struct {
	SetupRequired *bool             `json:"setup_required"`
	Endpoints     map[string]string `json:"endpoints"`
	AgentSetup    struct {
		Summary string `json:"summary"`
	} `json:"agent_setup"`
}

func serveDiscover(t *testing.T, handler *DiscoverHandler, path string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/v1/discover", handler.Discover)
	router.GET("/api/v1/agent-prompt", handler.AgentPrompt)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://depsilo.example"+path, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s: status = %d, body = %s", path, recorder.Code, recorder.Body.String())
	}
	return recorder
}

// setupHowTo lists what agent-facing text must never carry while setup is
// pending: completing setup is a human operator's step, so neither the token
// nor the API that consumes it is described.
var setupHowTo = []string{"token", "bootstrap", "setup/complete", "depsilo setup", "X-Depsilo", "curl", "POST"}

func TestDiscoverReportsPendingSetup(t *testing.T) {
	pending := true
	handler := NewDiscoverHandler(nil)
	handler.SetSetupRequired(func() bool { return pending })

	var catalog discoverCatalog
	if err := json.Unmarshal(serveDiscover(t, handler, "/api/v1/discover").Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SetupRequired == nil || !*catalog.SetupRequired {
		t.Fatalf("setup_required = %v, want true", catalog.SetupRequired)
	}
	if catalog.Endpoints["setup_status"] != "/api/v1/setup/status" {
		t.Fatalf("endpoints.setup_status = %q", catalog.Endpoints["setup_status"])
	}
	summary := catalog.AgentSetup.Summary
	for _, expected := range []string{"setup is not finished", "human operator", "Portal at http://depsilo.example/"} {
		if !strings.Contains(summary, expected) {
			t.Errorf("pending summary omits %q: %s", expected, summary)
		}
	}
	for _, forbidden := range setupHowTo {
		if strings.Contains(summary, forbidden) {
			t.Errorf("pending summary explains how to run setup (%q): %s", forbidden, summary)
		}
	}

	// The state is read per request, so the catalog follows setup completion.
	pending = false
	catalog = discoverCatalog{}
	if err := json.Unmarshal(serveDiscover(t, handler, "/api/v1/discover").Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SetupRequired == nil || *catalog.SetupRequired {
		t.Fatalf("setup_required after completion = %v, want false", catalog.SetupRequired)
	}
	if strings.Contains(catalog.AgentSetup.Summary, "not finished") || !strings.Contains(catalog.AgentSetup.Summary, "bootstrap prompt") {
		t.Fatalf("configured summary = %q", catalog.AgentSetup.Summary)
	}
}

func TestDiscoverWithoutSetupStateDescribesAConfiguredService(t *testing.T) {
	var catalog discoverCatalog
	if err := json.Unmarshal(serveDiscover(t, NewDiscoverHandler(nil), "/api/v1/discover").Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.SetupRequired == nil || *catalog.SetupRequired {
		t.Fatalf("setup_required = %v, want an explicit false", catalog.SetupRequired)
	}
}

func TestAgentPromptSaysSetupIsPendingInsteadOfConfiguring(t *testing.T) {
	handler := NewDiscoverHandler([]string{"pypi"})
	handler.SetSetupRequired(func() bool { return true })

	prompt := serveDiscover(t, handler, "/api/v1/agent-prompt").Body.String()
	for _, expected := range []string{"has not finished first-run setup", "human operator", "Portal at http://depsilo.example/", "Do not point any package manager"} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("pending prompt omits %q:\n%s", expected, prompt)
		}
	}
	for _, forbidden := range append([]string{"pip config", "npm config", "GOPROXY"}, setupHowTo...) {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("pending prompt contains %q:\n%s", forbidden, prompt)
		}
	}

	handler.SetSetupRequired(func() bool { return false })
	if configured := serveDiscover(t, handler, "/api/v1/agent-prompt").Body.String(); !strings.Contains(configured, "pip config set global.index-url http://depsilo.example/pypi/simple/") {
		t.Fatalf("configured prompt lost its instructions:\n%s", configured)
	}
}

// readme_anchor once pointed at a README heading that no longer exists. It
// has to name a heading of a document that is in this repository.
func TestDiscoverDocumentationAnchorPointsAtAnExistingHeading(t *testing.T) {
	var catalog struct {
		AgentSetup struct {
			Anchor string `json:"readme_anchor"`
		} `json:"agent_setup"`
	}
	if err := json.Unmarshal(serveDiscover(t, NewDiscoverHandler(nil), "/api/v1/discover").Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	const prefix = "https://github.com/depsilo/depsilo/blob/master/"
	file, anchor, found := strings.Cut(strings.TrimPrefix(catalog.AgentSetup.Anchor, prefix), "#")
	if !strings.HasPrefix(catalog.AgentSetup.Anchor, prefix) || !found {
		t.Fatalf("readme_anchor = %q, want %s<file>#<heading>", catalog.AgentSetup.Anchor, prefix)
	}
	document, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(file)))
	if err != nil {
		t.Fatalf("readme_anchor names a missing file: %v", err)
	}
	for _, line := range strings.Split(string(document), "\n") {
		heading := strings.TrimLeft(line, "#")
		if heading == line || !strings.HasPrefix(heading, " ") {
			continue
		}
		if strings.ReplaceAll(strings.ToLower(strings.TrimSpace(heading)), " ", "-") == anchor {
			return
		}
	}
	t.Fatalf("%s has no heading for #%s", file, anchor)
}
