package huggingface_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/adapter/huggingface"
)

func TestResolver_Direct200(t *testing.T) {
	// Upstream returns 200 directly (non-LFS small file like config.json).
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"hidden":false}`))
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/test/resolve/main/config.json", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/test/resolve/main/config.json", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("X-Repo-Commit"); got != "abc1234" {
		t.Errorf("X-Repo-Commit = %q, want abc1234", got)
	}
	if !strings.Contains(w.Body.String(), `"hidden":false`) {
		t.Errorf("body did not contain expected JSON: %s", w.Body.String())
	}
}

func TestResolver_Follow302ToCDN(t *testing.T) {
	// CDN server: serves the actual blob bytes when fetched with the
	// signed URL. Receives no Authorization header.
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("CDN must not receive Authorization header; got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "11")
		w.WriteHeader(200)
		w.Write([]byte("FAKE_WEIGHT"))
	}))
	defer cdn.Close()

	// Upstream "huggingface.co": 302 to the CDN with HF headers
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Linked-Etag", "deadbeef")
		w.Header().Set("X-Linked-Size", "11")
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Location", cdn.URL+"/blob?sig=zzz")
		w.WriteHeader(302)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/repo/resolve/main/weights.bin", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/repo/resolve/main/weights.bin", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200 (resolver should follow 302 internally)", w.Code)
	}
	if w.Body.String() != "FAKE_WEIGHT" {
		t.Errorf("body = %q, want FAKE_WEIGHT", w.Body.String())
	}
	if got := w.Header().Get("X-Linked-Etag"); got != "deadbeef" {
		t.Errorf("X-Linked-Etag = %q, want deadbeef (must be passed through)", got)
	}
	if got := w.Header().Get("X-Repo-Commit"); got != "abc1234" {
		t.Errorf("X-Repo-Commit = %q, want abc1234", got)
	}
}

func TestResolver_AuthForwardedToUpstreamNotCDN(t *testing.T) {
	upstreamGotAuth := false
	cdnGotAuth := false

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			cdnGotAuth = true
		}
		w.Write([]byte("ok"))
	}))
	defer cdn.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer testtoken" {
			upstreamGotAuth = true
		}
		w.Header().Set("Location", cdn.URL+"/blob")
		w.WriteHeader(302)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/repo/resolve/main/file", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/repo/resolve/main/file", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if !upstreamGotAuth {
		t.Error("upstream must receive Authorization header")
	}
	if cdnGotAuth {
		t.Error("CDN must NOT receive Authorization header (signed URL carries its own auth)")
	}
}

func TestResolver_404PassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":"Entry not found"}`))
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.GET("/missing/resolve/main/x", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodGet, "/missing/resolve/main/x", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Entry not found") {
		t.Errorf("body = %q, want pass-through of upstream body", w.Body.String())
	}
}

func TestResolver_HEAD_BodyNotFetched(t *testing.T) {
	cdnGotRequest := false
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnGotRequest = true
		w.Write([]byte("body"))
	}))
	defer cdn.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("upstream got %s, want HEAD", r.Method)
		}
		w.Header().Set("X-Linked-Etag", "deadbeef")
		w.Header().Set("X-Linked-Size", "11")
		w.Header().Set("Location", cdn.URL+"/blob")
		w.WriteHeader(302)
	}))
	defer upstream.Close()

	gin.SetMode(gin.TestMode)
	g := gin.New()
	res := huggingface.NewResolver()
	g.HEAD("/repo/resolve/main/weights.bin", func(c *gin.Context) {
		res.Handle(c, upstream.URL, c.Request.URL.Path)
	})

	req := httptest.NewRequest(http.MethodHead, "/repo/resolve/main/weights.bin", nil)
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("HEAD response had body of %d bytes, want 0", w.Body.Len())
	}
	if got := w.Header().Get("X-Linked-Etag"); got != "deadbeef" {
		t.Errorf("X-Linked-Etag = %q, want deadbeef (header passes through on HEAD)", got)
	}
	if cdnGotRequest {
		t.Error("CDN must NOT be hit for HEAD requests — only the upstream HEAD response is needed")
	}
}
