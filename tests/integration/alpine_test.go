//go:build integration

package integration

import (
	"net/http"
	"testing"
)

func TestAlpineAPKIndexIsServedThroughPublicRepositoryRoute(t *testing.T) {
	response := httpGet(t, depsiloURL+"/alpine/v3.23/main/x86_64/APKINDEX.tar.gz")
	defer response.Body.Close()
	body := readBody(t, response)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("APKINDEX status = %d, want 200; body=%q", response.StatusCode, body)
	}
	if body != "FAKE_ALPINE_SIGNED_APKINDEX" {
		t.Fatalf("APKINDEX body = %q, want signed fixture bytes", body)
	}
}
