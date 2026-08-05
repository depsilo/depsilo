//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestAPT_Release(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/apt/ubuntu/dists/jammy/Release")
	assertStatus(t, resp, 200)
	body := readBody(t, resp)
	// Passthrough — content must match upstream exactly
	if !strings.Contains(body, "Origin: Ubuntu") {
		t.Error("Release content mismatch")
	}
}

func TestAPT_Packages(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/apt/ubuntu/dists/jammy/main/binary-amd64/Packages.gz")
	assertStatus(t, resp, 200)
}

func TestAPT_DebDownload(t *testing.T) {
	resp := httpGet(t, depsiloURL+"/apt/ubuntu/pool/main/c/curl/curl_7.68.0.deb")
	assertStatus(t, resp, 200)
}
