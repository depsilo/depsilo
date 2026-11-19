package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"depsilo/internal/cascade"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/relay"
	"depsilo/internal/upstream"
)

func TestCascadeRuntimeTurnsTheRelayAndParentsOnAndOffWithoutARestart(t *testing.T) {
	database := serverTestDB(t)
	record := db.UpstreamRecord{AdapterType: "npm", Name: "npmjs", URL: "https://registry.npmjs.org", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true, Via: "home"}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := upstream.NewRegistry(database, []string{"npm"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newCascadeRuntime("node-1", registry, config.CascadeConfig{}, func(settings config.CascadeConfig) *relay.Handler {
		return &relay.Handler{Identity: "node-1", Token: settings.Token}
	})
	probe := func(token string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, cascade.RelayPath, nil)
		request.Header.Set(cascade.HeaderToken, token)
		request.Header.Set(cascade.HeaderProbe, "1")
		runtime.ServeHTTP(recorder, request)
		return recorder.Code
	}
	if code := probe("shared-token-0123456789"); code != http.StatusNotFound {
		t.Fatalf("relay while cascade is off answered %d, want 404", code)
	}

	enabled := config.CascadeConfig{
		Enabled: true, Token: "shared-token-0123456789", MaxHops: 4, DirectFallback: true,
		Peers: []config.CascadePeerConfig{{Name: "home", URL: "https://cache.example"}},
	}
	if err := runtime.Apply(enabled); err != nil {
		t.Fatal(err)
	}
	if code := probe("shared-token-0123456789"); code != http.StatusNoContent {
		t.Fatalf("relay after Apply answered %d, want 204", code)
	}
	if _, ok := registry.Peers().Resolve("home"); !ok {
		t.Fatal("the parent is not in effect after Apply")
	}
	if got := runtime.Settings(); !got.Enabled || len(got.Peers) != 1 {
		t.Fatalf("settings = %+v", got)
	}

	if err := runtime.Apply(config.CascadeConfig{}); err != nil {
		t.Fatal(err)
	}
	if code := probe("shared-token-0123456789"); code != http.StatusNotFound {
		t.Fatalf("relay after turning cascade off answered %d, want 404", code)
	}
	if registry.Peers() != nil {
		t.Fatal("turning cascade off must clear the parents")
	}
}
