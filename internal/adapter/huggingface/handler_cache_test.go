package huggingface

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/db"
	"depsilo/internal/middleware"
	"depsilo/internal/upstream"
)

func TestHandlerCachesResolvedLFSAndServesOffline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const body = "cached model weights"
	var originRequests atomic.Int64
	var cdnRequests atomic.Int64
	var offline atomic.Bool

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cdnRequests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "20")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(cdn.Close)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originRequests.Add(1)
		if offline.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Location", cdn.URL+"/blob?signature=secret")
		w.Header().Set("X-Linked-Etag", "sha256:model")
		w.Header().Set("X-Linked-Size", "20")
		w.Header().Set("X-Repo-Commit", "0123456789abcdef0123456789abcdef01234567")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	handler, manager, database := newCachingTestHandler(t, origin.URL)
	availability := &availabilitySelector{delegate: handler.selector}
	handler.selector = availability
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	path := "/huggingface/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/model.bin"
	request := func() *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		return recorder
	}

	first := request()
	if first.Code != http.StatusOK || first.Body.String() != body {
		t.Fatalf("first response = (%d, %q), want (200, %q)", first.Code, first.Body.String(), body)
	}
	waitForCacheEntry(t, database, "huggingface/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/model.bin")
	availability.offline.Store(true)
	second := request()
	if second.Code != http.StatusOK || second.Body.String() != body {
		t.Fatalf("second response = (%d, %q), want (200, %q)", second.Code, second.Body.String(), body)
	}
	for _, response := range []*httptest.ResponseRecorder{first, second} {
		if got := response.Header().Get("X-Linked-Etag"); got != "sha256:model" {
			t.Fatalf("cached X-Linked-Etag = %q, want sha256:model", got)
		}
		if got := response.Header().Get("X-Repo-Commit"); got != "0123456789abcdef0123456789abcdef01234567" {
			t.Fatalf("cached X-Repo-Commit = %q", got)
		}
	}
	if got := originRequests.Load(); got != 1 {
		t.Fatalf("origin requests after warm hit = %d, want 1", got)
	}
	if got := cdnRequests.Load(); got != 1 {
		t.Fatalf("CDN requests after warm hit = %d, want 1", got)
	}

	offline.Store(true)
	third := request()
	if third.Code != http.StatusOK || third.Body.String() != body {
		t.Fatalf("offline response = (%d, %q), want cached (200, %q)", third.Code, third.Body.String(), body)
	}
	if got := originRequests.Load(); got != 1 {
		t.Fatalf("offline cache hit contacted origin; requests = %d, want 1", got)
	}
	if got := availability.calls.Load(); got != 1 {
		t.Fatalf("cache hits selected an upstream; selector calls = %d, want 1", got)
	}
	if got := third.Header().Get("X-Linked-Size"); got != "20" {
		t.Fatalf("offline cached X-Linked-Size = %q, want 20", got)
	}

	var logs []db.AccessLog
	if err := database.Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 {
		t.Fatalf("access logs = %d, want 3", len(logs))
	}
	if logs[0].Hit || logs[0].Upstream != "mock-huggingface" {
		t.Fatalf("miss access log = %+v", logs[0])
	}
	for _, log := range logs[1:] {
		if !log.Hit || log.Upstream != "" || log.StatusCode != http.StatusOK {
			t.Fatalf("hit access log = %+v", log)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := manager.Close(ctx); err != nil {
		t.Fatalf("close cache manager: %v", err)
	}
}

func TestHandlerArtifactIdleTimeoutCancelsBodyAndMarksUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Repo-Commit", commit)
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(origin.Close)

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	handler.artifactIdleTimeout = 20 * time.Millisecond
	selected, err := handler.selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	recorder := httptest.NewRecorder()
	start := time.Now()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/stalled.bin",
			nil,
		),
	)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("stalled Hugging Face body took %v to cancel", elapsed)
	}
	if got := selected.SuccessRate(); got != 0 {
		t.Fatalf("upstream success rate after artifact idle timeout = %v, want 0", got)
	}
	if selected.IsHealthy() {
		t.Fatal("artifact idle timeout was not recorded as an upstream failure")
	}
}

func TestHandlerArtifactErrorBodyIdleTimeoutBoundsStatusBuffering(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	releaseOrigin := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseOrigin) })
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-releaseOrigin:
		}
	}))
	t.Cleanup(func() {
		release()
		origin.Close()
	})

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	handler.artifactIdleTimeout = 20 * time.Millisecond
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	recorder := httptest.NewRecorder()
	start := time.Now()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/"+commit+"/forbidden.bin",
			nil,
		),
	)
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		release()
		t.Fatalf("stalled Hugging Face error body took %v to bound", elapsed)
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestHandlerDirectArtifactIdleTimeoutCancelsBodyAndMarksUpstreamFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	releaseOrigin := make(chan struct{})
	originCanceled := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseOrigin) })
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer hf_private" {
			t.Errorf("origin Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Repo-Commit", commit)
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			close(originCanceled)
		case <-releaseOrigin:
		}
	}))
	t.Cleanup(func() {
		release()
		origin.Close()
	})

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	handler.artifactIdleTimeout = 20 * time.Millisecond
	selected, err := handler.selector.Select(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/huggingface/acme/model/resolve/"+commit+"/private.bin",
		nil,
	)
	request.Header.Set("Authorization", "Bearer hf_private")
	requestDone := make(chan struct{})
	go func() {
		router.ServeHTTP(recorder, request)
		close(requestDone)
	}()

	select {
	case <-requestDone:
	case <-time.After(500 * time.Millisecond):
		release()
		<-requestDone
		t.Fatal("direct Hugging Face artifact body did not honor the idle timeout")
	}
	select {
	case <-originCanceled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("direct artifact idle timeout did not cancel the upstream request")
	}
	if got := selected.SuccessRate(); got != 0 {
		t.Fatalf("upstream success rate after direct artifact idle timeout = %v, want 0", got)
	}
	if selected.IsHealthy() {
		t.Fatal("direct artifact idle timeout was not recorded as an upstream failure")
	}
}

func waitForCacheEntry(t *testing.T, database *gorm.DB, key string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		var count int64
		if err := database.Model(&db.CacheEntry{}).Where("key = ?", key).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cache entry %q was not committed", key)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

type availabilitySelector struct {
	delegate upstream.Selector
	offline  atomic.Bool
	calls    atomic.Int64
}

func (s *availabilitySelector) Select(ctx context.Context) (*upstream.Upstream, error) {
	s.calls.Add(1)
	if s.offline.Load() {
		return nil, fmt.Errorf("offline")
	}
	return s.delegate.Select(ctx)
}

func TestHandlerRangeBypassesCacheWithoutPoisoningFullObject(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const fullBody = "abcdefgh"
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes 0-3/8")
			w.Header().Set("Content-Length", "4")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "abcd")
			return
		}
		w.Header().Set("Content-Length", "8")
		_, _ = io.WriteString(w, fullBody)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/model.bin"

	partial := httptest.NewRecorder()
	partialReq := httptest.NewRequest(http.MethodGet, path, nil)
	partialReq.Header.Set("Range", "bytes=0-3")
	router.ServeHTTP(partial, partialReq)
	if partial.Code != http.StatusPartialContent || partial.Body.String() != "abcd" {
		t.Fatalf("range response = (%d, %q), want (206, %q)", partial.Code, partial.Body.String(), "abcd")
	}
	if got := partial.Header().Get("Content-Range"); got != "bytes 0-3/8" {
		t.Fatalf("Content-Range = %q, want bytes 0-3/8", got)
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("range request created %d cache rows, want 0", rows)
	}

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != fullBody {
			t.Fatalf("full response %d = (%d, %q), want (200, %q)", i+1, recorder.Code, recorder.Body.String(), fullBody)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("origin requests = %d, want 2 (range plus one full miss)", got)
	}
}

func TestHandlerRejectsExcessiveRangesBeforeSelectingOrContactingUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Range", "bytes */8")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Length", "8")
		_, _ = io.WriteString(w, "abcdefgh")
	}))
	t.Cleanup(origin.Close)

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/" +
		"0123456789abcdef0123456789abcdef01234567/model.bin"

	specifications := make([]string, maxRequestedByteRanges+1)
	for index := range specifications {
		specifications[index] = strconv.Itoa(index*2) + "-" + strconv.Itoa(index*2)
	}
	excessive := httptest.NewRecorder()
	excessiveRequest := httptest.NewRequest(http.MethodGet, path, nil)
	excessiveRequest.Header.Set("Range", "bytes="+strings.Join(specifications, ","))
	router.ServeHTTP(excessive, excessiveRequest)
	if excessive.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("excessive Range status = %d, want 416", excessive.Code)
	}
	if got := excessive.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", got)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("excessive Range contacted upstream %d times", got)
	}

	for _, acceptEncoding := range []string{
		"gzip, identity;q=0",
		"gzip, *;q=0",
	} {
		notAcceptable := httptest.NewRecorder()
		notAcceptableRequest := httptest.NewRequest(http.MethodGet, path, nil)
		notAcceptableRequest.Header.Set("Range", "bytes=0-1")
		notAcceptableRequest.Header.Set("Accept-Encoding", acceptEncoding)
		router.ServeHTTP(notAcceptable, notAcceptableRequest)
		if notAcceptable.Code != http.StatusNotAcceptable {
			t.Fatalf(
				"Accept-Encoding %q status = %d, want 406",
				acceptEncoding,
				notAcceptable.Code,
			)
		}
		if got := notAcceptable.Header().Get("Vary"); got != "Accept-Encoding" {
			t.Fatalf("406 Vary = %q, want Accept-Encoding", got)
		}
		if got := requests.Load(); got != 0 {
			t.Fatalf(
				"unacceptable identity encoding contacted upstream %d times",
				got,
			)
		}
	}

	atLimit := httptest.NewRecorder()
	atLimitRequest := httptest.NewRequest(http.MethodGet, path, nil)
	atLimitRequest.Header.Set(
		"Range",
		"bytes="+strings.Join(specifications[:maxRequestedByteRanges], ","),
	)
	router.ServeHTTP(atLimit, atLimitRequest)
	if atLimit.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("Range at limit status = %d, want upstream 416", atLimit.Code)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("Range at limit contacted upstream %d times, want 1", got)
	}

	full := httptest.NewRecorder()
	router.ServeHTTP(full, httptest.NewRequest(http.MethodGet, path, nil))
	if full.Code != http.StatusOK || full.Body.String() != "abcdefgh" {
		t.Fatalf(
			"request after local rejection = (%d, %q), want (200, abcdefgh)",
			full.Code,
			full.Body.String(),
		)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("total upstream requests = %d, want 2", got)
	}
}

func TestHandlerIgnoresRangeOnHEAD(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requests atomic.Int64
	var upstreamRange, upstreamIfRange string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		upstreamRange = r.Header.Get("Range")
		upstreamIfRange = r.Header.Get("If-Range")
		w.Header().Set("Content-Length", "8")
	}))
	t.Cleanup(origin.Close)

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/" +
		"0123456789abcdef0123456789abcdef01234567/model.bin"

	specifications := make([]string, maxRequestedByteRanges+1)
	for index := range specifications {
		specifications[index] = strconv.Itoa(index*2) + "-" + strconv.Itoa(index*2)
	}
	request := httptest.NewRequest(http.MethodHead, path, nil)
	request.Header.Set("Range", "bytes="+strings.Join(specifications, ","))
	request.Header.Set("If-Range", `"blob"`)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", recorder.Code)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HEAD contacted upstream %d times, want 1", got)
	}
	if upstreamRange != "" || upstreamIfRange != "" {
		t.Fatalf(
			"HEAD forwarded Range headers (%q, %q)",
			upstreamRange,
			upstreamIfRange,
		)
	}
}

func TestHandlerReselectsAfterMutableRefMetadataTransportFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	var badGETs atomic.Int64
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("test server does not support hijacking")
				return
			}
			connection, _, err := hijacker.Hijack()
			if err != nil {
				t.Errorf("hijack failed: %v", err)
				return
			}
			_ = connection.Close()
			return
		}
		badGETs.Add(1)
		_, _ = io.WriteString(w, "bad")
	}))
	t.Cleanup(bad.Close)

	var goodGETs atomic.Int64
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			goodGETs.Add(1)
		}
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("Content-Length", "4")
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, "good")
		}
	}))
	t.Cleanup(good.Close)

	handler, _, _ := newCachingTestHandler(t, bad.URL)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "bad", URL: bad.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "good", URL: good.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.selector = upstream.NewPrioritySelector(pool)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/main/model.bin?nonce=direct",
			nil,
		),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != "good" {
		t.Fatalf("fallback response = (%d, %q), want (200, good)", recorder.Code, recorder.Body.String())
	}
	if badGETs.Load() != 0 || goodGETs.Load() != 1 {
		t.Fatalf("GET requests = (bad:%d good:%d), want (0, 1)", badGETs.Load(), goodGETs.Load())
	}
}

func TestServeDirectSelectedReplacesCriticallyLatchedPreferredUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	var badRequests atomic.Int64
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		badRequests.Add(1)
		_, _ = io.WriteString(w, "bad")
	}))
	t.Cleanup(bad.Close)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "4")
		w.Header().Set("X-Repo-Commit", commit)
		_, _ = io.WriteString(w, "good")
	}))
	t.Cleanup(good.Close)

	handler, _, _ := newCachingTestHandler(t, bad.URL)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "bad", URL: bad.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "good", URL: good.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.selector = upstream.NewPrioritySelector(pool)
	var preferred *upstream.Upstream
	for _, candidate := range pool.Snapshot() {
		if candidate.Name == "bad" {
			preferred = candidate
			break
		}
	}
	if preferred == nil {
		t.Fatal("preferred upstream not found")
	}
	preferred.ReportCriticalFailure(time.Millisecond)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	target := "/acme/model/resolve/" + commit + "/model.bin"
	ginContext.Request = httptest.NewRequest(http.MethodGet, "/huggingface"+target, nil)
	selectedName := handler.serveDirectSelected(ginContext, preferred, target)

	if recorder.Code != http.StatusOK || recorder.Body.String() != "good" {
		t.Fatalf("response = (%d, %q), want (200, good)", recorder.Code, recorder.Body.String())
	}
	if selectedName != "good" {
		t.Fatalf("selected upstream = %q, want good", selectedName)
	}
	if got := badRequests.Load(); got != 0 {
		t.Fatalf("critically failed upstream received %d requests", got)
	}
}

func TestHandlerReselectsStalePinRangeAfterTransientMetadataStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "0123456789abcdef0123456789abcdef01234567"
		body   = "abcdefgh"
	)
	var transient atomic.Bool
	var badRangeGETs atomic.Int64
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && transient.Load() {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			return
		}
		if r.Header.Get("Range") != "" {
			badRangeGETs.Add(1)
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(bad.Close)

	var goodGETs atomic.Int64
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("alternate upstream method = %s, want GET", r.Method)
			return
		}
		goodGETs.Add(1)
		if got := r.Header.Get("Range"); got != "bytes=0-3" {
			t.Errorf("alternate upstream Range = %q, want bytes=0-3", got)
		}
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "abcd")
	}))
	t.Cleanup(good.Close)

	handler, _, database := newCachingTestHandler(t, bad.URL)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "bad", URL: bad.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "good", URL: good.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.selector = upstream.NewPrioritySelector(pool)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/main/model.bin"

	warm := httptest.NewRecorder()
	router.ServeHTTP(warm, httptest.NewRequest(http.MethodGet, path, nil))
	if warm.Code != http.StatusOK || warm.Body.String() != body {
		t.Fatalf("warm response = (%d, %q), want (200, %q)", warm.Code, warm.Body.String(), body)
	}
	waitForCacheEntry(
		t,
		database,
		"huggingface/acme/model/resolve/"+commit+"/model.bin",
	)
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	transient.Store(true)
	partial := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Range", "bytes=0-3")
	router.ServeHTTP(partial, request)

	if partial.Code != http.StatusPartialContent || partial.Body.String() != "abcd" {
		t.Fatalf(
			"transient fallback response = (%d, %q), want (206, abcd)",
			partial.Code,
			partial.Body.String(),
		)
	}
	if badRangeGETs.Load() != 0 || goodGETs.Load() != 1 {
		t.Fatalf(
			"range GET requests = (bad:%d good:%d), want (0, 1)",
			badRangeGETs.Load(),
			goodGETs.Load(),
		)
	}
}

func TestHandlerStalePinRangeFallbackDoesNotReuseIntegrityFailedUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "0123456789abcdef0123456789abcdef01234567"
		body   = "abcdefgh"
	)
	var corruptMetadata atomic.Bool
	var badGETs atomic.Int64
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			if corruptMetadata.Load() {
				w.Header().Set("Content-Length", strconv.Itoa(len(body)-1))
			} else {
				w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			}
			return
		}
		badGETs.Add(1)
		if r.Header.Get("Range") != "" {
			w.Header().Set("Content-Length", "4")
			w.Header().Set("Content-Range", "bytes 4-7/8")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "efgh")
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(bad.Close)

	var goodGETs atomic.Int64
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		goodGETs.Add(1)
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("fallback Range = %q, want bytes=0-3", r.Header.Get("Range"))
		}
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		w.Header().Set("Content-Length", "4")
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "abcd")
	}))
	t.Cleanup(good.Close)

	handler, _, database := newCachingTestHandler(t, bad.URL)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "bad", URL: bad.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "good", URL: good.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.selector = upstream.NewPrioritySelector(pool)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/main/model.bin"

	warm := httptest.NewRecorder()
	router.ServeHTTP(warm, httptest.NewRequest(http.MethodGet, path, nil))
	if warm.Code != http.StatusOK || warm.Body.String() != body {
		t.Fatalf("warm response = (%d, %q)", warm.Code, warm.Body.String())
	}
	waitForCacheEntry(
		t,
		database,
		"huggingface/acme/model/resolve/"+commit+"/model.bin",
	)
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	corruptMetadata.Store(true)
	partial := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Range", "bytes=0-3")
	router.ServeHTTP(partial, request)

	if partial.Code != http.StatusPartialContent || partial.Body.String() != "abcd" {
		t.Fatalf("range fallback = (%d, %q), want (206, abcd)", partial.Code, partial.Body.String())
	}
	if badGETs.Load() != 1 || goodGETs.Load() != 1 {
		t.Fatalf("GET requests = (bad:%d good:%d), want warm bad once and fallback good once", badGETs.Load(), goodGETs.Load())
	}
	for _, candidate := range pool.Snapshot() {
		if candidate.Name == "bad" && candidate.IsHealthy() {
			t.Fatal("integrity-failed upstream remained healthy")
		}
	}
}

func TestHandlerSeparatesHFAuthorizationFromProjectToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requests atomic.Int64
	var leakedProjectToken atomic.Bool
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		auth := r.Header.Get("Authorization")
		if auth == "Bearer depsilo_proj_not-an-upstream-secret" {
			leakedProjectToken.Store(true)
		}
		w.Header().Set("Content-Type", "text/plain")
		if auth != "" {
			_, _ = io.WriteString(w, "private:"+auth)
			return
		}
		_, _ = io.WriteString(w, "public")
	}))
	t.Cleanup(origin.Close)

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/0123456789abcdef0123456789abcdef01234567/config.txt"

	project := httptest.NewRecorder()
	projectReq := httptest.NewRequest(http.MethodGet, path, nil)
	projectReq.Header.Set("Authorization", "Bearer depsilo_proj_not-an-upstream-secret")
	router.ServeHTTP(project, projectReq)
	if project.Code != http.StatusOK || project.Body.String() != "public" {
		t.Fatalf("project-token response = (%d, %q), want sanitized public response", project.Code, project.Body.String())
	}
	if leakedProjectToken.Load() {
		t.Fatal("Depsilo project token leaked to Hugging Face upstream")
	}

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer hf_private")
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK || recorder.Body.String() != "private:Bearer hf_private" {
			t.Fatalf("HF-auth response %d = (%d, %q)", i+1, recorder.Code, recorder.Body.String())
		}
	}

	anonymous := httptest.NewRecorder()
	router.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, path, nil))
	if anonymous.Code != http.StatusOK || anonymous.Body.String() != "public" {
		t.Fatalf("anonymous response = (%d, %q), want cached public response", anonymous.Code, anonymous.Body.String())
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("origin requests = %d, want 3 (one public plus two authenticated bypasses)", got)
	}
}

func TestHandlerForwardsAndSeparatesCanonicalQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cursor":"`+r.URL.Query().Get("cursor")+`"}`)
	}))
	t.Cleanup(origin.Close)

	handler, _, _ := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	request := func(rawQuery string) string {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(
			http.MethodGet,
			"/huggingface/api/models/acme/model/tree/main?"+rawQuery,
			nil,
		))
		if recorder.Code != http.StatusOK {
			t.Fatalf("query %q status = %d, body = %q", rawQuery, recorder.Code, recorder.Body.String())
		}
		return recorder.Body.String()
	}

	if got := request("cursor=one&limit=10"); got != `{"cursor":"one"}` {
		t.Fatalf("first query body = %q", got)
	}
	if got := request("limit=10&cursor=one"); got != `{"cursor":"one"}` {
		t.Fatalf("canonical-equivalent query body = %q", got)
	}
	if got := request("cursor=two&limit=10"); got != `{"cursor":"two"}` {
		t.Fatalf("second query body = %q", got)
	}
	if got := request("cursor=two&limit=10"); got != `{"cursor":"two"}` {
		t.Fatalf("cached second query body = %q", got)
	}
	for i := 0; i < 2; i++ {
		if got := request("nonce=ignored"); got != `{"cursor":""}` {
			t.Fatalf("unknown-query response = %q", got)
		}
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("origin requests = %d, want 2 cached queries plus 2 unknown-query bypasses", got)
	}
}

func TestHandlerCanonicalizesBoundedMetadataQueriesAndBypassesExpand(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, r.URL.RawQuery)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/api/models/acme/model"

	request := func(rawQuery string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, path+"?"+rawQuery, nil),
		)
		if recorder.Code != http.StatusOK {
			t.Fatalf("query %q status = %d", rawQuery, recorder.Code)
		}
		return recorder
	}

	first := request("blobs=True")
	if first.Body.String() != "blobs=true" {
		t.Fatalf("canonical upstream query = %q, want blobs=true", first.Body.String())
	}
	parsed := ParseRequestPath("/api/models/acme/model")
	key, err := CacheKeyForRawQuery(parsed, "blobs=true")
	if err != nil {
		t.Fatal(err)
	}
	waitForCacheEntry(t, database, key)
	if got := request("blobs=true").Body.String(); got != "blobs=true" {
		t.Fatalf("cached canonical query = %q", got)
	}

	for i := 0; i < 2; i++ {
		if got := request("expand=likes").Body.String(); got != "expand=likes" {
			t.Fatalf("expand bypass response = %q", got)
		}
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("origin requests = %d, want one bounded miss plus two expand bypasses", got)
	}
}

func TestHandlerDoesNotCacheUnboundedArtifactQueryVariants(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Repo-Commit", commit)
		_, _ = io.WriteString(w, r.URL.RawQuery)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	basePath := "/huggingface/acme/model/resolve/" + commit + "/model.bin"

	request := func(rawQuery string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(
			recorder,
			httptest.NewRequest(http.MethodGet, basePath+"?"+rawQuery, nil),
		)
		if recorder.Code != http.StatusOK {
			t.Fatalf("query %q status = %d, body = %q", rawQuery, recorder.Code, recorder.Body.String())
		}
		return recorder
	}

	for i := 0; i < 2; i++ {
		if got := request("nonce=1").Body.String(); got != "nonce=1" {
			t.Fatalf("unknown-query response = %q", got)
		}
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("unknown artifact query created %d cache rows, want 0", rows)
	}

	if got := request("download=true").Body.String(); got != "download=true" {
		t.Fatalf("download response = %q", got)
	}
	parsed := ParseRequestPath("/acme/model/resolve/" + commit + "/model.bin")
	downloadKey, err := CacheKeyForRawQuery(parsed, "download=true")
	if err != nil {
		t.Fatal(err)
	}
	waitForCacheEntry(t, database, downloadKey)
	if got := request("download=true").Body.String(); got != "download=true" {
		t.Fatalf("cached download response = %q", got)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("origin requests = %d, want two bypasses plus one cached download miss", got)
	}
}

func TestHandlerDoesNotPersistNoStoreResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Age", "29")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Repo-Commit", commit)
		_, _ = io.WriteString(w, "private representation")
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/" + commit + "/private.bin"

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != "private representation" {
			t.Fatalf("response %d = (%d, %q)", i+1, recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
			t.Fatalf("response %d Cache-Control = %q, want no-store", i+1, got)
		}
		if got := recorder.Header().Get("Age"); got != "29" {
			t.Fatalf("response %d Age = %q, want 29", i+1, got)
		}
	}

	var rows int64
	if err := database.Model(&db.CacheEntry{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("no-store response created %d cache rows, want 0", rows)
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("origin requests = %d, want policy probe plus direct stream per request", got)
	}
}

func TestHandlerCollapsesVaryUserAgentForSharedCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit       = "0123456789abcdef0123456789abcdef01234567"
		cacheControl = "public, max-age=259200"
	)
	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Cache-Control", cacheControl)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Vary", "User-Agent")
		w.Header().Set("X-Repo-Commit", commit)
		_, _ = io.WriteString(w, r.Header.Get("User-Agent"))
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/" + commit + "/config.txt"
	key := "huggingface/acme/model/resolve/" + commit + "/config.txt"

	request := func(userAgent string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("User-Agent", userAgent)
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("User-Agent %q status = %d", userAgent, recorder.Code)
		}
		return recorder
	}

	first := request("downstream-one/1.0")
	if first.Body.String() != "depsilo/0.1" {
		t.Fatalf("first representation = %q, want normalized User-Agent", first.Body.String())
	}
	waitForCacheEntry(t, database, key)
	second := request("downstream-two/2.0")
	if second.Body.String() != "depsilo/0.1" {
		t.Fatalf("cached representation = %q, want normalized User-Agent", second.Body.String())
	}
	for index, response := range []*httptest.ResponseRecorder{first, second} {
		if got := response.Header().Get("Vary"); got != "" {
			t.Fatalf("response %d retained collapsed Vary dimension %q", index+1, got)
		}
		if got := response.Header().Get("Cache-Control"); got != "" {
			t.Fatalf("response %d replayed upstream Cache-Control %q", index+1, got)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("origin requests = %d, want one shared-cache miss", got)
	}
}

func TestHandlerRejectsCanonicalCommitMismatchOnEveryResponsePath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit      = "0123456789abcdef0123456789abcdef01234567"
		otherCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	tests := []struct {
		name          string
		rawQuery      string
		authorization string
		rangeHeader   string
		cacheControl  string
	}{
		{name: "cache policy bypass", cacheControl: "no-store"},
		{name: "unknown artifact query", rawQuery: "nonce=1"},
		{name: "authenticated request", authorization: "Bearer hf_private"},
		{name: "range request", rangeHeader: "bytes=0-3"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.cacheControl != "" {
					w.Header().Set("Cache-Control", test.cacheControl)
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("X-Repo-Commit", otherCommit)
				_, _ = io.WriteString(w, "wrong revision")
			}))
			t.Cleanup(origin.Close)

			handler, _, _ := newCachingTestHandler(t, origin.URL)
			selected, err := handler.selector.Select(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			handler.Register(router.Group("/huggingface"))
			target := "/huggingface/acme/model/resolve/" + commit + "/model.bin"
			if test.rawQuery != "" {
				target += "?" + test.rawQuery
			}
			request := httptest.NewRequest(http.MethodGet, target, nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			if test.rangeHeader != "" {
				request.Header.Set("Range", test.rangeHeader)
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502; body = %q", recorder.Code, recorder.Body.String())
			}
			if selected.IsHealthy() || selected.SuccessRate() != 0 {
				t.Fatalf(
					"mismatching upstream health = (healthy=%v rate=%v), want false/0",
					selected.IsHealthy(),
					selected.SuccessRate(),
				)
			}
		})
	}
}

func TestHandlerCanonicalCommitMismatchAllowsHealthyFallbackOnNextRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit      = "0123456789abcdef0123456789abcdef01234567"
		otherCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	var badRequests atomic.Int64
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		badRequests.Add(1)
		w.Header().Set("X-Repo-Commit", otherCommit)
		_, _ = io.WriteString(w, "wrong revision")
	}))
	t.Cleanup(bad.Close)
	var goodRequests atomic.Int64
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodRequests.Add(1)
		w.Header().Set("X-Repo-Commit", commit)
		_, _ = io.WriteString(w, "correct revision")
	}))
	t.Cleanup(good.Close)

	handler, _, _ := newCachingTestHandler(t, bad.URL)
	pool, err := upstream.NewPool([]config.UpstreamConfig{
		{Name: "bad", URL: bad.URL, Priority: 1, ProbeMode: "passive"},
		{Name: "good", URL: good.URL, Priority: 2, ProbeMode: "passive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	handler.selector = upstream.NewPrioritySelector(pool)
	for _, candidate := range pool.Snapshot() {
		if candidate.Name != "bad" {
			continue
		}
		for i := 0; i < 1000; i++ {
			candidate.Report(time.Millisecond, true)
		}
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/" + commit + "/model.bin?nonce=uncached"

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	if first.Code != http.StatusBadGateway {
		t.Fatalf("first status = %d, want 502", first.Code)
	}
	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, path, nil))
	if second.Code != http.StatusOK || second.Body.String() != "correct revision" {
		t.Fatalf("fallback response = (%d, %q), want (200, correct revision)", second.Code, second.Body.String())
	}
	if badRequests.Load() != 1 || goodRequests.Load() != 1 {
		t.Fatalf("origin requests = (bad=%d good=%d), want 1/1", badRequests.Load(), goodRequests.Load())
	}
}

func TestHandlerPreservesEncodedRevisionAndFilenameOnPublicAndProjectRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	var escapedPaths []string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		escapedPaths = append(escapedPaths, r.URL.EscapedPath())
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	if err := database.Create(&db.Project{
		Name: "Encoded paths",
		Slug: "encoded",
	}).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	project := router.Group("/p/:slug")
	project.Use(middleware.ProjectSlugMiddleware(database))
	handler.Register(project.Group("/huggingface"))

	publicPath := "/huggingface/acme/model/resolve/refs%2Fpr%2F1/a%3Fb"
	public := httptest.NewRecorder()
	router.ServeHTTP(public, httptest.NewRequest(http.MethodGet, publicPath, nil))
	if public.Code != http.StatusOK || public.Body.String() != "ok" {
		t.Fatalf("public response = (%d, %q)", public.Code, public.Body.String())
	}
	waitForCacheEntry(t, database, "huggingface/acme/model/resolve/"+commit+"/a%3Fb")

	projectPath := "/p/encoded/huggingface/acme/model/resolve/refs%2Fpr%2F2/a%23b"
	projectResponse := httptest.NewRecorder()
	projectRequest := httptest.NewRequest(http.MethodGet, projectPath, nil)
	// A real HF token deliberately bypasses the public representation cache,
	// while /p/:slug supplies project attribution without occupying this header.
	projectRequest.Header.Set("Authorization", "Bearer hf_private")
	router.ServeHTTP(projectResponse, projectRequest)
	if projectResponse.Code != http.StatusOK || projectResponse.Body.String() != "ok" {
		t.Fatalf("project response = (%d, %q)", projectResponse.Code, projectResponse.Body.String())
	}

	want := []string{
		"/acme/model/resolve/refs%2Fpr%2F1/a%3Fb",
		"/acme/model/resolve/" + commit + "/a%3Fb",
		"/acme/model/resolve/refs%2Fpr%2F2/a%23b",
	}
	if len(escapedPaths) != len(want) {
		t.Fatalf("origin paths = %q, want %q", escapedPaths, want)
	}
	for i := range want {
		if escapedPaths[i] != want[i] {
			t.Fatalf("origin path %d = %q, want %q", i, escapedPaths[i], want[i])
		}
	}
}

func TestHandlerCachesPaginationLinkAndKeepsProjectScope(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requests atomic.Int64
	var origin *httptest.Server
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set(
			"Link",
			"<"+origin.URL+"/api/models/acme/model/tree/main/folder?cursor=next>; rel=\"next\"",
		)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	if err := database.Create(&db.Project{Name: "Link scope", Slug: "link-scope"}).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	project := router.Group("/p/:slug")
	project.Use(middleware.ProjectSlugMiddleware(database))
	handler.Register(project.Group("/huggingface"))

	assertLink := func(path, want string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.String() != "[]" {
			t.Fatalf("%s response = (%d, %q)", path, recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("Link"); got != want {
			t.Fatalf("%s Link = %q, want %q", path, got, want)
		}
	}

	publicPath := "/huggingface/api/models/acme/model/tree/main/folder"
	publicLink := `</huggingface/api/models/acme/model/tree/main/folder?cursor=next>; rel="next"`
	assertLink(publicPath, publicLink)
	waitForCacheEntry(t, database, "huggingface/api/models/acme/model/tree/main/folder")
	assertLink(publicPath, publicLink)
	assertLink(
		"/p/link-scope"+publicPath,
		`</p/link-scope/huggingface/api/models/acme/model/tree/main/folder?cursor=next>; rel="next"`,
	)
	if got := requests.Load(); got != 1 {
		t.Fatalf("origin requests = %d, want one miss plus two cache hits", got)
	}
}

func TestHandlerHEADSkipsArtifactAndCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var originRequests atomic.Int64
	var artifactRequests atomic.Int64
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		artifactRequests.Add(1)
		_, _ = io.WriteString(w, "abcdefgh")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests.Add(1)
		if r.Method != http.MethodHead {
			t.Errorf("origin method = %s, want HEAD", r.Method)
		}
		w.Header().Set("X-Linked-Etag", "sha256:model")
		w.Header().Set("X-Linked-Size", "8")
		w.Header().Set("Location", artifact.URL+"/signed/model")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/acme/model/resolve/main/model.bin"

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 {
			t.Fatalf("HEAD %d = (%d, %q), want (200, empty)", i+1, recorder.Code, recorder.Body.String())
		}
		if got := recorder.Header().Get("X-Linked-Etag"); got != "sha256:model" {
			t.Fatalf("HEAD X-Linked-Etag = %q", got)
		}
	}
	if got := originRequests.Load(); got != 2 {
		t.Fatalf("origin HEAD requests = %d, want 2", got)
	}
	if got := artifactRequests.Load(); got != 0 {
		t.Fatalf("HEAD contacted signed artifact %d times", got)
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("HEAD created %d cache rows, want 0", rows)
	}
}

func TestHandlerPinsMutableRefAcrossHEADGETAndServesBothOffline(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		bodyA   = "weights-from-commit-a"
		bodyB   = "weights-from-commit-b"
	)
	var current atomic.Int64
	var offline atomic.Bool
	var requestsMu sync.Mutex
	var requests []string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		requestsMu.Unlock()
		if offline.Load() {
			http.Error(w, "offline", http.StatusServiceUnavailable)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		ref := parts[4]
		commit := ref
		if ref == "main" {
			commit = commitA
			if current.Load() == 1 {
				commit = commitB
			}
		}
		body := bodyA
		if commit == commitB {
			body = bodyB
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Linked-Etag", "sha256:"+commit)
		w.Header().Set("X-Linked-Size", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	const path = "/huggingface/acme/model/resolve/main/model.bin"
	const canonicalA = "huggingface/acme/model/resolve/" + commitA + "/model.bin"

	request := func(method string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder
	}

	headA := request(http.MethodHead)
	if headA.Code != http.StatusOK || headA.Header().Get("X-Repo-Commit") != commitA {
		t.Fatalf("initial HEAD = (%d, headers=%v)", headA.Code, headA.Header())
	}
	headAAgain := request(http.MethodHead)
	if headAAgain.Code != http.StatusOK || headAAgain.Header().Get("X-Repo-Commit") != commitA {
		t.Fatalf("fresh-pin HEAD = (%d, headers=%v)", headAAgain.Code, headAAgain.Header())
	}
	// The branch moves after metadata discovery. GET must remain pinned to A.
	current.Store(1)
	getA := request(http.MethodGet)
	if getA.Code != http.StatusOK || getA.Body.String() != bodyA ||
		getA.Header().Get("X-Repo-Commit") != commitA {
		t.Fatalf("pinned GET A = (%d, %q, headers=%v)", getA.Code, getA.Body.String(), getA.Header())
	}
	waitForCacheEntry(t, database, canonicalA)

	requestsMu.Lock()
	beforeOffline := len(requests)
	requestsMu.Unlock()
	offline.Store(true)
	offlineHead := request(http.MethodHead)
	offlineGet := request(http.MethodGet)
	if offlineHead.Code != http.StatusOK || offlineHead.Header().Get("X-Repo-Commit") != commitA {
		t.Fatalf("offline HEAD = (%d, headers=%v)", offlineHead.Code, offlineHead.Header())
	}
	if offlineGet.Code != http.StatusOK || offlineGet.Body.String() != bodyA {
		t.Fatalf("offline GET = (%d, %q)", offlineGet.Code, offlineGet.Body.String())
	}
	requestsMu.Lock()
	afterOffline := len(requests)
	requestsMu.Unlock()
	if afterOffline != beforeOffline {
		t.Fatalf("offline HEAD/GET contacted origin: before=%d after=%d", beforeOffline, afterOffline)
	}

	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	requestsMu.Lock()
	beforeStaleRetry := len(requests)
	requestsMu.Unlock()
	staleHead := request(http.MethodHead)
	staleGet := request(http.MethodGet)
	if staleHead.Code != http.StatusOK || staleHead.Header().Get("X-Repo-Commit") != commitA {
		t.Fatalf("stale fallback HEAD = (%d, headers=%v)", staleHead.Code, staleHead.Header())
	}
	if staleGet.Code != http.StatusOK || staleGet.Body.String() != bodyA {
		t.Fatalf("stale fallback GET = (%d, %q)", staleGet.Code, staleGet.Body.String())
	}
	requestsMu.Lock()
	afterStaleRetry := len(requests)
	requestsMu.Unlock()
	if afterStaleRetry != beforeStaleRetry+1 {
		t.Fatalf(
			"stale retry backoff contacted origin %d times, want exactly one",
			afterStaleRetry-beforeStaleRetry,
		)
	}

	offline.Store(false)
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	headB := request(http.MethodHead)
	if headB.Code != http.StatusOK || headB.Header().Get("X-Repo-Commit") != commitB {
		t.Fatalf("renewed HEAD = (%d, headers=%v)", headB.Code, headB.Header())
	}
	getB := request(http.MethodGet)
	if getB.Code != http.StatusOK || getB.Body.String() != bodyB ||
		getB.Header().Get("X-Repo-Commit") != commitB {
		t.Fatalf("renewed GET B = (%d, %q, headers=%v)", getB.Code, getB.Body.String(), getB.Header())
	}

	requestsMu.Lock()
	gotRequests := append([]string(nil), requests...)
	requestsMu.Unlock()
	wantGETA := "GET /acme/model/resolve/" + commitA + "/model.bin"
	wantGETB := "GET /acme/model/resolve/" + commitB + "/model.bin"
	if !containsString(gotRequests, wantGETA) || !containsString(gotRequests, wantGETB) {
		t.Fatalf("canonical GET paths missing: requests=%q", gotRequests)
	}
	for _, got := range gotRequests {
		if got == "GET /acme/model/resolve/main/model.bin" {
			t.Fatalf("moving ref body was fetched without canonical pin: %q", gotRequests)
		}
	}
}

func TestHandlerAuthoritativeRefFailureInvalidatesCanonicalCacheWhenPinDeleteFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		body   = "previously-public-weights"
	)
	var mode atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			http.Error(w, "repository became private", http.StatusForbidden)
			return
		case 2:
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Repo-Commit", commit)
		if r.Method != http.MethodHead {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	const mutablePath = "/huggingface/acme/model/resolve/main/model.bin"
	const canonicalKey = "huggingface/acme/model/resolve/" + commit + "/model.bin"

	warm := httptest.NewRecorder()
	router.ServeHTTP(warm, httptest.NewRequest(http.MethodGet, mutablePath, nil))
	if warm.Code != http.StatusOK || warm.Body.String() != body {
		t.Fatalf("warm response = (%d, %q)", warm.Code, warm.Body.String())
	}
	waitForCacheEntry(t, database, canonicalKey)
	if err := database.Model(&db.HuggingFaceRefPin{}).
		Where("key = ?", "huggingface/acme/model/ref/main").
		Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`
		CREATE TRIGGER fail_hugging_face_ref_pin_delete
		BEFORE DELETE ON hugging_face_ref_pins
		BEGIN
			SELECT RAISE(FAIL, 'ref pin delete unavailable');
		END;
	`).Error; err != nil {
		t.Fatal(err)
	}

	mode.Store(1)
	revoked := httptest.NewRecorder()
	router.ServeHTTP(revoked, httptest.NewRequest(http.MethodGet, mutablePath, nil))
	if revoked.Code != http.StatusForbidden {
		t.Fatalf("revoked response = (%d, %q)", revoked.Code, revoked.Body.String())
	}
	var pins, cached int64
	if err := database.Model(&db.HuggingFaceRefPin{}).Count(&pins).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&db.CacheEntry{}).Where("key = ?", canonicalKey).Count(&cached).Error; err != nil {
		t.Fatal(err)
	}
	if pins != 1 || cached != 0 {
		t.Fatalf("authoritative cleanup = pins:%d cached:%d, want failed pin delete but no canonical cache", pins, cached)
	}

	mode.Store(2)
	for _, path := range []string{
		mutablePath,
		"/huggingface/acme/model/resolve/" + commit + "/model.bin",
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code == http.StatusOK || recorder.Body.String() == body {
			t.Fatalf("revoked cache revived for %s: (%d, %q)", path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestHandlerPinsRangeRequestWithoutLeakingRangeIntoMetadataHEAD(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	var metadataRange string
	var artifactRange string
	var artifactIfRange string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			metadataRange = r.Header.Get("Range")
			if got := r.Header.Get("If-Range"); got != "" {
				t.Errorf("metadata HEAD If-Range = %q, want empty", got)
			}
			w.Header().Set("X-Repo-Commit", commit)
			w.Header().Set("X-Linked-Etag", `"blob"`)
			w.Header().Set("X-Linked-Size", "8")
			return
		}
		if !strings.Contains(r.URL.Path, "/resolve/"+commit+"/") {
			t.Errorf("artifact path = %q, want canonical commit", r.URL.EscapedPath())
		}
		artifactRange = r.Header.Get("Range")
		artifactIfRange = r.Header.Get("If-Range")
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.Header().Set("Content-Length", "4")
		w.Header().Set("X-Repo-Commit", commit)
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "abcd")
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/huggingface/acme/model/resolve/main/model.bin",
		nil,
	)
	request.Header.Set("Range", "bytes=0-3")
	request.Header.Set("If-Range", `"blob"`)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "abcd" {
		t.Fatalf("range response = (%d, %q)", recorder.Code, recorder.Body.String())
	}
	if metadataRange != "" {
		t.Fatalf("metadata HEAD inherited Range %q", metadataRange)
	}
	if artifactRange != "bytes=0-3" || artifactIfRange != `"blob"` {
		t.Fatalf("canonical GET range headers = (%q, %q)", artifactRange, artifactIfRange)
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("range request created %d cache rows", rows)
	}
}

func TestHandlerAuthenticatedMutableHEADRedirectsLocallyToCanonicalCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const commit = "0123456789abcdef0123456789abcdef01234567"
	var originPaths []string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer hf_private" {
			t.Errorf("origin Authorization = %q", got)
		}
		originPaths = append(originPaths, r.URL.EscapedPath())
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("X-Linked-Etag", `"private-blob"`)
		w.Header().Set("X-Linked-Size", "8")
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	if err := database.Create(&db.Project{Name: "Private HF", Slug: "private-hf"}).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	project := router.Group("/p/:slug")
	project.Use(middleware.ProjectSlugMiddleware(database))
	handler.Register(project.Group("/huggingface"))

	requestHEAD := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodHead, path, nil)
		request.Header.Set("Authorization", "Bearer hf_private")
		router.ServeHTTP(recorder, request)
		return recorder
	}

	public := requestHEAD("/huggingface/acme/model/resolve/main/model.bin")
	publicLocation := "/huggingface/acme/model/resolve/" + commit + "/model.bin"
	if public.Code != http.StatusTemporaryRedirect || public.Header().Get("Location") != publicLocation {
		t.Fatalf("public authenticated HEAD = (%d, Location=%q)", public.Code, public.Header().Get("Location"))
	}
	canonical := requestHEAD(publicLocation)
	if canonical.Code != http.StatusOK || canonical.Header().Get("X-Repo-Commit") != commit {
		t.Fatalf("canonical authenticated HEAD = (%d, headers=%v)", canonical.Code, canonical.Header())
	}

	projectResponse := requestHEAD("/p/private-hf/huggingface/acme/model/resolve/main/model.bin")
	wantProjectLocation := "/p/private-hf" + publicLocation
	if projectResponse.Code != http.StatusTemporaryRedirect ||
		projectResponse.Header().Get("Location") != wantProjectLocation {
		t.Fatalf("project authenticated HEAD = (%d, Location=%q)", projectResponse.Code, projectResponse.Header().Get("Location"))
	}
	var pins int64
	if err := database.Model(&db.HuggingFaceRefPin{}).Count(&pins).Error; err != nil {
		t.Fatal(err)
	}
	var cacheRows int64
	if err := database.Model(&db.CacheEntry{}).Count(&cacheRows).Error; err != nil {
		t.Fatal(err)
	}
	if pins != 0 || cacheRows != 0 {
		t.Fatalf("authenticated HEAD persisted public state: pins=%d cacheRows=%d", pins, cacheRows)
	}
	if !containsString(originPaths, "/acme/model/resolve/main/model.bin") ||
		!containsString(originPaths, "/acme/model/resolve/"+commit+"/model.bin") {
		t.Fatalf("origin paths = %q", originPaths)
	}
}

func TestHandlerKeepsMutableDownloadAvailableWhenRefPinReadFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "0123456789abcdef0123456789abcdef01234567"
		body   = "canonical model bytes"
	)
	var requestsMu sync.Mutex
	var requests []string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		requestsMu.Unlock()
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	if err := database.Migrator().DropTable(&db.HuggingFaceRefPin{}); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/main/model.bin",
			nil,
		),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != body {
		t.Fatalf("response = (%d, %q), want available canonical body", recorder.Code, recorder.Body.String())
	}

	requestsMu.Lock()
	gotRequests := append([]string(nil), requests...)
	requestsMu.Unlock()
	wantRequests := []string{
		"HEAD /acme/model/resolve/main/model.bin",
		"GET /acme/model/resolve/" + commit + "/model.bin",
	}
	if strings.Join(gotRequests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("upstream requests = %q, want %q", gotRequests, wantRequests)
	}
}

func TestHandlerKeepsMutableDownloadAvailableWhenRefPinWriteFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "89abcdef0123456789abcdef0123456789abcdef"
		body   = "canonical bytes after pin write failure"
	)
	var requestsMu sync.Mutex
	var requests []string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.EscapedPath())
		requestsMu.Unlock()
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	if err := database.Exec(`
		CREATE TRIGGER fail_hugging_face_ref_pin_insert
		BEFORE INSERT ON hugging_face_ref_pins
		BEGIN
			SELECT RAISE(FAIL, 'ref pin storage unavailable');
		END
	`).Error; err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	handler.Register(router.Group("/huggingface"))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(
			http.MethodGet,
			"/huggingface/acme/model/resolve/main/model.bin",
			nil,
		),
	)
	if recorder.Code != http.StatusOK || recorder.Body.String() != body {
		t.Fatalf("response = (%d, %q), want available canonical body", recorder.Code, recorder.Body.String())
	}

	requestsMu.Lock()
	gotRequests := append([]string(nil), requests...)
	requestsMu.Unlock()
	wantRequests := []string{
		"HEAD /acme/model/resolve/main/model.bin",
		"GET /acme/model/resolve/" + commit + "/model.bin",
	}
	if strings.Join(gotRequests, "\n") != strings.Join(wantRequests, "\n") {
		t.Fatalf("upstream requests = %q, want %q", gotRequests, wantRequests)
	}
}

func TestHandlerRefPinWriteFailureRedirectsHEADToCanonicalRootAndProjectTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const (
		commit = "fedcba9876543210fedcba9876543210fedcba98"
		body   = "ephemeral canonical bytes"
	)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Repo-Commit", commit)
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, body)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	if err := database.Create(&db.Project{Slug: "platform"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Exec(`
		CREATE TRIGGER fail_hugging_face_ref_pin_insert
		BEFORE INSERT ON hugging_face_ref_pins
		BEGIN
			SELECT RAISE(FAIL, 'ref pin storage unavailable');
		END
	`).Error; err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	project := router.Group("/p/:slug")
	project.Use(middleware.ProjectSlugMiddleware(database))
	handler.Register(project.Group("/huggingface"))

	tests := []struct {
		name         string
		requestPath  string
		wantLocation string
	}{
		{
			name:         "root route keeps query",
			requestPath:  "/huggingface/acme/model/resolve/main/model.bin?download=true",
			wantLocation: "/huggingface/acme/model/resolve/" + commit + "/model.bin?download=true",
		},
		{
			name:         "project route keeps scope and query",
			requestPath:  "/p/platform/huggingface/acme/model/resolve/main/model.bin?download=project",
			wantLocation: "/p/platform/huggingface/acme/model/resolve/" + commit + "/model.bin?download=project",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			redirect := httptest.NewRecorder()
			router.ServeHTTP(
				redirect,
				httptest.NewRequest(http.MethodHead, test.requestPath, nil),
			)
			if redirect.Code != http.StatusTemporaryRedirect ||
				redirect.Header().Get("Location") != test.wantLocation {
				t.Fatalf("HEAD redirect = (%d, %q), want (307, %q)",
					redirect.Code,
					redirect.Header().Get("Location"),
					test.wantLocation,
				)
			}

			canonicalHEAD := httptest.NewRecorder()
			router.ServeHTTP(
				canonicalHEAD,
				httptest.NewRequest(http.MethodHead, test.wantLocation, nil),
			)
			if canonicalHEAD.Code != http.StatusOK ||
				canonicalHEAD.Header().Get("X-Repo-Commit") != commit {
				t.Fatalf("canonical HEAD = (%d, headers=%v)", canonicalHEAD.Code, canonicalHEAD.Header())
			}

			canonicalGET := httptest.NewRecorder()
			router.ServeHTTP(
				canonicalGET,
				httptest.NewRequest(http.MethodGet, test.wantLocation, nil),
			)
			if canonicalGET.Code != http.StatusOK || canonicalGET.Body.String() != body {
				t.Fatalf("canonical GET = (%d, %q), want available body", canonicalGET.Code, canonicalGET.Body.String())
			}
		})
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestHandlerPassesOriginErrorWithoutCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var requests atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"Entry not found"}`)
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	path := "/huggingface/missing/model/resolve/main/config.json"

	for i := 0; i < 2; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), "Entry not found") {
			t.Fatalf("error response %d = (%d, %q)", i+1, recorder.Code, recorder.Body.String())
		}
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("origin error requests = %d, want 4 (HEAD preflight plus GET per attempt)", got)
	}
	var rows int64
	if err := database.Model(&db.CacheEntry{}).Count(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("404 response created %d cache rows, want 0", rows)
	}
	var logs []db.AccessLog
	if err := database.Order("id ASC").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	for _, log := range logs {
		if log.StatusCode != http.StatusNotFound || log.Hit {
			t.Fatalf("error access log = %+v", log)
		}
	}
}

func TestHandlerPreservesHuggingFaceProtocolErrorHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		status     int
		headers    http.Header
		wantHeader string
		wantValue  string
	}{
		{
			name:   "missing entry",
			status: http.StatusNotFound,
			headers: http.Header{
				"X-Error-Code":    {"EntryNotFound"},
				"X-Error-Message": {"missing config.json"},
				"X-Request-Id":    {"request-404"},
			},
			wantHeader: "X-Error-Code",
			wantValue:  "EntryNotFound",
		},
		{
			name:       "gated repository",
			status:     http.StatusForbidden,
			headers:    http.Header{"X-Error-Code": {"GatedRepo"}},
			wantHeader: "X-Error-Code",
			wantValue:  "GatedRepo",
		},
		{
			name:       "rate limited",
			status:     http.StatusTooManyRequests,
			headers:    http.Header{"Retry-After": {"30"}},
			wantHeader: "Retry-After",
			wantValue:  "30",
		},
		{
			name:       "invalid range",
			status:     http.StatusRequestedRangeNotSatisfiable,
			headers:    http.Header{"Content-Range": {"bytes */100"}},
			wantHeader: "Content-Range",
			wantValue:  "bytes */100",
		},
		{
			name:       "authentication required",
			status:     http.StatusUnauthorized,
			headers:    http.Header{"WWW-Authenticate": {`Bearer realm="hf"`}},
			wantHeader: "WWW-Authenticate",
			wantValue:  `Bearer realm="hf"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				for name, values := range test.headers {
					for _, value := range values {
						w.Header().Add(name, value)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, `{"error":"upstream"}`)
			}))
			t.Cleanup(origin.Close)

			handler, _, _ := newCachingTestHandler(t, origin.URL)
			router := gin.New()
			handler.Register(router.Group("/huggingface"))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(
				recorder,
				httptest.NewRequest(
					http.MethodGet,
					"/huggingface/acme/model/resolve/main/config.json",
					nil,
				),
			)

			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d", recorder.Code, test.status)
			}
			if got := recorder.Header().Get(test.wantHeader); got != test.wantValue {
				t.Fatalf("%s = %q, want %q", test.wantHeader, got, test.wantValue)
			}
		})
	}
}

func TestHandlerDoesNotHideAuthoritativeErrorsBehindStaleMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var mode atomic.Int64
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch mode.Load() {
		case 1:
			w.Header().Set("X-Error-Code", "RepositoryNotFound")
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"gone"}`)
		case 2:
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"modelId":"acme/model"}`)
		}
	}))
	t.Cleanup(origin.Close)

	handler, _, database := newCachingTestHandler(t, origin.URL)
	router := gin.New()
	handler.Register(router.Group("/huggingface"))
	const path = "/huggingface/api/models/acme/model"
	const key = "huggingface/api/models/acme/model"

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, path, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("warm response = (%d, %q)", first.Code, first.Body.String())
	}
	waitForCacheEntry(t, database, key)
	if err := database.Model(&db.CacheEntry{}).
		Where("key = ?", key).
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}

	mode.Store(1)
	missing := httptest.NewRecorder()
	router.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, path, nil))
	if missing.Code != http.StatusNotFound || missing.Header().Get("X-Error-Code") != "RepositoryNotFound" {
		t.Fatalf("authoritative response = (%d, %q, headers=%v)", missing.Code, missing.Body.String(), missing.Header())
	}

	mode.Store(2)
	transient := httptest.NewRecorder()
	router.ServeHTTP(transient, httptest.NewRequest(http.MethodGet, path, nil))
	if transient.Code != http.StatusServiceUnavailable {
		t.Fatalf("cache revived after authoritative removal = (%d, %q)", transient.Code, transient.Body.String())
	}
}

func TestHandlerTTLUsesBlobTTLForCommitPinnedAPIRoutes(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	handler := &Handler{cfg: config.CacheConfig{
		TTLIndex: 2 * time.Minute,
		TTLBlob:  48 * time.Hour,
	}}
	tests := []struct {
		path string
		want time.Duration
	}{
		{path: "/api/models/acme/model", want: 2 * time.Minute},
		{path: "/api/models/acme/model/tree/main", want: 2 * time.Minute},
		{path: "/api/models/acme/model/tree/" + commit, want: 48 * time.Hour},
		{path: "/api/datasets/acme/data/revision/" + commit, want: 48 * time.Hour},
		{path: "/acme/model/resolve/" + commit + "/model.bin", want: 48 * time.Hour},
	}
	for _, test := range tests {
		if got := handler.ttl(ParseRequestPath(test.path)); got != test.want {
			t.Errorf("ttl(%q) = %v, want %v", test.path, got, test.want)
		}
	}
}

func newCachingTestHandler(t *testing.T, originURL string) (*Handler, *cache.Manager, *gorm.DB) {
	t.Helper()

	database, err := db.Open("sqlite", filepath.Join(t.TempDir(), "huggingface.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	storage, err := cache.NewLocalStorage(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	manager := cache.NewManager(storage, database, cache.NewEventBus(), 72*time.Hour)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = manager.Close(ctx)
	})

	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "mock-huggingface",
		URL:       originURL,
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	handler := New(
		manager,
		upstream.NewPrioritySelector(pool),
		config.CacheConfig{TTLIndex: 5 * time.Minute, TTLBlob: 72 * time.Hour},
		database,
	)
	return handler, manager, database
}
