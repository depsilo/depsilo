package goproxy

import (
	"net/http"
	"testing"
)

// proxy.golang.org answers a module version it does not have with 410 Gone.
// The go command reports that as not found and falls back to its next GOPROXY
// entry only for 404 or 410; a 502 would stop it with an error.
func TestModuleProxyRelaysAGoneVersionAsGone(t *testing.T) {
	mirror := newSumDBMirror(t)
	mirror.answer(http.StatusGone)
	engine, _ := newSumDBRoute(t, sumDBTTL, mirror)

	for _, path := range []string{
		"/go/example.com/mod/@v/v9.9.9.info",
		"/go/example.com/mod/@v/v9.9.9.mod",
		"/go/example.com/mod/@v/v9.9.9.zip",
		"/go/example.com/mod/@latest",
	} {
		if response := getSumDB(engine, path); response.Code != http.StatusGone {
			t.Errorf("GET %s on a proxy answering 410 = %d, want 410", path, response.Code)
		}
	}
}
