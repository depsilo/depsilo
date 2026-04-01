//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestGo_ModuleList(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/list")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "v1.0.0") {
		t.Error("version list missing v1.0.0")
	}
}

func TestGo_ModuleInfo(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/v1.0.0.info")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, `"Version":"v1.0.0"`) {
		t.Error("version info missing")
	}
}

func TestGo_ModuleMod(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/v1.0.0.mod")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "module github.com/test/mod") {
		t.Error("go.mod content missing")
	}
}

func TestGo_ModuleZip(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/v1.0.0.zip")
	assertStatus(t, resp, 200)
}

func TestGo_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/v1.0.0.zip")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/go/github.com/test/mod/@v/v1.0.0.zip")
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
