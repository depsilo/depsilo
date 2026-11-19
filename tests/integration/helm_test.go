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

func TestHelm_ChartMissThenHit(t *testing.T) {
	assertMissThenHit(t, depsiloURL+"/helm/testchart-1.0.0.tgz", "FAKE_HELM_CHART", false)
}
