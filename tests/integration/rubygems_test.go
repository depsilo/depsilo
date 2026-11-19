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

func TestRubyGems_GemMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/rubygems/gems/testgem-1.0.0.gem", "FAKE_GEM", false)
}
