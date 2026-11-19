//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestCargo_ConfigJson(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/crates/config.json")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	// dl field should be rewritten to depsilo
	if !strings.Contains(body, depsiloURL) {
		t.Error("config.json dl not rewritten to depsilo")
	}
}

func TestCargo_Index(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/crates/se/rd/serde")
	assertStatus(t, resp, 200)
}

func TestCargo_CrateMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/crates/api/v1/crates/serde/1.0.0/download", "FAKE_CRATE", false)
}
