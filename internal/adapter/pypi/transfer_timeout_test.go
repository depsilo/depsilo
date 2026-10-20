package pypi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"depsilo/internal/cache"
)

func TestArtifactIdleReadCancelsWithoutCommittingPartialWheel(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(origin.Close)
	family, _ := newTestChannelFamily(t, origin.URL)
	prototype := family.prototype
	handler := New(prototype.cacheMgr, prototype.selector, prototype.cfg, prototype.db)
	handler.artifactIdleTimeout = 20 * time.Millisecond
	router := gin.New()
	handler.Register(router.Group("/pypi"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ctx, tracker := cache.WithTrackedForceRefresh(ctx)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet,
		"/pypi/files/packages/torch-1.0.0-py3-none-any.whl", nil).WithContext(ctx))
	if err := tracker.Wait(ctx); !errors.Is(err, cache.ErrFetchIdleTimeout) {
		t.Fatalf("artifact persistence error = %v, want idle timeout", err)
	}
	var committed int64
	if err := prototype.db.Table("cache_entries").Count(&committed).Error; err != nil {
		t.Fatal(err)
	}
	if committed != 0 {
		t.Fatalf("stalled wheel committed %d cache entries", committed)
	}
}

func TestArtifactTransferDoesNotInheritMetadataTotalTimeout(t *testing.T) {
	for _, channel := range []bool{false, true} {
		name := "pypi"
		if channel {
			name = "pytorch-channel"
		}
		t.Run(name, func(t *testing.T) {
			const payload = "complete-wheel"
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".whl") {
					_, _ = io.WriteString(w, payload)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, `<a href="/packages/torch-1.0.0-py3-none-any.whl">torch</a>`)
			}))
			t.Cleanup(origin.Close)
			family, _ := newTestChannelFamily(t, origin.URL)
			injectTimeout := false
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if injectTimeout {
					c.Request = c.Request.WithContext(cache.WithFetchTimeout(c.Request.Context(), time.Nanosecond))
				}
			})
			prefix := "/pypi"
			if channel {
				family.Register(router.Group("/pypi-torch"))
				prefix = "/pypi-torch/cpu"
			} else {
				prototype := family.prototype
				New(prototype.cacheMgr, prototype.selector, prototype.cfg, prototype.db).Register(router.Group(prefix))
			}
			index := httptest.NewRecorder()
			router.ServeHTTP(index, httptest.NewRequest(http.MethodGet, prefix+"/simple/torch/", nil))
			if index.Code != http.StatusOK {
				t.Fatalf("index status = %d", index.Code)
			}
			artifactURL, err := url.Parse(hrefFromMatch(hrefRe.FindStringSubmatch(index.Body.String())))
			if err != nil || artifactURL.Path == "" {
				t.Fatalf("artifact URL missing: %v", err)
			}
			injectTimeout = true
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, artifactURL.RequestURI(), nil))
			if response.Code != http.StatusOK || response.Body.String() != payload {
				t.Fatalf("artifact response = (%d, %q), want complete wheel", response.Code, response.Body.String())
			}
			metadata := httptest.NewRecorder()
			router.ServeHTTP(metadata, httptest.NewRequest(http.MethodGet, prefix+"/simple/uncached/", nil))
			if metadata.Code != http.StatusBadGateway {
				t.Fatalf("uncached metadata status = %d, want bounded fetch failure", metadata.Code)
			}
		})
	}
}
