package pypi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"depsilo/internal/cache"
	"github.com/gin-gonic/gin"
)

func TestArtifactRecoversInterruptedColdTransfer(t *testing.T) {
	for _, kind := range []string{"pypi", "pytorch-signed", "changed-etag"} {
		t.Run(kind, func(t *testing.T) {
			const payload = "complete-wheel-bytes"
			var requests atomic.Int32
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, ".whl") {
					w.Header().Set("Content-Type", "text/html")
					_, _ = io.WriteString(w, `<a href="/packages/demo-1.0-py3-none-any.whl">demo</a>`)
					return
				}
				requests.Add(1)
				w.Header().Set("ETag", `"wheel-v1"`)
				w.Header().Set("Content-Type", "application/octet-stream")
				if r.Header.Get("Range") == "" {
					w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
					_, _ = io.WriteString(w, payload[:5])
					w.(http.Flusher).Flush()
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						_ = conn.Close()
					}
					return
				}
				if r.Header.Get("Range") != "bytes=5-" || r.Header.Get("If-Range") != `"wheel-v1"` {
					t.Errorf("unsafe resume headers: %q %q", r.Header.Get("Range"), r.Header.Get("If-Range"))
				}
				if kind == "changed-etag" {
					w.Header().Set("ETag", `"wheel-v2"`)
				}
				w.Header().Set("Content-Range", fmt.Sprintf("bytes 5-%d/%d", len(payload)-1, len(payload)))
				w.Header().Set("Content-Length", fmt.Sprint(len(payload)-5))
				w.WriteHeader(http.StatusPartialContent)
				_, _ = io.WriteString(w, payload[5:])
			}))
			t.Cleanup(origin.Close)
			family, _ := newTestChannelFamily(t, origin.URL)
			p := family.prototype
			handler := New(p.cacheMgr, p.selector, p.cfg, p.db)
			router := gin.New()
			target := "/pypi/files/packages/demo-1.0-py3-none-any.whl"
			if kind == "pytorch-signed" {
				family.Register(router.Group("/pypi-torch"))
				index := httptest.NewRecorder()
				router.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/pypi-torch/cpu/simple/demo/", nil))
				parsed, err := url.Parse(hrefFromMatch(hrefRe.FindStringSubmatch(index.Body.String())))
				if err != nil || parsed.Path == "" {
					t.Fatal("missing signed artifact reference")
				}
				target = parsed.RequestURI()
			} else {
				handler.Register(router.Group("/pypi"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ctx, tracker := cache.WithTrackedForceRefresh(ctx)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil).WithContext(ctx))
			if kind == "changed-etag" {
				if response.Body.String() != payload[:5] {
					t.Fatalf("changed object appended bytes: %q", response.Body.String())
				}
				if tracker.Wait(ctx) == nil {
					t.Fatal("changed object was committed")
				}
				var count int64
				if err := p.db.Table("cache_entries").Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				snapshots := p.cacheMgr.TransferSnapshots()
				if len(snapshots) != 1 || snapshots[0].Status != "failed" {
					t.Fatalf("failed fill not reflected in progress: %+v", snapshots)
				}
				if count != 0 {
					t.Fatal("partial object left committed cache entry")
				}
				return
			}
			if response.Code != 200 || response.Body.String() != payload {
				t.Fatalf("interrupted transfer returned (%d,%q), want full wheel", response.Code, response.Body.String())
			}
			if err := tracker.Wait(ctx); err != nil {
				t.Fatal(err)
			}
			snapshots := p.cacheMgr.TransferSnapshots()
			if len(snapshots) != 1 || snapshots[0].Status != "cached" || snapshots[0].ResumeAttempts != 1 || snapshots[0].DownloadedBytes != int64(len(payload)) {
				t.Fatalf("missing completed resume snapshot: %+v", snapshots)
			}
			if requests.Load() != 2 {
				t.Fatalf("origin requests=%d, want 2", requests.Load())
			}
			cached := httptest.NewRecorder()
			router.ServeHTTP(cached, httptest.NewRequest(http.MethodGet, target, nil))
			if cached.Body.String() != payload || requests.Load() != 2 {
				t.Fatal("recovered object was not reused from cache")
			}

		})
	}
}
