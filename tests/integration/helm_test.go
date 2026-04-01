//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestHelm_IndexYaml(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/helm/index.yaml")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "testchart") {
		t.Error("index.yaml missing testchart")
	}
}

func TestHelm_ChartDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/helm/testchart-1.0.0.tgz")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_HELM_CHART" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestHelm_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/helm/testchart-1.0.0.tgz")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/helm/testchart-1.0.0.tgz")
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
