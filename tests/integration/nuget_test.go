//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestNuGet_ServiceIndex(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/nuget/v3/index.json")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	// @id fields should be rewritten
	if strings.Contains(body, "api.nuget.org") {
		t.Error("external @id not rewritten")
	}
	if !strings.Contains(body, depsiloURL) {
		t.Error("local URL not found in service index")
	}
}

func TestNuGet_Passthrough(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/nuget/v3/search")
	assertStatus(t, resp, 200)
}
