package huggingface

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"depsilo/internal/db"
)

const deliveryTestPath = "/huggingface/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/model.bin"

// A writer can reject the body after a successful status has been committed.
type interruptedResponseWriter struct {
	*httptest.ResponseRecorder
}

func (w interruptedResponseWriter) Write(body []byte) (int, error) {
	n, _ := w.ResponseRecorder.Write(body[:min(3, len(body))])
	return n, io.ErrClosedPipe
}

func TestHandlerRecordsClientInterruptedDelivery(t *testing.T) {
	for _, mode := range []string{"cache miss", "cache hit", "direct"} {
		t.Run(mode, func(t *testing.T) {
			const body = "model artifact bytes"
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
				_, _ = io.WriteString(w, body)
			}))
			t.Cleanup(origin.Close)
			handler, _, database := newCachingTestHandler(t, origin.URL)
			if mode == "direct" {
				handler.cacheMgr = nil
			}
			router := gin.New()
			handler.Register(router.Group("/huggingface"))
			if mode == "cache hit" {
				warm := httptest.NewRecorder()
				router.ServeHTTP(warm, httptest.NewRequest(http.MethodGet, deliveryTestPath, nil))
				if warm.Code != http.StatusOK || warm.Body.String() != body {
					t.Fatalf("warm response = (%d, %q)", warm.Code, warm.Body.String())
				}
				waitForCacheEntry(t, database, "huggingface/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/model.bin")
			}
			writer := interruptedResponseWriter{httptest.NewRecorder()}
			router.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, deliveryTestPath, nil))
			if writer.Code != http.StatusOK {
				t.Fatalf("status = %d, want unchanged committed 200", writer.Code)
			}
			var log db.AccessLog
			if err := database.Last(&log).Error; err != nil {
				t.Fatal(err)
			}
			if log.StatusCode != http.StatusOK || log.DeliveryResult != "failed" || log.DeliveryReason != "response_body_incomplete" {
				t.Fatalf("interrupted delivery log = %+v", log)
			}
			if log.Hit != (mode == "cache hit") {
				t.Fatalf("cache hit = %v for %s", log.Hit, mode)
			}
		})
	}
}

func TestHandlerRecordsDirectBodyCompletionButNotHEAD(t *testing.T) {
	const body = "model artifact bytes"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)
	handler, _, database := newCachingTestHandler(t, origin.URL)
	handler.cacheMgr = nil
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, deliveryTestPath, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d", method, recorder.Code)
		}
		var log db.AccessLog
		if err := database.Last(&log).Error; err != nil {
			t.Fatal(err)
		}
		want := "completed"
		if method == http.MethodHead {
			want = "unknown"
		}
		if log.DeliveryResult != want {
			t.Fatalf("%s delivery = %q, want %q", method, log.DeliveryResult, want)
		}
	}
}

func TestHandlerRecordsTruncatedUpstreamBodyAsFailedDelivery(t *testing.T) {
	const body = "incomplete model"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+100))
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(origin.Close)
	handler, _, database := newCachingTestHandler(t, origin.URL)
	handler.cacheMgr = nil
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, deliveryTestPath, nil))
	var log db.AccessLog
	if err := database.Last(&log).Error; err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || log.StatusCode != http.StatusOK || log.DeliveryResult != "failed" {
		t.Fatalf("truncated delivery status = %d, log = %+v", recorder.Code, log)
	}
}
