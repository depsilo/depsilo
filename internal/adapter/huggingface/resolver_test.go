package huggingface

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"depsilo/internal/cache"
	"depsilo/internal/config"
	"depsilo/internal/upstream"
)

type closeUnblocksRedirectBody struct {
	readStarted chan struct{}
	closed      chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
}

type progressOnCloseBody struct {
	readStarted chan struct{}
	closed      chan struct{}
	readOnce    sync.Once
	closeOnce   sync.Once
}

type fixedErrorBody struct {
	err error
}

func (b *fixedErrorBody) Read([]byte) (int, error) { return 0, b.err }
func (b *fixedErrorBody) Close() error             { return nil }

func newProgressOnCloseBody() *progressOnCloseBody {
	return &progressOnCloseBody{
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (b *progressOnCloseBody) Read(buffer []byte) (int, error) {
	b.readOnce.Do(func() { close(b.readStarted) })
	<-b.closed
	if len(buffer) == 0 {
		return 0, nil
	}
	buffer[0] = 'x'
	return 1, nil
}

func (b *progressOnCloseBody) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

func newCloseUnblocksRedirectBody() *closeUnblocksRedirectBody {
	return &closeUnblocksRedirectBody{
		readStarted: make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (b *closeUnblocksRedirectBody) Read([]byte) (int, error) {
	b.readOnce.Do(func() {
		close(b.readStarted)
	})
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *closeUnblocksRedirectBody) Close() error {
	b.closeOnce.Do(func() {
		close(b.closed)
	})
	return nil
}

func TestRedirectBodyIsClosedWithoutPotentiallyBlockingDrain(t *testing.T) {
	body := newCloseUnblocksRedirectBody()
	t.Cleanup(func() {
		_ = body.Close()
	})

	done := make(chan struct{})
	go func() {
		drainAndCloseRedirect(body)
		close(done)
	}()

	select {
	case <-body.readStarted:
		// Unblock the old implementation before failing so the test never leaves
		// a goroutine behind, even during the required red phase.
		_ = body.Close()
		<-done
		t.Fatal("redirect cleanup attempted to read an untrusted response body")
	case <-done:
	case <-time.After(time.Second):
		_ = body.Close()
		t.Fatal("redirect body cleanup did not complete")
	}

	select {
	case <-body.closed:
	default:
		t.Fatal("redirect response body was not closed")
	}
}

func TestResolverDirect200(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "revision=main" {
			t.Errorf("origin query = %q, want revision=main", r.URL.RawQuery)
		}
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"hidden":false}`)
	}))
	t.Cleanup(origin.Close)

	result := resolve(t, origin.URL, http.MethodGet, "/test/resolve/main/config.json?revision=main", nil, true)
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.StatusCode)
	}
	if got := result.Header.Get("X-Repo-Commit"); got != "abc1234" {
		t.Errorf("X-Repo-Commit = %q, want abc1234", got)
	}
	if !strings.Contains(string(body), `"hidden":false`) {
		t.Errorf("body did not contain expected JSON: %s", body)
	}
}

func TestResolverFollowsCrossOriginArtifactWithoutForwardingAuthorization(t *testing.T) {
	var originGotAuth bool
	var artifactGotAuth bool
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		artifactGotAuth = r.Header.Get("Authorization") != ""
		if r.URL.Query().Get("signature") != "secret" {
			t.Errorf("signed query was not retained: %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "11")
		w.Header().Set("Accept-Ranges", "bytes")
		_, _ = io.WriteString(w, "FAKE_WEIGHT")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originGotAuth = r.Header.Get("Authorization") == "Bearer hf_private"
		w.Header().Set("X-Linked-Etag", "deadbeef")
		w.Header().Set("X-Linked-Size", "11")
		w.Header().Set("X-Repo-Commit", "abc1234")
		w.Header().Set("Location", artifact.URL+"/signed/blob?signature=secret")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer hf_private")
	result := resolve(t, origin.URL, http.MethodGet, "/repo/resolve/main/weights.bin", headers, false)
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.StatusCode)
	}
	if string(body) != "FAKE_WEIGHT" {
		t.Errorf("body = %q, want FAKE_WEIGHT", body)
	}
	if got := result.Header.Get("X-Linked-Etag"); got != "deadbeef" {
		t.Errorf("X-Linked-Etag = %q, want deadbeef", got)
	}
	if got := result.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Errorf("Accept-Ranges = %q, want bytes", got)
	}
	if !originGotAuth {
		t.Error("origin must receive Hugging Face Authorization")
	}
	if artifactGotAuth {
		t.Error("signed artifact target must not receive Authorization")
	}
}

func TestResolverForcesIdentityEncodingAcrossCacheableRedirect(t *testing.T) {
	var artifactEncoding string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repo/resolve/main/weights.bin":
			w.Header().Set("Location", server.URL+"/signed/blob")
			w.WriteHeader(http.StatusFound)
		case "/signed/blob":
			artifactEncoding = r.Header.Get("Accept-Encoding")
			_, _ = io.WriteString(w, "weights")
		}
	}))
	t.Cleanup(server.Close)

	headers := http.Header{"Accept-Encoding": {"gzip"}}
	result := resolve(t, server.URL, http.MethodGet, "/repo/resolve/main/weights.bin", headers, true)
	defer result.Body.Close()
	_, _ = io.Copy(io.Discard, result.Body)
	if artifactEncoding != "identity" {
		t.Fatalf("cacheable artifact Accept-Encoding = %q, want identity", artifactEncoding)
	}
}

func TestResolverPreservesOriginAuthorizationAcrossCanonicalRedirect(t *testing.T) {
	var canonicalGotAuth bool
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/old-name":
			w.Header().Set("X-Repo-Commit", "canonical-commit")
			w.Header().Set("Location", "/api/models/new-name")
			w.WriteHeader(http.StatusTemporaryRedirect)
		case "/api/models/new-name":
			canonicalGotAuth = r.Header.Get("Authorization") == "Bearer hf_private"
			if !canonicalGotAuth {
				http.Error(w, "missing auth", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"modelId":"new-name"}`)
		}
	}))
	t.Cleanup(server.Close)

	headers := http.Header{"Authorization": {"Bearer hf_private"}}
	result := resolve(t, server.URL, http.MethodGet, "/api/models/old-name", headers, false)
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK || string(body) != `{"modelId":"new-name"}` {
		t.Fatalf("canonical response = (%d, %q)", result.StatusCode, body)
	}
	if !canonicalGotAuth {
		t.Fatal("same-origin canonical redirect dropped Hugging Face Authorization")
	}
	if got := result.Header.Get("X-Repo-Commit"); got != "canonical-commit" {
		t.Fatalf("redirect metadata X-Repo-Commit = %q", got)
	}
}

func TestResolverHonorsRootRelativeRedirectFromBasePathOrigin(t *testing.T) {
	var requestedPaths []string
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPaths = append(requestedPaths, r.URL.Path)
		switch r.URL.Path {
		case "/mirror/api/models/old-name":
			http.Redirect(w, r, "/canonical", http.StatusTemporaryRedirect)
		case "/canonical":
			_, _ = io.WriteString(w, `{"modelId":"canonical"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	result := resolve(
		t,
		server.URL+"/mirror",
		http.MethodGet,
		"/api/models/old-name",
		nil,
		false,
	)
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK || string(body) != `{"modelId":"canonical"}` {
		t.Fatalf("canonical response = (%d, %q)", result.StatusCode, body)
	}
	want := []string{"/mirror/api/models/old-name", "/canonical"}
	if strings.Join(requestedPaths, ",") != strings.Join(want, ",") {
		t.Fatalf("origin paths = %q, want %q", requestedPaths, want)
	}
}

func TestResolverKeepsPaginationLinkAfterBasePathRedirectsToCanonicalRootPath(t *testing.T) {
	const apiPath = "/api/models/acme/model/tree/main"

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mirror" + apiPath:
			http.Redirect(w, r, apiPath, http.StatusTemporaryRedirect)
		case apiPath:
			w.Header().Add("Link", "<"+apiPath+"?cursor=next>; rel=\"next\"")
			w.Header().Add("Link", "</admin?cursor=unrelated>; rel=\"alternate\"")
			w.Header().Add("Link", "<"+apiPath+"?access_token=secret>; rel=\"alternate\"")
			w.Header().Add("Link", "<https://external.example/models?cursor=external>; rel=\"alternate\"")
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	result := resolve(
		t,
		server.URL+"/mirror",
		http.MethodGet,
		apiPath,
		nil,
		true,
	)
	defer result.Body.Close()

	const want = `</huggingface/api/models/acme/model/tree/main?cursor=next>; rel="next"`
	if got := result.Header.Values("Link"); len(got) != 1 || got[0] != want {
		t.Fatalf("canonical pagination Links = %q, want only %q", got, want)
	}
}

func TestResolverPassesErrorStatusAndBodyThrough(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"Entry not found"}`)
	}))
	t.Cleanup(origin.Close)

	result := resolve(t, origin.URL, http.MethodGet, "/missing/resolve/main/x", nil, true)
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", result.StatusCode)
	}
	if !strings.Contains(string(body), "Entry not found") {
		t.Errorf("body = %q, want upstream error body", body)
	}
}

func TestResolverNormalizesSameOriginPaginationLink(t *testing.T) {
	var origin *httptest.Server
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(
			"Link",
			"<"+origin.URL+"/api/models/acme/model/tree/main/folder%2Fsub?cursor=next&page=2>; rel=\"next\"",
		)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	}))
	t.Cleanup(origin.Close)

	result := resolve(
		t,
		origin.URL,
		http.MethodGet,
		"/api/models/acme/model/tree/main/folder%2Fsub",
		nil,
		true,
	)
	defer result.Body.Close()

	want := `</huggingface/api/models/acme/model/tree/main/folder%2Fsub?cursor=next&page=2>; rel="next"`
	if got := result.Header.Get("Link"); got != want {
		t.Fatalf("normalized Link = %q, want %q", got, want)
	}
}

func TestResolverDropsExternalOrCredentialBearingPaginationLink(t *testing.T) {
	for _, link := range []string{
		`<https://external.example/api/models/acme/model?cursor=next>; rel="next"`,
		`</api/models/acme/model?access_token=secret>; rel="next"`,
	} {
		t.Run(link, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Link", link)
				_, _ = io.WriteString(w, `[]`)
			}))
			t.Cleanup(origin.Close)

			result := resolve(t, origin.URL, http.MethodGet, "/api/models/acme/model", nil, true)
			defer result.Body.Close()
			if got := result.Header.Get("Link"); got != "" {
				t.Fatalf("unsafe Link was retained: %q", got)
			}
		})
	}
}

func TestResolverHEADDoesNotFetchArtifact(t *testing.T) {
	var artifactRequests int
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		artifactRequests++
		_, _ = io.WriteString(w, "body")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("origin method = %s, want HEAD", r.Method)
		}
		w.Header().Set("X-Linked-Etag", "deadbeef")
		w.Header().Set("X-Linked-Size", "11")
		// Real Hub LFS redirects carry a small redirect-document length
		// alongside the much larger linked object size.
		w.Header().Set("Content-Length", "1095")
		w.Header().Set("Location", artifact.URL+"/signed/blob")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	result := resolve(t, origin.URL, http.MethodHead, "/repo/resolve/main/weights.bin", nil, false)
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.StatusCode)
	}
	if len(body) != 0 {
		t.Errorf("HEAD response body = %q, want empty", body)
	}
	if got := result.Header.Get("X-Linked-Etag"); got != "deadbeef" {
		t.Errorf("X-Linked-Etag = %q, want deadbeef", got)
	}
	if result.Size != 11 || result.Header.Get("Content-Length") != "11" {
		t.Errorf("HEAD linked size = (%d, %q), want (11, 11)", result.Size, result.Header.Get("Content-Length"))
	}
	if artifactRequests != 0 {
		t.Fatalf("artifact requests = %d, want 0", artifactRequests)
	}
}

func TestResolverDoesNotInheritRedirectRepresentationHeaders(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Trailer", "X-Stream-End")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = io.WriteString(w, "artifact-11")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", artifact.URL+"/blob")
		w.Header().Set("Content-Length", "1095")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"redirect-document"`)
		w.Header().Set("Last-Modified", "Mon, 01 Jan 2024 00:00:00 GMT")
		w.Header().Set("X-Linked-Size", "11")
		w.WriteHeader(http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	result := resolve(t, origin.URL, http.MethodGet, "/repo/resolve/main/model.bin", nil, true)
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "artifact-11" || result.Size != 11 {
		t.Fatalf("artifact = (%q, size=%d)", body, result.Size)
	}
	for _, name := range []string{"ETag", "Last-Modified"} {
		if got := result.Header.Get(name); got != "" {
			t.Fatalf("redirect %s leaked into artifact response: %q", name, got)
		}
	}
	if got := result.Header.Get("Content-Type"); got == "text/plain" {
		t.Fatalf("redirect Content-Type leaked into artifact response: %q", got)
	}
	if got := result.Header.Get("Content-Length"); got != "" {
		t.Fatalf("redirect Content-Length leaked into chunked artifact: %q", got)
	}
}

func TestResolverAttributesSignedArtifactFailureToSelectedOrigin(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, artifact.URL+"/blob", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       origin.URL,
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	target := "/repo/resolve/main/model.bin"
	request := httptest.NewRequest(http.MethodGet, target, nil)
	result, err := newResolver().resolve(
		context.Background(),
		selected,
		request,
		target,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if result.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", result.StatusCode)
	}

	health := selected.HealthSnapshot()
	if health.Healthy || health.SuccessRate != 0 {
		t.Fatalf(
			"signed artifact failure health = (healthy=%v rate=%v), want false/0",
			health.Healthy,
			health.SuccessRate,
		)
	}
}

func TestExchangeHealthLatencyStopsAtFinalResponseHeaders(t *testing.T) {
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       "https://huggingface.example",
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	reporter := newExchangeHealthReporter(context.Background(), selected)
	response := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 1,
		Body:          io.NopCloser(strings.NewReader("x")),
		Header:        make(http.Header),
	}

	// Final response headers have arrived now. Delaying consumption simulates a
	// slow End User/cache sink and must not inflate Upstream network latency.
	reporter.observeArtifactResponse(response)
	time.Sleep(200 * time.Millisecond)
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	health := selected.HealthSnapshot()
	if health.SuccessRate != 1 {
		t.Fatalf("completed body success rate = %v, want 1", health.SuccessRate)
	}
	if health.AvgLatency >= 100*time.Millisecond {
		t.Fatalf(
			"upstream latency %v included the delayed body consumer; want header latency below 100ms",
			health.AvgLatency,
		)
	}
}

func TestExchangeHealthTreatsIdleTimeoutCauseAsFailureWhenBodyReturnsEOF(t *testing.T) {
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       "https://huggingface.example",
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	ctx, cancel := context.WithCancelCause(context.Background())
	reporter := newExchangeHealthReporter(ctx, selected)
	response := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 1,
		Body:          io.NopCloser(strings.NewReader("")),
		Header:        make(http.Header),
	}
	reporter.observeArtifactResponse(response)

	cancel(cache.ErrFetchIdleTimeout)
	if _, err := response.Body.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("body read error = %v, want EOF", err)
	}

	health := selected.HealthSnapshot()
	if health.Healthy || health.SuccessRate != 0 {
		t.Fatalf(
			"idle-timeout EOF health = (healthy=%v rate=%v), want false/0",
			health.Healthy,
			health.SuccessRate,
		)
	}
}

func TestExchangeHealthTreatsIdleTimeoutCauseAsFailureWhenBodyReturnsProgress(t *testing.T) {
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       "https://huggingface.example",
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	ctx, cancel := context.WithCancelCause(context.Background())
	reporter := newExchangeHealthReporter(ctx, selected)
	response := &http.Response{
		StatusCode:    http.StatusOK,
		ContentLength: 1,
		Body:          io.NopCloser(strings.NewReader("x")),
		Header:        make(http.Header),
	}
	reporter.observeArtifactResponse(response)

	cancel(cache.ErrFetchIdleTimeout)
	buffer := make([]byte, 1)
	if n, readErr := response.Body.Read(buffer); n != 1 || readErr != nil {
		t.Fatalf("body read = (%d, %v), want (1, nil)", n, readErr)
	}

	health := selected.HealthSnapshot()
	if health.Healthy || health.SuccessRate != 0 {
		t.Fatalf(
			"idle-timeout progress health = (healthy=%v rate=%v), want false/0",
			health.Healthy,
			health.SuccessRate,
		)
	}
}

func TestExchangeHealthIgnoresOrdinaryClientCancellation(t *testing.T) {
	tests := []struct {
		name            string
		body            io.ReadCloser
		baselineSuccess bool
	}{
		{
			name:            "EOF after cancellation",
			body:            io.NopCloser(strings.NewReader("")),
			baselineSuccess: false,
		},
		{
			name:            "transport close error after cancellation",
			body:            &fixedErrorBody{err: io.ErrClosedPipe},
			baselineSuccess: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool, err := upstream.NewPool([]config.UpstreamConfig{{
				Name:      "test-huggingface",
				URL:       "https://huggingface.example",
				Priority:  1,
				ProbeMode: "passive",
			}})
			if err != nil {
				t.Fatal(err)
			}
			selected := pool.Snapshot()[0]
			selected.Report(time.Millisecond, test.baselineSuccess)
			before := selected.HealthSnapshot()

			ctx, cancel := context.WithCancelCause(context.Background())
			reporter := newExchangeHealthReporter(ctx, selected)
			body := &healthReportingBody{ReadCloser: test.body, reporter: reporter}
			cancel(context.Canceled)
			_, _ = body.Read(make([]byte, 1))

			after := selected.HealthSnapshot()
			if after.SuccessRate != before.SuccessRate ||
				after.Healthy != before.Healthy ||
				after.AvgLatency != before.AvgLatency {
				t.Fatalf("ordinary cancellation changed health: before=%+v after=%+v", before, after)
			}
		})
	}
}

func TestExchangeHealthIdleTimeoutIgnoresCallerGapBeforeRead(t *testing.T) {
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       "https://huggingface.example",
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	ctx, cancel := context.WithCancelCause(context.Background())
	reporter := newExchangeHealthReporter(ctx, selected)
	originBody := newProgressOnCloseBody()
	body := cache.WithBodyIdleTimeout(
		cache.WithResponseMetadata(
			&healthReportingBody{ReadCloser: originBody, reporter: reporter},
			nil,
		),
		20*time.Millisecond,
		cancel,
	)

	select {
	case <-originBody.closed:
		t.Fatal("idle timeout treated time before an upstream Read as upstream failure")
	case <-time.After(50 * time.Millisecond):
	}

	readDone := make(chan error, 1)
	go func() {
		_, readErr := body.Read(make([]byte, 1))
		readDone <- readErr
	}()
	select {
	case <-originBody.readStarted:
	case <-time.After(time.Second):
		t.Fatal("nested upstream Read did not start")
	}
	select {
	case readErr := <-readDone:
		if !errors.Is(readErr, cache.ErrFetchIdleTimeout) {
			t.Fatalf("body read error = %v, want ErrFetchIdleTimeout", readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("active upstream Read did not honor idle timeout")
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}

	health := selected.HealthSnapshot()
	if health.Healthy || health.SuccessRate != 0 {
		t.Fatalf(
			"idle-timeout close health = (healthy=%v rate=%v), want false/0",
			health.Healthy,
			health.SuccessRate,
		)
	}
}

func TestDirectBodyIdleTimeoutReportsFailureWhenCloseUnblocksReadWithProgress(t *testing.T) {
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       "https://huggingface.example",
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	ctx, cancel := context.WithCancelCause(context.Background())
	reporter := newExchangeHealthReporter(ctx, selected)
	originBody := newProgressOnCloseBody()
	body := withDirectArtifactBodyIdleTimeout(
		&healthReportingBody{ReadCloser: originBody, reporter: reporter},
		20*time.Millisecond,
		cancel,
	)

	readDone := make(chan error, 1)
	go func() {
		buffer := make([]byte, 1)
		_, readErr := body.Read(buffer)
		readDone <- readErr
	}()
	select {
	case <-originBody.readStarted:
	case <-time.After(time.Second):
		t.Fatal("body read did not start")
	}
	select {
	case readErr := <-readDone:
		if !errors.Is(readErr, cache.ErrFetchIdleTimeout) {
			t.Fatalf("body read error = %v, want ErrFetchIdleTimeout", readErr)
		}
	case <-time.After(time.Second):
		t.Fatal("body read did not stop after idle timeout")
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}

	health := selected.HealthSnapshot()
	if health.Healthy || health.SuccessRate != 0 {
		t.Fatalf(
			"idle-timeout progress health = (healthy=%v rate=%v), want false/0",
			health.Healthy,
			health.SuccessRate,
		)
	}
}

func TestResolverAttributesTruncatedArtifactBodyToSelectedOrigin(t *testing.T) {
	artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "10")
		_, _ = io.WriteString(w, "abc")
	}))
	t.Cleanup(artifact.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		http.Redirect(w, request, artifact.URL+"/blob", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       origin.URL,
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	target := "/repo/resolve/main/model.bin"
	request := httptest.NewRequest(http.MethodGet, target, nil)
	result, err := newResolver().resolve(
		context.Background(),
		selected,
		request,
		target,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	if _, err := io.ReadAll(result.Body); err == nil {
		t.Fatal("truncated artifact body unexpectedly reached EOF successfully")
	}

	health := selected.HealthSnapshot()
	if health.Healthy || health.SuccessRate != 0 {
		t.Fatalf(
			"truncated artifact health = (healthy=%v rate=%v), want false/0",
			health.Healthy,
			health.SuccessRate,
		)
	}
}

func TestResolverTreatsBrokenSignedArtifact4xxAsOriginFailure(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			artifact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "broken signed URL", status)
			}))
			t.Cleanup(artifact.Close)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				http.Redirect(w, request, artifact.URL+"/blob", http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			pool, err := upstream.NewPool([]config.UpstreamConfig{{
				Name:      "test-huggingface",
				URL:       origin.URL,
				Priority:  1,
				ProbeMode: "passive",
			}})
			if err != nil {
				t.Fatal(err)
			}
			selected := pool.Snapshot()[0]
			target := "/repo/resolve/main/model.bin"
			request := httptest.NewRequest(http.MethodGet, target, nil)
			result, err := newResolver().resolve(
				context.Background(),
				selected,
				request,
				target,
				true,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer result.Body.Close()
			if result.StatusCode != status {
				t.Fatalf("status = %d, want %d", result.StatusCode, status)
			}
			health := selected.HealthSnapshot()
			if health.Healthy || health.SuccessRate != 0 {
				t.Fatalf(
					"signed artifact %d health = (healthy=%v rate=%v), want false/0",
					status,
					health.Healthy,
					health.SuccessRate,
				)
			}
		})
	}
}

func resolve(
	t *testing.T,
	originURL string,
	method string,
	target string,
	headers http.Header,
	cacheable bool,
) *resolvedResponse {
	t.Helper()
	pool, err := upstream.NewPool([]config.UpstreamConfig{{
		Name:      "test-huggingface",
		URL:       originURL,
		Priority:  1,
		ProbeMode: "passive",
	}})
	if err != nil {
		t.Fatal(err)
	}
	selected := pool.Snapshot()[0]
	request := httptest.NewRequest(method, target, nil)
	request.Header = headers.Clone()
	result, err := newResolver().resolve(
		context.Background(),
		selected,
		request,
		target,
		cacheable,
	)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
