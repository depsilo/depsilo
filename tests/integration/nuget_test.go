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

func TestNuGet_QueryIsForwardedAndCachedPerVariant(t *testing.T) {
	const first = "q=Integration%2FOne&skip=10&take=5"
	const second = "q=Integration%2FTwo&skip=0&take=5"
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/nuget/v3/search?"+first)
	assertStatus(t, resp, 200)
	readBody(t, resp)
	resp = httpGet(t, depsiloURL+"/nuget/v3/search?"+second)
	assertStatus(t, resp, 200)
	readBody(t, resp)

	requests := mockServer.Requests()[before:]
	seen := map[string]bool{}
	for _, request := range requests {
		if request.Path == "/v3/search" {
			seen[request.RawQuery] = true
		}
	}
	if !seen[first] || !seen[second] {
		t.Fatalf("NuGet upstream queries = %#v, want both exact variants", seen)
	}

	before = mockServer.RequestCount()
	resp = httpGet(t, depsiloURL+"/nuget/v3/search?"+first)
	assertStatus(t, resp, 200)
	readBody(t, resp)
	if got := mockServer.RequestCount(); got != before {
		t.Fatalf("cached NuGet query contacted upstream: before=%d after=%d", before, got)
	}
}
