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
	// The chart URL on the Upstream's origin becomes relative, so helm
	// downloads the chart through Depsilo.
	if !strings.Contains(body, "    - testchart-1.0.0.tgz\n") || strings.Contains(body, mockServer.URL()) {
		t.Errorf("index.yaml chart URL not made relative: %s", body)
	}
}

func TestHelm_ChartMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/helm/testchart-1.0.0.tgz", "FAKE_HELM_CHART", false)
}
