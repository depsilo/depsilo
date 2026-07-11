package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"depsilo/internal/db"
)

func TestRegistryCheckPersistsUpstreamIDEvenWhenNetworkIsUnhealthy(t *testing.T) {
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "down", URL: "http://127.0.0.1:1", Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	resource, check, err := registry.Check(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if resource.Healthy || check.Healthy || check.Err == nil {
		t.Fatalf("resource=%#v check=%#v", resource, check)
	}
	var log db.UpstreamLatencyLog
	if err := database.Order("id desc").First(&log).Error; err != nil {
		t.Fatal(err)
	}
	if log.UpstreamID != record.ID {
		t.Fatalf("upstream_id=%d want=%d", log.UpstreamID, record.ID)
	}
}

func TestRegistryCheckUsesConfiguredProxyClient(t *testing.T) {
	var proxyHits atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		proxyHits.Add(1)
		if request.Method != http.MethodHead || request.URL.Host != "origin.invalid" {
			t.Errorf("request=%s %s", request.Method, request.URL.String())
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()
	database := bootstrapDB(t)
	record := db.UpstreamRecord{AdapterType: "pypi", Name: "proxied", URL: "http://origin.invalid", Proxy: proxy.URL, Priority: 1, ProbeMode: "passive", ProbeInterval: "30m", Healthy: true}
	if err := database.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(database, []string{"pypi"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, result, err := registry.Check(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if proxyHits.Load() != 1 || !result.Healthy || !runtime.Healthy {
		t.Fatalf("proxy_hits=%d runtime=%#v result=%#v", proxyHits.Load(), runtime, result)
	}
}
