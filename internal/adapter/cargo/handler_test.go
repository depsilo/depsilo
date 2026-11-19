package cargo

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter"
	"depsilo/internal/adapter/adaptertest"
	"depsilo/internal/config"
)

func TestPackageUpstreamFailuresAreRecordedAsAuditErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	manager, database, _ := adaptertest.NewManager(t, 72*time.Hour)
	audit := &adaptertest.AuditCapture{}
	scope := adapter.NewRequestScope(nil, audit, nil, nil)

	handler := New(manager, adaptertest.UnavailableSelector{}, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: 72 * time.Hour}, database)
	router := gin.New()
	handler.Register(router.Group("/crates"))
	for _, target := range []string{
		"/crates/se/rd/serde",
		"/crates/api/v1/crates/serde/1.0.219/download",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, target, nil)
		scope.Wrap(router).ServeHTTP(response, request)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("GET %s status = %d, body=%s", target, response.Code, response.Body.String())
		}
	}

	if len(audit.Entries) != 2 {
		t.Fatalf("audit entries = %#v", audit.Entries)
	}
	for index, entry := range audit.Entries {
		if entry.Ecosystem != "cargo" || entry.CacheResult != "error" || entry.StatusCode != http.StatusBadGateway {
			t.Errorf("audit entry %d = %#v", index, entry)
		}
	}
	if audit.Entries[0].Action != "metadata" || audit.Entries[0].PackageName != "" || audit.Entries[0].Version != "" {
		t.Errorf("index audit entry = %#v", audit.Entries[0])
	}
	if audit.Entries[1].Action != "download" || audit.Entries[1].PackageName != "serde" || audit.Entries[1].Version != "1.0.219" {
		t.Errorf("download audit entry = %#v", audit.Entries[1])
	}
}

// X-Forwarded-Proto reaches the download URL written into config.json. Two
// chained proxies send a list, and any client can send anything, so the value
// must never become the URL scheme as it is.
func TestConfigDownloadURLTakesOnlyAnHTTPSchemeFromForwardedProto(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"dl":"https://static.crates.io/crates","api":"https://crates.io"}`)
	}))
	t.Cleanup(upstreamServer.Close)
	handler, _ := newCargoTestHandler(t, upstreamServer.URL, config.CacheConfig{TTLIndex: time.Hour, TTLBlob: time.Hour})

	for _, test := range []struct {
		forwarded string
		want      string
	}{
		{forwarded: "https, http", want: "https://depsilo.example.test/crates/api/v1/crates"},
		{forwarded: "gopher", want: "http://depsilo.example.test/crates/api/v1/crates"},
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/crates/config.json", nil)
		request.Host = "depsilo.example.test"
		request.Header.Set("X-Forwarded-Proto", test.forwarded)
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("X-Forwarded-Proto %q: status = %d, body=%s", test.forwarded, response.Code, response.Body.String())
		}
		var document struct {
			DL string `json:"dl"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
			t.Fatalf("X-Forwarded-Proto %q: decode config.json: %v", test.forwarded, err)
		}
		if document.DL != test.want {
			t.Errorf("X-Forwarded-Proto %q: dl = %q, want %q", test.forwarded, document.DL, test.want)
		}
	}
}
