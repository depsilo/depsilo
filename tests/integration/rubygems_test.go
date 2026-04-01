//go:build integration

package integration

import (
	"testing"
)

func TestRubyGems_Specs(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/rubygems/specs.4.8.gz")
	assertStatus(t, resp, 200)
}

func TestRubyGems_Info(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/rubygems/info/testgem")
	assertStatus(t, resp, 200)
}

func TestRubyGems_GemDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/rubygems/gems/testgem-1.0.0.gem")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_GEM" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestRubyGems_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/rubygems/gems/testgem-1.0.0.gem")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/rubygems/gems/testgem-1.0.0.gem")
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
