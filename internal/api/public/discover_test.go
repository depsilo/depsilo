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

	"depsilo/internal/api/contract"
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
		Ecosystems             []contract.DiscoverEcosystem `json:"ecosystems"`
		Endpoints              map[string]string            `json:"endpoints"`
		EndpointAuthentication map[string]string            `json:"endpoint_authentication"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := []contract.DiscoverEcosystem{
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

// setup_required follows the setup state per request; the agent guidance is
// the same either way, because package routes serve before setup.
func TestDiscoverReportsPendingSetupWithoutWithholdingGuidance(t *testing.T) {
	pending := true
	handler := NewDiscoverHandler(nil)
	handler.SetSetupRequired(func() bool { return pending })

	for _, want := range []bool{true, false} {
		pending = want
		var catalog discoverCatalog
		if err := json.Unmarshal(serveDiscover(t, handler, "/api/v1/discover").Body.Bytes(), &catalog); err != nil {
			t.Fatal(err)
		}
		if catalog.SetupRequired == nil || *catalog.SetupRequired != want {
			t.Fatalf("setup_required = %v, want %t", catalog.SetupRequired, want)
		}
		if catalog.Endpoints["setup_status"] != "/api/v1/setup/status" {
			t.Fatalf("endpoints.setup_status = %q", catalog.Endpoints["setup_status"])
		}
		if !strings.Contains(catalog.AgentSetup.Summary, "bootstrap prompt") {
			t.Fatalf("pending=%t summary = %q", want, catalog.AgentSetup.Summary)
		}
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

func TestAgentPromptConfiguresClientsBeforeSetup(t *testing.T) {
	handler := NewDiscoverHandler([]string{"pypi"})
	handler.SetSetupRequired(func() bool { return true })

	prompt := serveDiscover(t, handler, "/api/v1/agent-prompt").Body.String()
	if !strings.Contains(prompt, "pip config set global.index-url http://depsilo.example/pypi/simple/") {
		t.Fatalf("pending prompt lost its instructions:\n%s", prompt)
	}
	for _, forbidden := range []string{"setup/complete", "X-Depsilo", "depsilo setup"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("prompt explains how to run setup (%q):\n%s", forbidden, prompt)
		}
	}
}

// The bootstrap prompt is specific to the instance: it names the ecosystems
// the handler was built with and no others, and asks before anything that
// outlives the project.
func TestAgentPromptListsOnlyTheEcosystemsThisInstanceServes(t *testing.T) {
	prompt := serveDiscover(t, NewDiscoverHandler([]string{"pypi", "npm"}), "/api/v1/agent-prompt").Body.String()
	for _, expected := range []string{
		"PIP_INDEX_URL=http://depsilo.example/pypi/simple/",
		"registry=http://depsilo.example/npm/",
		"omit-lockfile-registry-resolved=true",
		"show the user the exact\nchanges and wait for approval",
	} {
		if !strings.Contains(prompt, expected) {
			t.Errorf("prompt omits %q:\n%s", expected, prompt)
		}
	}
	for _, absent := range []string{"GOPROXY", "/crates/", "/maven/", "/composer/", "HF_ENDPOINT", "registry-mirrors"} {
		if strings.Contains(prompt, absent) {
			t.Errorf("prompt names %q, which this instance does not serve", absent)
		}
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

// Every URL this package writes for a client comes from one rule for the
// request's scheme, so the catalog, the agent prompt and the integration
// prompt cannot disagree about it, and none of them repeats a header value
// that is not a scheme.
func TestRequestOriginTakesOnlyAnHTTPSchemeFromForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	discover := NewDiscoverHandler([]string{"npm"})
	router := gin.New()
	router.GET("/api/v1/discover", discover.Discover)
	router.GET("/api/v1/agent-prompt", discover.AgentPrompt)
	router.GET("/api/v1/integration-prompt", NewIntegrationPromptHandler().Get)

	for _, test := range []struct {
		forwarded string
		want      string
	}{
		{forwarded: "https", want: "https://depsilo.example.test"},
		{forwarded: "https, http", want: "https://depsilo.example.test"},
		{forwarded: "HTTPS", want: "https://depsilo.example.test"},
		{forwarded: "http, https", want: "http://depsilo.example.test"},
		{forwarded: "gopher", want: "http://depsilo.example.test"},
	} {
		get := func(path string) string {
			t.Helper()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Host = "depsilo.example.test"
			request.Header.Set("X-Forwarded-Proto", test.forwarded)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("X-Forwarded-Proto %q: GET %s status = %d, body = %.300s", test.forwarded, path, recorder.Code, recorder.Body.String())
			}
			return recorder.Body.String()
		}

		var catalog struct {
			MCP struct {
				URL string `json:"url"`
			} `json:"mcp"`
		}
		if err := json.Unmarshal([]byte(get("/api/v1/discover")), &catalog); err != nil {
			t.Fatal(err)
		}
		if catalog.MCP.URL != test.want+"/mcp" {
			t.Errorf("X-Forwarded-Proto %q: discover mcp.url = %q, want %s/mcp", test.forwarded, catalog.MCP.URL, test.want)
		}
		for _, path := range []string{"/api/v1/agent-prompt", "/api/v1/integration-prompt?ecosystem=node"} {
			body := get(path)
			if !strings.Contains(body, test.want+"/npm/") {
				t.Errorf("X-Forwarded-Proto %q: GET %s does not address the service as %s", test.forwarded, path, test.want)
			}
		}
	}
}
