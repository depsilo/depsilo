package huggingface

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseXetReadTokenRoutesAreExplicit(t *testing.T) {
	tests := []struct {
		path    string
		wantKey string
		wantRef string
	}{
		{
			path:    "/api/models/acme/model/xet-read-token/main",
			wantKey: "huggingface/api/models/acme/model/xet-read-token/main",
			wantRef: "main",
		},
		{
			path:    "/api/datasets/acme/corpus/xet-read-token/v1",
			wantKey: "huggingface/api/datasets/acme/corpus/xet-read-token/v1",
			wantRef: "v1",
		},
		{
			path:    "/api/spaces/acme/demo/xet-read-token/0123456789abcdef0123456789abcdef01234567",
			wantKey: "huggingface/api/spaces/acme/demo/xet-read-token/0123456789abcdef0123456789abcdef01234567",
			wantRef: "0123456789abcdef0123456789abcdef01234567",
		},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			parsed := ParseRequestPath(test.path)
			if parsed.Kind == PathUnknown {
				t.Fatal("Xet read-token route was not recognized")
			}
			if parsed.Ref != test.wantRef {
				t.Fatalf("Ref = %q, want %q", parsed.Ref, test.wantRef)
			}
			if got := CacheKey(parsed); got != test.wantKey {
				t.Fatalf("CacheKey = %q, want %q", got, test.wantKey)
			}
		})
	}

	for _, path := range []string{
		"/api/models/acme/model/xet-read-token",
		"/api/models/acme/model/xet-read-token/main/extra",
		"/api/models/acme/model/xet-write-token/main",
		"/api/buckets/acme/bucket/xet-read-token/main",
		"/api/agent-harnesses",
	} {
		t.Run("reject "+path, func(t *testing.T) {
			if parsed := ParseRequestPath(path); parsed.Kind != PathUnknown {
				t.Fatalf("unexpectedly recognized route: %+v", parsed)
			}
		})
	}
}

func TestHandlerXetReadTokenIsDirectAndSanitizesCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)

	type observedRequest struct {
		method   string
		path     string
		rawQuery string
		auth     string
		cookie   string
		private  string
	}
	var requestCount atomic.Int64
	var observedMu sync.Mutex
	var observed []observedRequest
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sequence := requestCount.Add(1)
		observedMu.Lock()
		observed = append(observed, observedRequest{
			method:   r.Method,
			path:     r.URL.EscapedPath(),
			rawQuery: r.URL.RawQuery,
			auth:     r.Header.Get("Authorization"),
			cookie:   r.Header.Get("Cookie"),
			private:  r.Header.Get("X-Private-Header"),
		})
		observedMu.Unlock()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("X-Origin-Secret", "must-not-pass")
		w.Header().Set("X-Xet-Cas-Url", "https://cas.example.test")
		w.Header().Set("X-Xet-Access-Token", fmt.Sprintf("xet-header-token-%d", sequence))
		w.Header().Set("X-Xet-Token-Expiration", "1848535668")
		_, _ = fmt.Fprintf(
			w,
			`{"accessToken":"xet-token-%d","exp":1848535668,"casUrl":"https://cas.example.test"}`,
			sequence,
		)
	}))
	t.Cleanup(origin.Close)

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	const upstreamPath = "/api/models/acme/model/xet-read-token/main"
	const rawQuery = "audience=cas%2Baccess&audience=download"
	const path = "/huggingface" + upstreamPath + "?" + rawQuery

	for index := int64(1); index <= 2; index++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer depsilo_proj_routing-only")
		request.Header.Set("Cookie", "session=must-not-pass")
		request.Header.Set("X-Private-Header", "must-not-pass")
		router.ServeHTTP(recorder, request)

		wantBody := fmt.Sprintf(
			`{"accessToken":"xet-token-%d","exp":1848535668,"casUrl":"https://cas.example.test"}`,
			index,
		)
		if recorder.Code != http.StatusOK || recorder.Body.String() != wantBody {
			t.Fatalf("response %d = (%d, %q), want (200, %q)", index, recorder.Code, recorder.Body.String(), wantBody)
		}
		if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Fatalf("response %d Content-Type = %q", index, got)
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("response %d Cache-Control = %q, want no-store", index, got)
		}
		if got := recorder.Header().Get("X-Origin-Secret"); got != "" {
			t.Fatalf("response %d leaked unlisted origin header %q", index, got)
		}
		if got := recorder.Header().Get("X-Xet-Cas-Url"); got != "https://cas.example.test" {
			t.Fatalf("response %d X-Xet-Cas-Url = %q", index, got)
		}
		if got := recorder.Header().Get("X-Xet-Access-Token"); got != fmt.Sprintf("xet-header-token-%d", index) {
			t.Fatalf("response %d X-Xet-Access-Token was not the fresh upstream value", index)
		}
		if got := recorder.Header().Get("X-Xet-Token-Expiration"); got != "1848535668" {
			t.Fatalf("response %d X-Xet-Token-Expiration = %q", index, got)
		}
	}

	observedMu.Lock()
	requests := append([]observedRequest(nil), observed...)
	observedMu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("origin requests = %d, want 2 distinct token fetches", len(requests))
	}
	for index, request := range requests {
		if request.method != http.MethodGet ||
			request.path != upstreamPath ||
			request.rawQuery != rawQuery {
			t.Fatalf("origin request %d target = (%s, %q, query=%q)", index+1, request.method, request.path, request.rawQuery)
		}
		if request.auth != "" || request.cookie != "" || request.private != "" {
			t.Fatalf("origin request %d leaked headers: %+v", index+1, request)
		}
	}

	head := httptest.NewRecorder()
	router.ServeHTTP(head, httptest.NewRequest(http.MethodHead, path, nil))
	if head.Code != http.StatusMethodNotAllowed || head.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("HEAD response = (%d, Allow=%q), want (405, GET)", head.Code, head.Header().Get("Allow"))
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("HEAD generated an upstream token; origin requests = %d", got)
	}

	probe := httptest.NewRecorder()
	router.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/huggingface/api/agent-harnesses", nil))
	if probe.Code != http.StatusNotFound {
		body, _ := io.ReadAll(probe.Result().Body)
		t.Fatalf("agent-harnesses probe = (%d, %q), want 404", probe.Code, body)
	}
	if got := requestCount.Load(); got != 2 {
		t.Fatalf("agent-harnesses probe reached upstream; origin requests = %d", got)
	}
}
