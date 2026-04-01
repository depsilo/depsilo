//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestMaven_PomDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/maven/org/example/test/1.0.0/test-1.0.0.pom")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "<artifactId>test</artifactId>") {
		t.Error("POM content missing")
	}
}

func TestMaven_JarDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/maven/org/example/test/1.0.0/test-1.0.0.jar")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if body != "FAKE_JAR" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestMaven_Metadata(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/maven/org/example/test/maven-metadata.xml")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	if !strings.Contains(body, "<version>1.0.0</version>") {
		t.Error("metadata content missing")
	}
}

func TestMaven_CacheHit(t *testing.T) {
	httpGet(t, depsiloURL+"/maven/org/example/test/1.0.0/test-1.0.0.jar")
	before := mockServer.RequestCount()
	resp := httpGet(t, depsiloURL+"/maven/org/example/test/1.0.0/test-1.0.0.jar")
	assertStatus(t, resp, 200)
	if mockServer.RequestCount() != before {
		t.Error("expected cache hit")
	}
}
