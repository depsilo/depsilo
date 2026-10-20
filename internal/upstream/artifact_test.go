package upstream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/traffic"
)

func breakArtifactResponse(w http.ResponseWriter, data string) {
	_, _ = io.WriteString(w, data)
	w.(http.Flusher).Flush()
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		_ = conn.Close()
	}
}

func TestArtifactResumeRejectsUnprovenRepresentations(t *testing.T) {
	for _, kind := range []string{"no-etag", "weak-etag", "ignored-range", "changed-etag", "wrong-range", "wrong-size", "encoded", "changed-target", "exhausted"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := calls.Add(1)
				w.Header().Set("ETag", `"v1"`)
				if call == 1 {
					if kind == "no-etag" {
						w.Header().Del("ETag")
					}
					if kind == "weak-etag" {
						w.Header().Set("ETag", `W/"v1"`)
					}
					w.Header().Set("Content-Length", "9")
					breakArtifactResponse(w, "abc")
					return
				}
				if kind == "changed-target" && r.URL.Path != "/changed" {
					http.Redirect(w, r, "/changed", http.StatusFound)
					return
				}
				offset := 3
				if kind == "exhausted" {
					_, _ = fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-", &offset)
				}
				w.Header().Set("Content-Length", fmt.Sprint(9-offset))
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-8/9", offset))
				status := 206
				switch kind {
				case "ignored-range":
					status = 200
				case "changed-etag":
					w.Header().Set("ETag", `"v2"`)
				case "wrong-range":
					w.Header().Set("Content-Range", "bytes 0-5/9")
				case "wrong-size":
					w.Header().Set("Content-Length", "5")
				case "encoded":
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.WriteHeader(status)
				if kind == "exhausted" {
					breakArtifactResponse(w, "abcdefghi"[offset:offset+1])
					return
				}
				_, _ = io.WriteString(w, "BADBAD")
			}))
			defer origin.Close()
			u := newTestUpstream(t, origin.URL)
			result, err := u.FetchArtifact(context.Background(), "/wheel")
			if err != nil {
				t.Fatal(err)
			}
			defer result.Body.Close()
			data, err := io.ReadAll(result.Body)
			if err == nil {
				t.Fatal("unsafe transfer unexpectedly succeeded")
			}
			if kind != "exhausted" && string(data) != "abc" {
				t.Fatalf("unsafe resume appended bytes: %q", data)
			}
			if kind == "exhausted" && (calls.Load() != 3 || string(data) != "abcde") {
				t.Fatalf("attempt budget: calls=%d, bytes=%q", calls.Load(), data)
			}
			if calls.Load() > 3 {
				t.Fatalf("unbounded resume attempts: %d", calls.Load())
			}
			if (kind == "no-etag" || kind == "weak-etag") && calls.Load() != 1 {
				t.Fatal("resumed without a strong validator")
			}
		})
	}
}

func TestArtifactResumeMetersBothOriginReads(t *testing.T) {
	var calls atomic.Int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"v1"`)
		if r.Header.Get("Range") == "" {
			w.Header().Set("Content-Length", "9")
			breakArtifactResponse(w, "abc")
			return
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			t.Error("resume must request identity encoding")
		}
		w.Header().Set("Content-Length", "6")
		w.Header().Set("Content-Range", "bytes 3-8/9")
		w.WriteHeader(206)
		_, _ = io.WriteString(w, "defghi")
	}))
	defer origin.Close()
	attr := traffic.NewAttribution()
	ctx := traffic.WithAttribution(context.Background(), attr)
	u := newTestUpstream(t, origin.URL)
	result, err := u.FetchArtifactURL(ctx, origin.URL+"/wheel")
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil || string(data) != "abcdefghi" {
		t.Fatalf("body=%q err=%v", data, err)
	}
	requests, bytes := attr.OriginTotals()
	if requests != 2 || bytes != 9 {
		t.Fatalf("origin totals=(%d,%d), want (2,9)", requests, bytes)
	}
}

func TestArtifactCloseCancelsBlockedResume(t *testing.T) {
	resumeStarted := make(chan struct{})
	resumeStopped := make(chan struct{})
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") == "" {
			w.Header().Set("ETag", `"v1"`)
			w.Header().Set("Content-Length", "9")
			breakArtifactResponse(w, "abc")
			return
		}
		close(resumeStarted)
		<-r.Context().Done()
		close(resumeStopped)
	}))
	defer origin.Close()
	u := newTestUpstream(t, origin.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := u.FetchArtifact(ctx, "/wheel")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _, _ = io.Copy(io.Discard, result.Body) }()
	select {
	case <-resumeStarted:
	case <-ctx.Done():
		t.Fatal("resume did not start")
	}
	_ = result.Body.Close()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("Close did not unblock reader")
	}
	select {
	case <-resumeStopped:
	case <-ctx.Done():
		t.Fatal("resume request leaked")
	}
}
