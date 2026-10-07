package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/upstream"
)

const cascadeConfigTestDocument = `config_version = 1

[cascade]
enabled = false

[[cascade.peers]]
name = "home"
url = "https://cache.example"
`

type cascadeConfigFixture struct {
	router *gin.Engine
	path   string
	store  *config.Store
}

func newCascadeConfigFixture(t *testing.T, document string, withRegistry bool) cascadeConfigFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	effective := &config.Config{}
	effective.Cascade.MaxHops = 4
	effective.Cascade.MaxTTL = 168 * time.Hour
	store := config.NewStore(path, effective, zap.NewAtomicLevel())

	var registry *upstream.Registry
	if withRegistry {
		database, err := db.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		if err := db.AutoMigrate(database); err != nil {
			t.Fatal(err)
		}
		record := db.UpstreamRecord{
			AdapterType: "npm", Name: "peer-npm", URL: "https://registry.npmjs.org",
			Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true, Via: "home",
		}
		if err := database.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
		peerURL, err := url.Parse("https://cache.example")
		if err != nil {
			t.Fatal(err)
		}
		registry, err = upstream.NewRegistryWithOptions(database, []string{"npm"}, upstream.PoolOptions{
			Peers: upstream.NewPeerSet([]upstream.PeerEgress{{
				Name: "home", URL: peerURL, Token: "shared-secret-token-0123456789",
				LocalInstanceID: "node-1", MaxHops: 4,
			}}),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	handler := NewCascadeConfigHandler(store, registry)
	router := gin.New()
	router.GET("/cascade/config", handler.Get)
	router.PUT("/cascade/config", handler.Update)
	return cascadeConfigFixture{router: router, path: path, store: store}
}

func performCascadeConfigRequest(router http.Handler, method, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/cascade/config", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func containsCascadeString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCascadeConfigHandlerMasksTokensAndWritesPatch(t *testing.T) {
	fixture := newCascadeConfigFixture(t, cascadeConfigTestDocument, true)

	initial := performCascadeConfigRequest(fixture.router, http.MethodGet, "")
	if initial.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", initial.Code, initial.Body.String())
	}
	if strings.Contains(initial.Body.String(), "shared-secret") {
		t.Fatalf("GET leaked a token: %s", initial.Body.String())
	}

	body := `{
		"enabled": true,
		"shared_token": "shared-secret-token-0123456789",
		"max_hops": 3,
		"max_ttl": "12h",
		"allow_insecure_http": true,
		"peers": [
			{"name": "home", "url": "https://cache.example"},
			{"name": "lab", "url": "https://cache.lab.example", "token": "peer-secret-token-0123456789", "forward_credentials": true}
		]
	}`
	updated := performCascadeConfigRequest(fixture.router, http.MethodPut, body)
	if updated.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", updated.Code, updated.Body.String())
	}
	if strings.Contains(updated.Body.String(), "shared-secret") || strings.Contains(updated.Body.String(), "peer-secret") {
		t.Fatalf("PUT leaked a token: %s", updated.Body.String())
	}
	var response cascadeConfigUpdateResponse
	if err := json.Unmarshal(updated.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.RestartRequired || !response.State.TokenSet || len(response.State.Peers) != 2 {
		t.Fatalf("response = %#v", response)
	}
	if response.State.Peers[0].TokenSet || !response.State.Peers[1].TokenSet {
		t.Fatalf("masked peers = %#v", response.State.Peers)
	}
	if !containsCascadeString(response.Changed, "cascade.enabled") || !containsCascadeString(response.Changed, "cascade.peers") {
		t.Fatalf("changed = %v", response.Changed)
	}

	written, err := os.ReadFile(fixture.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`enabled = true`,
		`token = "shared-secret-token-0123456789"`,
		`name = "lab"`,
		`token = "peer-secret-token-0123456789"`,
		`forward_credentials = true`,
		`max_ttl = "12h"`,
	} {
		if !strings.Contains(string(written), want) {
			t.Fatalf("written config missing %q:\n%s", want, written)
		}
	}

	secondGet := performCascadeConfigRequest(fixture.router, http.MethodGet, "")
	if secondGet.Code != http.StatusOK || strings.Contains(secondGet.Body.String(), "secret") {
		t.Fatalf("second GET = %d %s", secondGet.Code, secondGet.Body.String())
	}
}

func TestCascadeConfigHandlerRejectsRemovingBoundPeer(t *testing.T) {
	fixture := newCascadeConfigFixture(t, cascadeConfigTestDocument, true)
	before, _ := os.ReadFile(fixture.path)

	recorder := performCascadeConfigRequest(fixture.router, http.MethodPut, `{"peers": []}`)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "still used by upstreams") {
		t.Fatalf("body = %s", recorder.Body.String())
	}
	after, _ := os.ReadFile(fixture.path)
	if string(before) != string(after) {
		t.Fatal("rejected patch rewrote the config file")
	}
}

func TestCascadeConfigHandlerRejectsMalformedRequests(t *testing.T) {
	fixture := newCascadeConfigFixture(t, cascadeConfigTestDocument, false)
	cases := []struct {
		name string
		body string
	}{
		{"unknown field", `{"bogus": true}`},
		{"trailing object", `{} {}`},
		{"empty patch", `{}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recorder := performCascadeConfigRequest(fixture.router, http.MethodPut, testCase.body)
			if recorder.Code != http.StatusBadRequest && recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestCascadeConfigHandlerReportsReadOnlyConfig(t *testing.T) {
	fixture := newCascadeConfigFixture(t, cascadeConfigTestDocument, false)
	if err := os.Chmod(fixture.path, 0o444); err != nil {
		t.Fatal(err)
	}
	get := performCascadeConfigRequest(fixture.router, http.MethodGet, "")
	if get.Code != http.StatusOK || !strings.Contains(get.Body.String(), `"config_writable":false`) {
		t.Fatalf("GET = %d %s", get.Code, get.Body.String())
	}
	put := performCascadeConfigRequest(fixture.router, http.MethodPut, `{"max_hops": 2}`)
	if put.Code != http.StatusConflict || !strings.Contains(put.Body.String(), "CONFIG_READ_ONLY") {
		t.Fatalf("PUT = %d %s", put.Code, put.Body.String())
	}
}
