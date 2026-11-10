package pypi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/config"
)

// countingOrigin serves artifacts named after its label and answers 404 for
// paths matched by missing.
func countingOrigin(t *testing.T, label string, missing func(path string) bool) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if missing(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, label+":"+lastPathSegment(r.URL.Path))
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

// A mirror can lack a file that another pool member's index advertised: PEP
// 658 core metadata, which mirrors such as TUNA do not publish, or a release
// it has not synchronized yet. The download then comes from the next member.
func TestPypiArtifactMissingOnMirrorComesFromAnotherPoolMember(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mirror, mirrorHits := countingOrigin(t, "mirror", func(path string) bool {
		return strings.HasSuffix(path, ".metadata") || strings.Contains(path, "-2.0.0-")
	})
	official, officialHits := countingOrigin(t, "official", func(path string) bool {
		return strings.Contains(path, "absent")
	})
	handler, scoped, _, pool := newPoolTestHandler(t, Options{PathPrefix: "/pypi", AdapterID: "pypi"},
		config.UpstreamConfig{Name: "mirror", URL: mirror.URL, Priority: 1, ProbeMode: "passive"},
		config.UpstreamConfig{Name: "official", URL: official.URL, Priority: 2, ProbeMode: "passive"})
	handler.SetBlocklistCovered(true)

	for _, tc := range []struct {
		name, path, body         string
		fromMirror, fromOfficial int64
	}{
		{"core metadata the mirror does not publish", "packages/ab/cd/acme_demo-1.0.0-py3-none-any.whl.metadata", "official:acme_demo-1.0.0-py3-none-any.whl.metadata", 1, 1},
		{"release the mirror has not synchronized", "packages/ab/cd/acme_demo-2.0.0-py3-none-any.whl", "official:acme_demo-2.0.0-py3-none-any.whl", 1, 1},
		{"artifact the mirror serves", "packages/ab/cd/acme_demo-1.0.0-py3-none-any.whl", "mirror:acme_demo-1.0.0-py3-none-any.whl", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mirrorBefore, officialBefore := mirrorHits.Load(), officialHits.Load()
			for attempt := range 2 {
				// The second request is a cache hit for whichever member served it.
				response := requestPypiFile(t, scoped, tc.path)
				if response.Code != http.StatusOK || response.Body.String() != tc.body {
					t.Fatalf("attempt %d: status=%d body=%q", attempt+1, response.Code, response.Body.String())
				}
			}
			if got := [2]int64{mirrorHits.Load() - mirrorBefore, officialHits.Load() - officialBefore}; got != [2]int64{tc.fromMirror, tc.fromOfficial} {
				t.Fatalf("origin requests (mirror, official) = %v, want (%d, %d)", got, tc.fromMirror, tc.fromOfficial)
			}
		})
	}
	// A 404 does not count against a member: the mirror stays preferred.
	for _, member := range pool.Snapshot() {
		if !member.IsHealthy() {
			t.Fatalf("%s was marked unhealthy by a 404", member.Name)
		}
	}

	mirrorBefore, officialBefore := mirrorHits.Load(), officialHits.Load()
	missing := requestPypiFile(t, scoped, "packages/ab/cd/absent-1.0.0-py3-none-any.whl.metadata")
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), `"code":"NOT_FOUND"`) {
		t.Fatalf("artifact missing everywhere: status=%d body=%s", missing.Code, missing.Body.String())
	}
	if mirrorHits.Load()-mirrorBefore != 1 || officialHits.Load()-officialBefore != 1 {
		t.Fatal("an artifact missing everywhere was not requested exactly once from each member")
	}
}

// A signed external reference names one exact artifact URL and source, so a
// 404 there is final rather than a reason to ask another pool member.
func TestPypiExternalArtifactNotFoundIsNotRetriedElsewhere(t *testing.T) {
	gin.SetMode(gin.TestMode)
	host, hostHits := countingOrigin(t, "host", func(string) bool { return true })
	other, otherHits := countingOrigin(t, "other", func(string) bool { return false })
	_, scoped, _, _ := newPoolTestHandler(t,
		Options{PathPrefix: "/pypi", AdapterID: "pypi", UpstreamSimplePath: "/simple", ArtifactSigningKey: testArtifactSigningKey},
		config.UpstreamConfig{Name: "host", URL: host.URL, Priority: 1, ProbeMode: "passive"},
		config.UpstreamConfig{Name: "other", URL: other.URL, Priority: 2, ProbeMode: "passive"})
	token, err := encodeExternalArtifactToken(testArtifactSigningKey, "pypi", externalArtifactClaims{
		Target: host.URL + "/packages/demo-1.0-py3-none-any.whl",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := requestPypiFile(t, scoped, "_external/"+token+"/demo-1.0-py3-none-any.whl")
	if response.Code != http.StatusNotFound || hostHits.Load() != 1 || otherHits.Load() != 0 {
		t.Fatalf("status=%d host=%d other=%d", response.Code, hostHits.Load(), otherHits.Load())
	}
}
